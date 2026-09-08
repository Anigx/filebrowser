package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/filebrowser/filebrowser/v2/diskcache"
	"github.com/filebrowser/filebrowser/v2/files"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
	"github.com/spf13/afero"
)

func TestCommandEndpointRejectsBeforeWebSocketUpgrade(t *testing.T) {
	root := t.TempDir()
	key := []byte("security-regression-key")
	perm := users.Permissions{Execute: false}
	st := scopedUserStorage(t, root, perm, key)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Auth", signToken(t, st, perm, key))
	rec := httptest.NewRecorder()

	handle(commandsHandler, "", st, &settings.Server{EnableExec: false}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled command endpoint status=%d, want 403 before WebSocket upgrade", rec.Code)
	}
}

func TestSubtitleFileHandlerRejectsOversizedSource(t *testing.T) {
	for _, name := range []string{"large.srt", "large.ass"} {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := "/" + name
			content := make([]byte, maxSubtitleBytes+1)
			if err := afero.WriteFile(fs, path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := fs.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			status, err := subtitleFileHandler(
				httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "/api/subtitle/"+name, nil),
				&files.FileInfo{Fs: fs, Path: path, Name: name, Size: info.Size(), ModTime: info.ModTime()},
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized subtitle status=%d, want %d", status, http.StatusRequestEntityTooLarge)
			}
		})
	}
}

// FileInfo is built from a prior stat, so the object can change before a
// parser opens it. A stale regular-file classification must not let a special
// object (or a directory) reach a buffering reader.
func TestSubtitleFileHandlerRejectsObjectReplacedAfterStat(t *testing.T) {
	fs := afero.NewMemMapFs()
	const subtitlePath = "/replaced.srt"
	if err := afero.WriteFile(fs, subtitlePath, []byte("1\n00:00:00,000 --> 00:00:01,000\nok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := fs.Stat(subtitlePath)
	if err != nil {
		t.Fatal(err)
	}
	stale := &files.FileInfo{Fs: fs, Path: subtitlePath, Name: "replaced.srt", Size: info.Size(), ModTime: info.ModTime()}
	if err := fs.Remove(subtitlePath); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(subtitlePath, 0o700); err != nil {
		t.Fatal(err)
	}

	status, err := subtitleFileHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/subtitle/replaced.srt", nil), stale)
	if err == nil {
		t.Fatal("expected replacement to be rejected")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("replaced subtitle status=%d, want %d", status, http.StatusBadRequest)
	}
}

func TestResourcePostDirectoryDoesNotBypassDeletePermission(t *testing.T) {
	root := t.TempDir()
	scope := filepath.Join(root, "scope")
	target := filepath.Join(scope, "team")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "protected.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	key := []byte("security-regression-key")
	perm := users.Permissions{Create: true, Modify: true, Delete: false}
	st := scopedUserStorage(t, scope, perm, key)
	token := signToken(t, st, perm, key)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/team", nil)
	deleteReq.Header.Set("X-Auth", token)
	deleteRec := httptest.NewRecorder()
	handle(resourceDeleteHandler(diskcache.NewNoOp()), "", st, &settings.Server{}).ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusForbidden {
		t.Fatalf("negative control DELETE status=%d, want 403", deleteRec.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/team?override=true", strings.NewReader("x"))
	postReq.Header.Set("X-Auth", token)
	postRec := httptest.NewRecorder()
	handle(resourcePostHandler(diskcache.NewNoOp()), "", st, &settings.Server{}).ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("directory POST status=%d, want 400", postRec.Code)
	}
	if content, err := os.ReadFile(filepath.Join(target, "protected.txt")); err != nil || string(content) != "keep" {
		t.Fatalf("directory contents were altered: content=%q err=%v", content, err)
	}
}
