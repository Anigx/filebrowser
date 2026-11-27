package cmd

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/asdine/storm/v3"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

func TestStartupWebAuthnEnvironment(t *testing.T) {
	t.Setenv("FB_WEBAUTHN_ORIGIN", "https://FILES.example:443")
	start := &cobra.Command{}
	start.Flags().AddFlagSet(rootCmd.Flags())
	start.Flags().AddFlagSet(rootCmd.PersistentFlags())
	v, err := initViper(start)
	require.NoError(t, err)
	require.Equal(t, "https://FILES.example:443", v.GetString("webauthn.origin"))
}

func TestStartupWebAuthnPrecedence(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"webauthn":{"origin":"https://config.example"}}`), 0600))
	makeCommand := func() *cobra.Command {
		t.Helper()
		cmd := &cobra.Command{}
		cmd.Flags().String("config", configPath, "")
		cmd.Flags().String("webauthn.origin", "", "")
		return cmd
	}
	t.Setenv("FB_WEBAUTHN_ORIGIN", "")
	v, err := initViper(makeCommand())
	require.NoError(t, err)
	require.NoError(t, validateStartupWebAuthn(v, false))
	require.Empty(t, v.GetString("webauthn.origin"))

	t.Setenv("FB_WEBAUTHN_ORIGIN", "https://env.example")
	cmd := makeCommand()
	v, err = initViper(cmd)
	require.NoError(t, err)
	require.Equal(t, "https://env.example", v.GetString("webauthn.origin"))
	require.NoError(t, cmd.Flags().Set("webauthn.origin", "https://flag.example"))
	v, err = initViper(cmd)
	require.NoError(t, err)
	require.Equal(t, "https://flag.example", v.GetString("webauthn.origin"))

	v = viper.New()
	v.Set("webauthn.origin", []string{"https://invalid.example"})
	require.ErrorContains(t, validateStartupWebAuthn(v, false), "must be a string")
}

func TestStartupWebAuthnRechecksStoreUnderLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contended.db")
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("database", path, "")
	run := withViperAndStore(func(_ *cobra.Command, _ []string, _ *viper.Viper, st *store) error {
		require.True(t, st.databaseExisted, "do not bootstrap over settings created by another start")
		return nil
	}, storeOptions{allowsNoDatabase: true, validateBeforeOpen: func(_ *viper.Viper, exists bool) error {
		require.False(t, exists)
		db, err := storm.Open(path)
		require.NoError(t, err)
		defer db.Close()
		s, err := bolt.NewStorage(db)
		require.NoError(t, err)
		return s.Settings.Save(&settings.Settings{Key: []byte("existing"), AuthMethod: auth.MethodJSONAuth})
	}})
	require.NoError(t, run(cmd, nil))
}

func TestStartupWebAuthnUnchangedWhenPortIsUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := storm.Open(path)
	require.NoError(t, err)
	s, err := bolt.NewStorage(db)
	require.NoError(t, err)
	require.NoError(t, s.Settings.Save(&settings.Settings{Key: []byte("test"), AuthMethod: auth.MethodJSONAuth, WebAuthnOrigin: "https://files.example"}))
	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	require.NoError(t, s.Settings.SaveServer(&settings.Server{Address: "127.0.0.1", Port: port, Root: t.TempDir()}))
	require.NoError(t, db.Close())

	t.Setenv("FB_DATABASE", path)
	t.Setenv("FB_WEBAUTHN_ORIGIN", "https://files.example:8443")
	cmd := &cobra.Command{}
	cmd.Flags().AddFlagSet(rootCmd.Flags())
	cmd.Flags().AddFlagSet(rootCmd.PersistentFlags())
	require.ErrorContains(t, rootCmd.RunE(cmd, nil), "listen")
	db, err = storm.Open(path)
	require.NoError(t, err)
	defer db.Close()
	s, err = bolt.NewStorage(db)
	require.NoError(t, err)
	set, err := s.Settings.Get()
	require.NoError(t, err)
	require.Equal(t, "https://files.example", set.WebAuthnOrigin)
}

func TestStartupWebAuthnValidationBeforeOpen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
		noauth bool
	}{
		{name: "insecure origin", origin: "http://files.example"},
		{name: "noauth bootstrap", origin: "https://files.example", noauth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "new.db")
			flags := &cobra.Command{}
			flags.Flags().String("config", "", "")
			flags.Flags().String("database", path, "")
			flags.Flags().String("webauthn.origin", tc.origin, "")
			flags.Flags().Bool("noauth", tc.noauth, "")
			t.Setenv("FB_WEBAUTHN_ORIGIN", tc.origin)
			t.Setenv("FB_DATABASE", path)
			run := withViperAndStore(func(_ *cobra.Command, _ []string, _ *viper.Viper, _ *store) error {
				t.Error("invalid startup settings reached the open database")
				return nil
			}, storeOptions{allowsNoDatabase: true, validateBeforeOpen: validateStartupWebAuthn})
			require.Error(t, run(flags, nil))
			require.NoFileExists(t, path)
		})
	}
}

func TestStartupWebAuthnPersistsAndGuardsOrigin(t *testing.T) {
	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s, err := bolt.NewStorage(db)
	require.NoError(t, err)
	st := &store{Storage: s}
	set := &settings.Settings{Key: []byte("test"), AuthMethod: auth.MethodJSONAuth}
	require.NoError(t, st.Settings.Save(set))
	check := func(want string) {
		t.Helper()
		settings, err := st.Settings.Get()
		require.NoError(t, err)
		require.Equal(t, want, settings.WebAuthnOrigin)
		require.Equal(t, auth.MethodJSONAuth, settings.AuthMethod)
		require.Equal(t, []byte("test"), settings.Key)
	}
	next, err := stageStartupWebAuthn(st, "https://FILES.example:443")
	require.NoError(t, err)
	check("")
	require.Equal(t, "https://files.example", next.WebAuthnOrigin)
	require.NoError(t, st.Settings.Save(next))
	next, err = stageStartupWebAuthn(st, "https://files.example")
	require.NoError(t, err)
	require.Nil(t, next)
	next, err = stageStartupWebAuthn(st, "")
	require.NoError(t, err)
	require.Nil(t, next)
	_, err = stageStartupWebAuthn(st, "http://files.example")
	require.Error(t, err)
	check("https://files.example")

	user := &users.User{Username: "test-user", Password: "test-hash"}
	require.NoError(t, st.Users.Save(user))
	err = st.SecurityKeys.Change(user.ID, 0, func(_ *users.User, state *users.SecurityState) error {
		state.Handle = make([]byte, 32)
		state.Keys = []users.SecurityKey{{Credential: webauthn.Credential{ID: []byte("id"), PublicKey: []byte("pub")}}}
		return nil
	})
	require.NoError(t, err)
	_, err = stageStartupWebAuthn(st, "https://other.example")
	require.Error(t, err)
	check("https://files.example")
	next, err = stageStartupWebAuthn(st, "https://files.example:8443")
	require.NoError(t, err)
	require.Equal(t, "https://files.example:8443", next.WebAuthnOrigin)
}
