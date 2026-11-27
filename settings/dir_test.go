package settings

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/filebrowser/filebrowser/v2/users"
)

func TestMakeUserDirDoesNotLogRejectedIdentity(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)

	s := &Settings{CreateUserDir: true}
	if _, err := s.MakeUserDir("-", "", t.TempDir()); err == nil {
		t.Fatal("invalid identity accepted")
	}
	if output.Len() != 0 {
		t.Fatal("invalid identity should not be logged by directory provisioning")
	}
}

// A user provisioned with CreateUserDir must receive a per-user home directory
// derived from its username, not the default scope which normalizes to the
// server root.
func TestCreateUserHomeDerivesPerUserScope(t *testing.T) {
	s := &Settings{CreateUserDir: true, UserHomeBasePath: "/users"}

	user := &users.User{Username: "alice", Scope: "."}
	root := t.TempDir()
	derived, err := s.CreateUserHome(user, root, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !derived {
		t.Error("expected the scope to be reported as derived")
	}
	if user.Scope != "/users/alice" {
		t.Errorf("expected derived scope /users/alice, got %q", user.Scope)
	}
	info, err := os.Stat(filepath.Join(root, "users", "alice"))
	if err != nil || !info.IsDir() {
		t.Fatalf("home was not created beneath the configured root: %v", err)
	}
}

// A scope explicitly supplied by the caller (e.g. returned by an auth hook) must
// be preserved instead of being replaced by a derived home directory, and must
// not be reported as derived: it is legitimate for several users to share it.
func TestCreateUserHomePreservesExplicitScope(t *testing.T) {
	s := &Settings{CreateUserDir: true, UserHomeBasePath: "/users"}

	user := &users.User{Username: "alice", Scope: "/custom"}
	derived, err := s.CreateUserHome(user, t.TempDir(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if derived {
		t.Error("an explicit scope must not be reported as derived")
	}
	if user.Scope != "/custom" {
		t.Errorf("explicit scope should be preserved, got %q", user.Scope)
	}
}
