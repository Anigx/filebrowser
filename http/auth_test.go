package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/golang-jwt/jwt/v5"

	fbAuth "github.com/filebrowser/filebrowser/v2/auth"
	"github.com/filebrowser/filebrowser/v2/sessions"
	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/storage/bolt"
	"github.com/filebrowser/filebrowser/v2/users"
)

// Regression for the username-normalization home-directory collision
// (GHSA-7rc3-g7h6-22m7): with Signup and CreateUserDir enabled, two distinct
// usernames that cleanUsername() normalizes to the same directory must not be
// handed the same home directory. The second registration is rejected.
func TestSignupRejectsCollidingNormalizedScope(t *testing.T) {
	root := t.TempDir()

	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatalf("failed to get storage: %v", err)
	}
	if err := st.Settings.Save(&settings.Settings{
		Key:                   []byte("test-signing-key"),
		Signup:                true,
		CreateUserDir:         true,
		UserHomeBasePath:      "/users",
		MinimumPasswordLength: 1,
	}); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	server := &settings.Server{Root: root}

	signup := func(username string) *httptest.ResponseRecorder {
		body := `{"username":"` + username + `","password":"CollidePw12345!"}`
		req, _ := http.NewRequest(http.MethodPost, "/signup", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handle(signupHandler, "", st, server).ServeHTTP(rec, req)
		return rec
	}

	// Victim registers first and gets /users/teamone-x.
	if rec := signup("teamone-x"); rec.Code != http.StatusOK {
		t.Fatalf("first signup: expected 200, got %d body=%q", rec.Code, rec.Body.String())
	}

	// Attacker picks a distinct username that normalizes to the same scope.
	if rec := signup("teamone/x"); rec.Code != http.StatusConflict {
		t.Fatalf("VULNERABLE: colliding signup expected 409, got %d body=%q", rec.Code, rec.Body.String())
	}

	// The shared scope must still be owned solely by the first user.
	owner, err := st.Users.GetByScope("/users/teamone-x")
	if err != nil {
		t.Fatalf("expected first user to own the scope: %v", err)
	}
	if owner.Username != "teamone-x" {
		t.Fatalf("scope owner = %q, want teamone-x", owner.Username)
	}
}

// Expiry is a hard boundary even with a matching trusted proxy identity and
// a non-default logout page. A fresh proxy login, not an expired JWT renewal,
// is the reauthentication contract.
func TestProxySessionExpiryAndReauthentication(t *testing.T) {
	const proxyHeader = "X-Fb-User"
	key := []byte("test-signing-key")
	perm := users.Permissions{Download: true}
	st := scopedUserStorage(t, t.TempDir(), perm, key)
	if err := st.Settings.Save(&settings.Settings{
		Key: key, AuthMethod: fbAuth.MethodProxyAuth, LogoutPage: "/logged-out",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Auth.Save(&fbAuth.ProxyAuth{Header: proxyHeader}); err != nil {
		t.Fatal(err)
	}
	server := &settings.Server{TrustedProxyIPs: []string{"192.0.2.1"}}
	protected := withUser(func(_ http.ResponseWriter, _ *http.Request, _ *data) (int, error) {
		return http.StatusOK, nil
	})
	request := func(handler handleFunc, token, identity, peer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Auth", token)
		r.Header.Set(proxyHeader, identity)
		w := httptest.NewRecorder()
		handle(handler, "", st, server).ServeHTTP(w, r)
		return w
	}
	tokenFor := func(jwtExpiry, sessionExpiry time.Time, signingKey []byte, revoked bool) string {
		id, err := newSessionID()
		if err != nil {
			t.Fatal(err)
		}
		claims := &authToken{User: userInfo{ID: 1, Username: "u", Perm: perm}, RegisteredClaims: jwt.RegisteredClaims{
			ID: id, ExpiresAt: jwt.NewNumericDate(jwtExpiry),
		}}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Sessions.Create(sessions.Session{ID: id, UserID: 1, ExpiresAt: sessionExpiry}); err != nil {
			t.Fatal(err)
		}
		if revoked {
			if err := st.Sessions.Revoke(id); err != nil {
				t.Fatal(err)
			}
		}
		return token
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	expired := tokenFor(past, past, key, false)
	for name, token := range map[string]string{
		"expired JWT and session": expired,
		// An inconsistent store must never extend the signed JWT's lifetime.
		"expired JWT with longer persisted session": tokenFor(past, future, key, false),
		"valid JWT with expired session":            tokenFor(future, past, key, false),
		"revoked session":                           tokenFor(future, future, key, true),
		"expired and invalid signature":             tokenFor(past, future, []byte("wrong-key"), false),
	} {
		t.Run(name, func(t *testing.T) {
			for _, identity := range []string{"", "someone-else", "u"} {
				for _, peer := range []string{"192.0.2.1:1234", "198.51.100.1:1234"} {
					for _, handler := range []handleFunc{protected, renewHandler(time.Hour), logoutHandler} {
						rec := request(handler, token, identity, peer)
						if rec.Code != http.StatusUnauthorized {
							t.Fatalf("identity=%q peer=%q status=%d, want 401", identity, peer, rec.Code)
						}
						if hint := rec.Header().Get("X-Renew-Token"); hint != "" {
							t.Fatalf("rejected token received renewal hint %q", hint)
						}
					}
				}
			}
		})
	}
	// Existing unexpired bearer sessions remain compatible without proxy headers.
	if rec := request(protected, signToken(t, st, perm, key), "", "198.51.100.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("valid bearer token status=%d", rec.Code)
	}
	for _, tc := range []struct{ identity, peer string }{
		{"u", "198.51.100.1:1234"}, {"", "192.0.2.1:1234"},
	} {
		if rec := request(loginHandler(time.Hour), expired, tc.identity, tc.peer); rec.Code != http.StatusForbidden {
			t.Fatalf("unauthenticated proxy login status=%d, want 403", rec.Code)
		}
	}
	rec := request(loginHandler(time.Hour), expired, "u", "192.0.2.1:1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh trusted proxy login status=%d body=%q", rec.Code, rec.Body.String())
	}
	fresh := rec.Body.String()
	if fresh == expired {
		t.Fatal("proxy login reused expired token")
	}
	if rec := request(protected, fresh, "", "198.51.100.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("fresh token status=%d", rec.Code)
	}
	if rec := request(logoutHandler, fresh, "u", "192.0.2.1:1234"); rec.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d", rec.Code)
	}
	for _, handler := range []handleFunc{protected, renewHandler(time.Hour), logoutHandler} {
		if rec := request(handler, fresh, "u", "192.0.2.1:1234"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("logged-out token status=%d", rec.Code)
		}
	}

	// Deletion invalidates access and renewal even if its JTI row remains.
	deletedUserToken := signToken(t, st, perm, key)
	if err := st.Users.Delete(uint(1)); err != nil {
		t.Fatal(err)
	}
	for _, handler := range []handleFunc{protected, renewHandler(time.Hour), logoutHandler} {
		rec := request(handler, deletedUserToken, "u", "192.0.2.1:1234")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("deleted-user token status=%d, want 401", rec.Code)
		}
		if rec.Header().Get("X-Renew-Token") != "" {
			t.Fatal("deleted user received renewal hint")
		}
	}
}
