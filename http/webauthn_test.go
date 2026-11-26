package fbhttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"

	"github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

func TestWebAuthnChallengeLifecycle(t *testing.T) {
	now := time.Now()
	cache := newKeyChallengeCache()
	cache.now = func() time.Time { return now }
	state := keyChallenge{purpose: "register", actorID: 1, sessionID: "session"}
	id, err := cache.put(state, time.Minute)
	require.NoError(t, err)
	_, ok := cache.take(id, func(s keyChallenge) bool { return s.sessionID == "other" })
	require.False(t, ok)
	got, ok := cache.take(id, func(s keyChallenge) bool { return s.sessionID == "session" })
	require.True(t, ok)
	require.EqualValues(t, 1, got.actorID)
	_, ok = cache.take(id, nil)
	require.False(t, ok)
	id, err = cache.put(state, time.Minute)
	require.NoError(t, err)
	now = now.Add(time.Minute)
	_, ok = cache.take(id, nil)
	require.False(t, ok)
}

func TestWebAuthnLoginAuthorization(t *testing.T) {
	st := sessionTestStorage(t, []byte("test-webauthn-session"))
	require.NoError(t, st.Settings.Save(&settings.Settings{
		AuthMethod: auth.MethodJSONAuth, Key: []byte("test-webauthn-session"),
		WebAuthnOrigin: "http://localhost:8000",
	}))
	require.NoError(t, st.Auth.Save(&auth.JSONAuth{}))
	require.NoError(t, st.SecurityKeys.Change(1, 0, func(_ *users.User, state *users.SecurityState) error {
		state.Handle = make([]byte, 32)
		state.Keys = []users.SecurityKey{{Credential: webauthn.Credential{ID: []byte("key"), PublicKey: []byte("pub")}}}
		return nil
	}))
	handler, err := NewHandler(nil, nil, nil, st, &settings.Server{Root: t.TempDir()}, fstest.MapFS{})
	require.NoError(t, err)
	call := func(path, origin string, payload any) *httptest.ResponseRecorder {
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, "http://localhost:8000/api/webauthn/login/"+path, bytes.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusForbidden, call("start", "https://other.example", map[string]string{"username": "session-user"}).Code)
	start := call("start", "http://localhost:8000", map[string]string{"username": "session-user"})
	require.Equal(t, http.StatusOK, start.Code)
	require.Equal(t, "no-store", start.Header().Get("Cache-Control"))
	var ceremony struct{ ID string }
	require.NoError(t, json.Unmarshal(start.Body.Bytes(), &ceremony))
	finish := map[string]any{"id": ceremony.ID, "credential": map[string]string{"type": "public-key"}}
	require.Equal(t, http.StatusForbidden, call("finish", "http://localhost:8000", finish).Code)
	require.Equal(t, http.StatusForbidden, call("finish", "http://localhost:8000", finish).Code)
}

func TestWebAuthnCredentialManagement(t *testing.T) {
	st := sessionTestStorage(t, []byte("test-webauthn-session"))
	require.NoError(t, st.Settings.Save(&settings.Settings{
		AuthMethod: auth.MethodJSONAuth, Key: []byte("test-webauthn-session"),
		WebAuthnOrigin: "http://localhost:8000", MinimumPasswordLength: 8,
	}))
	require.NoError(t, st.Auth.Save(&auth.JSONAuth{}))
	admin, err := st.Users.Get("", false, uint(1))
	require.NoError(t, err)
	admin.Password, err = users.HashPwd("admin-current-password")
	require.NoError(t, err)
	admin.Perm.Admin = true
	require.NoError(t, st.Users.Update(admin))
	retired, err := users.HashPwd("target-retired-password")
	require.NoError(t, err)
	target := &users.User{Username: "target", Password: retired, LockPassword: true}
	require.NoError(t, st.Users.Save(target))
	require.NoError(t, st.SecurityKeys.Change(target.ID, 0, func(_ *users.User, state *users.SecurityState) error {
		state.Handle = make([]byte, 32)
		state.Keys = []users.SecurityKey{{Credential: webauthn.Credential{ID: []byte("key"), PublicKey: []byte("pub")}}}
		return nil
	}))
	handler, err := NewHandler(nil, nil, nil, st, &settings.Server{Root: t.TempDir()}, fstest.MapFS{})
	require.NoError(t, err)
	signIn := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"session-user","password":"admin-current-password"}`))
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, signIn)
	require.Equal(t, http.StatusOK, login.Code)
	call := func(payload string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/webauthn/users/2/remove", bytes.NewBufferString(payload))
		r.Header.Set("X-Auth", login.Body.String())
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	require.Equal(t, http.StatusForbidden, call(`{"newPassword":"target-new-password"}`))
	require.Equal(t, http.StatusBadRequest, call(`{"keyID":"a2V5","currentPassword":"admin-current-password"}`))
	require.Equal(t, http.StatusBadRequest, call(`{"currentPassword":"admin-current-password","newPassword":"target-retired-password"}`))
	require.Equal(t, http.StatusNoContent, call(`{"currentPassword":"admin-current-password","newPassword":"target-new-password"}`))
	recovered, err := st.Users.Get("", false, target.ID)
	require.NoError(t, err)
	require.False(t, recovered.KeyOnly)
	require.True(t, users.CheckPwd("target-new-password", recovered.Password))
}
