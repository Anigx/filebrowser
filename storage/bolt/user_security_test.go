package bolt

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/sessions"
	"github.com/filebrowser/filebrowser/v2/users"
)

func TestSecurityStateAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := storm.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	st, err := NewStorage(db)
	require.NoError(t, err)

	u := &users.User{Username: "alice", Password: "previous-hash", Scope: "/alice"}
	require.NoError(t, st.Users.Save(u))
	initial, err := st.SecurityKeys.Get(u.ID)
	require.NoError(t, err)
	require.Zero(t, initial.Epoch)

	enroll := func(_ *users.User, state *users.SecurityState) error {
		state.Handle = []byte("01234567890123456789012345678901")
		state.Keys = []users.SecurityKey{{
			Label: "test key", CreatedAt: time.Now(),
			Credential: webauthn.Credential{ID: []byte("test-credential"), PublicKey: []byte("test-public-key")},
		}}
		return nil
	}
	cancel := errors.New("canceled")
	require.ErrorIs(t, st.SecurityKeys.Change(u.ID, 0, func(account *users.User, state *users.SecurityState) error {
		require.NoError(t, enroll(account, state))
		return cancel
	}), cancel)
	before, err := st.Users.Get("", false, u.ID)
	require.NoError(t, err)
	require.Equal(t, "previous-hash", before.Password)
	require.False(t, before.KeyOnly)

	require.NoError(t, st.SecurityKeys.Change(u.ID, 0, enroll))
	require.Error(t, st.SecurityKeys.Change(u.ID, 0, func(*users.User, *users.SecurityState) error {
		t.Error("stale callback executed")
		return nil
	}))
	require.NoError(t, db.Close())
	db, err = storm.Open(path)
	require.NoError(t, err)
	st, err = NewStorage(db)
	require.NoError(t, err)

	keyOnly, err := st.Users.Get("", false, "alice")
	require.NoError(t, err)
	require.True(t, keyOnly.KeyOnly)
	require.EqualValues(t, 1, keyOnly.SessionVersion)
	require.NotEmpty(t, keyOnly.Password)
	require.NotEqual(t, "previous-hash", keyOnly.Password)
	state, err := st.SecurityKeys.Get(u.ID)
	require.NoError(t, err)
	require.Equal(t, "previous-hash", state.RetiredHash)
	require.Len(t, state.Keys, 1)
	require.ErrorIs(t, st.Users.Update(&users.User{ID: u.ID, Username: strings.Repeat("a", 257)}, "Username"), fberrors.ErrInvalidDataType)

	original := state.Keys[0].Credential
	verified := original
	verified.Authenticator.SignCount = 3
	require.NoError(t, st.SecurityKeys.UpdateCredential(u.ID, state.Epoch, original, verified))
	stale := original
	stale.Authenticator.SignCount = 2
	require.Error(t, st.SecurityKeys.UpdateCredential(u.ID, state.Epoch, original, stale))
	currentKeys, err := st.SecurityKeys.Get(u.ID)
	require.NoError(t, err)
	require.Equal(t, state.Epoch, currentKeys.Epoch, "reading a key must not revoke current sessions")
	require.EqualValues(t, 3, currentKeys.Keys[0].Credential.Authenticator.SignCount)

	for _, encode := range []func(any) ([]byte, error){json.Marshal, yaml.Marshal} {
		out, err := encode(keyOnly)
		require.NoError(t, err)
		require.NotContains(t, string(out), "test-public-key")
		require.NotContains(t, string(out), "previous-hash")
		require.NotContains(t, string(out), "essionVersion")
		require.NotContains(t, string(out), "essionversion")
		require.NotContains(t, string(out), "eyOnly")
		require.NotContains(t, string(out), "keyonly")
	}

	before.Locale = "de"
	require.Error(t, st.Users.Update(before))
	require.Error(t, st.Users.Update(&users.User{ID: u.ID, Password: "replacement-hash"}, "Password"))
	require.NoError(t, st.Users.Update(&users.User{ID: u.ID, Locale: "de"}, "Locale"))
	updated, err := st.Users.Get("", false, u.ID)
	require.NoError(t, err)
	require.True(t, updated.KeyOnly)
	require.EqualValues(t, 1, updated.SessionVersion, "profile preferences must not sign the user out")
	require.Equal(t, "de", updated.Locale)
	require.Equal(t, keyOnly.Password, updated.Password)

	updated.Locale = "en"
	require.NoError(t, st.Users.Update(updated))
	snapshot, err := st.SecurityKeys.Get(u.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Epoch)
	require.Len(t, snapshot.Keys, 1)
	require.Equal(t, "previous-hash", snapshot.RetiredHash)

	require.NoError(t, st.Users.Delete(u.ID))
	_, err = st.SecurityKeys.Get(u.ID)
	require.ErrorIs(t, err, fberrors.ErrNotExist)
}

func TestAccountWriteChecksSessionAndTargetAtCommit(t *testing.T) {
	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	st, err := NewStorage(db)
	require.NoError(t, err)
	admin := &users.User{Username: "admin", Password: "hash", Perm: users.Permissions{Admin: true}}
	target := &users.User{Username: "target", Password: "hash"}
	require.NoError(t, st.Users.Save(admin))
	require.NoError(t, st.Users.Save(target))
	require.NoError(t, st.Sessions.Create(sessions.Session{ID: "live", UserID: admin.ID, ExpiresAt: time.Now().Add(time.Hour)}))
	approval := users.WriteApproval{ActorID: admin.ID, SessionID: "live"}
	require.ErrorIs(t, st.SecurityKeys.InsertApproved(approval, target), fberrors.ErrInvalidDataType)
	require.NoError(t, st.SecurityKeys.UpdateApproved(approval, &users.User{ID: target.ID, Scope: "/new"}, "Scope"))
	require.ErrorIs(t, st.SecurityKeys.UpdateApproved(approval, &users.User{ID: target.ID, Scope: "/stale"}, "Scope"), users.ErrStaleSecurityState)
	require.NoError(t, st.Sessions.Revoke("live"))
	approval.TargetEpoch = 1
	require.Error(t, st.SecurityKeys.DeleteApproved(approval, target.ID))
	require.Error(t, st.SecurityKeys.InsertApproved(approval, &users.User{Username: "fresh", Password: "hash"}))
	got, err := st.Users.Get("", false, target.ID)
	require.NoError(t, err)
	require.Equal(t, "/new", got.Scope)
}
