package bolt

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/asdine/storm/v3"

	"github.com/filebrowser/filebrowser/v2/sessions"
)

func TestSessionsPersistAndRotateAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	open := func() *storm.DB {
		db, err := storm.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return db
	}

	db := open()
	st, err := NewStorage(db)
	if err != nil {
		t.Fatal(err)
	}
	old := sessions.Session{ID: "old", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.Sessions.Create(old); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening the Bolt file must preserve revocation state rather than relying
	// on process-local memory.
	db = open()
	t.Cleanup(func() { _ = db.Close() })
	st, err = NewStorage(db)
	if err != nil {
		t.Fatal(err)
	}
	if valid, err := st.Sessions.Valid("old", 1); err != nil || !valid {
		t.Fatalf("persisted session valid=%v err=%v", valid, err)
	}
	next := sessions.Session{ID: "next", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}
	if rotated, err := st.Sessions.Rotate("old", next); err != nil || !rotated {
		t.Fatalf("rotate=%v err=%v", rotated, err)
	}
	if valid, _ := st.Sessions.Valid("old", 1); valid {
		t.Fatal("old session remained valid after rotation")
	}
	if valid, _ := st.Sessions.Valid("next", 1); !valid {
		t.Fatal("rotated session is not valid")
	}
	if rotated, err := st.Sessions.Rotate("old", sessions.Session{ID: "replay", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}); err != nil || rotated {
		t.Fatalf("replayed rotation=%v err=%v, want false nil", rotated, err)
	}
}
