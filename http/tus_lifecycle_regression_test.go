package fbhttp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/filebrowser/filebrowser/v2/diskcache"
	"github.com/filebrowser/filebrowser/v2/settings"
)

func (f *tusTestFixture) resourceMutation(t *testing.T, handler handleFunc, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-Auth", f.token)
	rec := httptest.NewRecorder()
	handle(withUploadCacheLease(f.cache, handler), "", f.store, &settings.Server{}).ServeHTTP(rec, req)
	return rec
}

// Resource delete, overwrite, and rename must invalidate the upload state that
// was bound to the old object. Recreating the pathname is deliberately part of
// each case: PATCH must never authorize the replacement through the old entry.
func TestTusLifecycleMutationsCannotRebindReplacement(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *tusTestFixture, string)
	}{
		{
			name: "delete",
			mutate: func(t *testing.T, f *tusTestFixture, uploadURL string) {
				rec := f.resourceMutation(t, resourceDeleteHandler(diskcache.NewNoOp(), f.cache), http.MethodDelete, "/active.bin", nil)
				if rec.Code != http.StatusNoContent {
					t.Fatalf("DELETE = %d, body=%q", rec.Code, rec.Body.String())
				}
				if err := os.WriteFile(filepath.Join(f.scope, "active.bin"), []byte("replacement"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "overwrite",
			mutate: func(t *testing.T, f *tusTestFixture, uploadURL string) {
				rec := f.resourceMutation(t, resourcePostHandler(diskcache.NewNoOp(), f.cache), http.MethodPost, "/active.bin?override=true", bytes.NewBufferString("replacement"))
				if rec.Code != http.StatusOK {
					t.Fatalf("POST override = %d, body=%q", rec.Code, rec.Body.String())
				}
			},
		},
		{
			name: "rename",
			mutate: func(t *testing.T, f *tusTestFixture, uploadURL string) {
				rec := f.resourceMutation(t, resourcePatchHandler(diskcache.NewNoOp()), http.MethodPatch, "/active.bin?action=rename&destination=/moved.bin", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("PATCH rename = %d, body=%q", rec.Code, rec.Body.String())
				}
				if err := os.WriteFile(filepath.Join(f.scope, "active.bin"), []byte("replacement"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTusTestFixture(t)
			uploadURL := f.create(t, "active.bin", 16)
			tt.mutate(t, f, uploadURL)

			res, _ := f.patch(t, uploadURL, 0, []byte("attacker-data"))
			defer res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("stale PATCH = %d, want 404", res.StatusCode)
			}
			got, err := os.ReadFile(filepath.Join(f.scope, "active.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "replacement" {
				t.Fatalf("replacement was modified: %q", got)
			}
		})
	}
}
