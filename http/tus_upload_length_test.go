package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

// A TUS PATCH must not write more than the declared Upload-Length. A client that
// declares a small length and then streams a large body must be rejected without
// the excess being written to disk, while a correctly-sized upload still works.
func TestTusPatchEnforcesUploadLength(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}

	key := []byte("test-signing-key")
	perm := users.Permissions{Create: true, Modify: true}
	st := scopedUserStorage(t, userScope, perm, key)
	signed := signToken(t, st, perm, key)

	cache := newMemoryUploadCache()
	t.Cleanup(cache.Close)
	post := handle(tusPostHandler(cache), "", st, &settings.Server{})
	patch := handle(tusPatchHandler(cache), "", st, &settings.Server{})

	patchReq := func(body string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(http.MethodPatch, "/file.txt", strings.NewReader(body))
		req.Header.Set("X-Auth", signed)
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.Header.Set("Upload-Offset", "0")
		rec := httptest.NewRecorder()
		patch.ServeHTTP(rec, req)
		return rec
	}

	// Create an upload that declares only 5 bytes.
	reqPost, _ := http.NewRequest(http.MethodPost, "/file.txt", http.NoBody)
	reqPost.Header.Set("X-Auth", signed)
	reqPost.Header.Set("Upload-Length", "5")
	recPost := httptest.NewRecorder()
	post.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusCreated {
		t.Fatalf("POST expected 201, got %d body=%q", recPost.Code, recPost.Body.String())
	}

	// A body far larger than the declared length must be rejected, and nothing
	// beyond the declared length may reach disk.
	if rec := patchReq(strings.Repeat("A", 5000)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-length PATCH expected 413, got %d body=%q", rec.Code, rec.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(userScope, "file.txt")); err != nil {
		t.Fatalf("stat file.txt: %v", err)
	} else if fi.Size() > 5 {
		t.Fatalf("wrote %d bytes despite Upload-Length 5", fi.Size())
	}

	// A correctly-sized upload still completes.
	if rec := patchReq("hello"); rec.Code != http.StatusNoContent {
		t.Fatalf("valid PATCH expected 204, got %d body=%q", rec.Code, rec.Body.String())
	}
	if data, _ := os.ReadFile(filepath.Join(userScope, "file.txt")); string(data) != "hello" {
		t.Fatalf("expected file content \"hello\", got %q", string(data))
	}
}

func TestTusPatchRejectsCloudflareOversizedChunk(t *testing.T) {
	root := t.TempDir()
	userScope := filepath.Join(root, "user")
	if err := os.MkdirAll(userScope, 0o755); err != nil {
		t.Fatal(err)
	}

	key := []byte("cloudflare-chunk-test-key")
	perm := users.Permissions{Create: true, Modify: true}
	st := scopedUserStorage(t, userScope, perm, key)
	token := signToken(t, st, perm, key)
	cache := newMemoryUploadCache()
	t.Cleanup(cache.Close)
	post := handle(tusPostHandler(cache), "", st, &settings.Server{})
	patch := handle(tusPatchHandler(cache), "", st, &settings.Server{})

	create, _ := http.NewRequest(http.MethodPost, "/large.bin", http.NoBody)
	create.Header.Set("X-Auth", token)
	create.Header.Set("Upload-Length", strconv.FormatInt(maxTusPatchBytes+1, 10))
	created := httptest.NewRecorder()
	post.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("POST expected 201, got %d body=%q", created.Code, created.Body.String())
	}

	// The declared request length is rejected before its body is consumed or
	// appended, so this test does not need to allocate a >95 MiB buffer.
	request, _ := http.NewRequest(http.MethodPatch, "/large.bin", strings.NewReader("x"))
	request.ContentLength = maxTusPatchBytes + 1
	request.Header.Set("X-Auth", token)
	request.Header.Set("Content-Type", "application/offset+octet-stream")
	request.Header.Set("Upload-Offset", "0")
	recorded := httptest.NewRecorder()
	patch.ServeHTTP(recorded, request)
	if recorded.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized PATCH expected 413, got %d body=%q", recorded.Code, recorded.Body.String())
	}
	if info, err := os.Stat(filepath.Join(userScope, "large.bin")); err != nil {
		t.Fatal(err)
	} else if info.Size() != 0 {
		t.Fatalf("oversized PATCH wrote %d bytes", info.Size())
	}
}
