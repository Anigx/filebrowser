package cmd

import (
	"path/filepath"
	"testing"

	"github.com/asdine/storm/v3"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"

	"github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/storage/bolt"
	"github.com/filebrowser/filebrowser/v2/users"
)

func enrolledTestUser(t *testing.T) (*store, *users.User) {
	t.Helper()
	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	st, err := bolt.NewStorage(db)
	require.NoError(t, err)
	hash, err := users.HashPwd("distinct retired password")
	require.NoError(t, err)
	u := &users.User{Username: "test-account", Password: hash, LockPassword: true}
	require.NoError(t, st.Users.Save(u))
	require.NoError(t, st.SecurityKeys.Change(u.ID, 0, func(_ *users.User, state *users.SecurityState) error {
		state.Handle = make([]byte, 32)
		state.Keys = []users.SecurityKey{{
			Credential: webauthn.Credential{ID: []byte("id"), PublicKey: []byte("pub")},
		}}
		return nil
	}))
	return &store{Storage: st}, u
}

func TestWebAuthnConfigWithEnrolledUsers(t *testing.T) {
	st, _ := enrolledTestUser(t)
	existing := &settings.Settings{AuthMethod: auth.MethodJSONAuth, WebAuthnOrigin: "https://files.example"}
	for _, next := range []*settings.Settings{
		{AuthMethod: auth.MethodJSONAuth},
		{AuthMethod: auth.MethodJSONAuth, WebAuthnOrigin: "https://other.example"},
		{AuthMethod: auth.MethodProxyAuth, WebAuthnOrigin: "https://files.example"},
	} {
		require.Error(t, validateWebAuthnConfig(st, existing, next))
	}
	next := &settings.Settings{AuthMethod: auth.MethodJSONAuth, WebAuthnOrigin: "https://FILES.example:8443"}
	require.NoError(t, validateWebAuthnConfig(st, existing, next))
	require.Equal(t, "https://files.example:8443", next.WebAuthnOrigin)
}

func TestRecoverPassword(t *testing.T) {
	st, u := enrolledTestUser(t)
	require.Error(t, recoverKeyAccount(st, u.ID, "", 8))
	require.Error(t, recoverKeyAccount(st, u.ID, "distinct retired password", 8))
	require.NoError(t, recoverKeyAccount(st, u.ID, "unique replacement password", 8))
	got, err := st.Users.Get("", false, u.ID)
	require.NoError(t, err)
	require.False(t, got.KeyOnly)
	require.True(t, got.LockPassword)
	require.True(t, users.CheckPwd("unique replacement password", got.Password))
	state, err := st.SecurityKeys.Get(u.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, state.Epoch)
	require.Empty(t, state.Keys)
	require.Empty(t, state.RetiredHash)
}

func TestKeyUserImportExportGuards(t *testing.T) {
	st, u := enrolledTestUser(t)
	require.Error(t, guardUserExport(st))
	require.Error(t, guardUserImport(st, nil, true, false))
	require.Error(t, guardUserImport(st, []*users.User{{ID: u.ID}}, false, true))
	require.NoError(t, guardUserImport(st, []*users.User{{Username: "unrelated"}}, false, false))
}
