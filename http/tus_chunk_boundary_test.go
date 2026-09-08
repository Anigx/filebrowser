package fbhttp

import (
	"io"
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

// A streaming body without Content-Length must not bypass the per-PATCH cap.
// Verify rollback preserves prior bytes and permits retry at the same offset.
func TestTusUnknownLengthChunkRollbackAndBoundary(t *testing.T) {
	scope := t.TempDir()
	key := []byte("chunk-boundary-fixture-key")
	perm := users.Permissions{Create: true, Modify: true}
	st := scopedUserStorage(t, scope, perm, key)
	token := signToken(t, st, perm, key)
	cache := newMemoryUploadCache()
	t.Cleanup(cache.Close)
	post := handle(tusPostHandler(cache), "", st, &settings.Server{})
	patch := handle(tusPatchHandler(cache), "", st, &settings.Server{})
	req := httptest.NewRequest(http.MethodPost, "/boundary.bin", nil)
	req.Header.Set("X-Auth", token)
	req.Header.Set("Upload-Length", strconv.Itoa(maxTusPatchBytes+1))
	rec := httptest.NewRecorder()
	post.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	send := func(body io.Reader, offset int) int {
		r := httptest.NewRequest(http.MethodPatch, "/boundary.bin", body)
		r.ContentLength = -1
		r.Header.Set("X-Auth", token)
		r.Header.Set("Upload-Offset", strconv.Itoa(offset))
		r.Header.Set("Content-Type", "application/offset+octet-stream")
		w := httptest.NewRecorder()
		patch.ServeHTTP(w, r)
		return w.Code
	}
	if s := send(strings.NewReader("A"), 0); s != http.StatusNoContent {
		t.Fatalf("prefix: %d", s)
	}
	if s := send(io.LimitReader(zeroChunkReader{}, maxTusPatchBytes+1), 1); s != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: %d", s)
	}
	data, err := os.ReadFile(filepath.Join(scope, "boundary.bin"))
	if err != nil || string(data) != "A" {
		t.Fatalf("rollback did not preserve prefix: %v length=%d", err, len(data))
	}
	if s := send(io.LimitReader(zeroChunkReader{}, maxTusPatchBytes), 1); s != http.StatusNoContent {
		t.Fatalf("exact boundary retry: %d", s)
	}
	info, err := os.Stat(filepath.Join(scope, "boundary.bin"))
	if err != nil || info.Size() != maxTusPatchBytes+1 {
		t.Fatalf("final size: %v %v", info, err)
	}
}

type zeroChunkReader struct{}

func (zeroChunkReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
