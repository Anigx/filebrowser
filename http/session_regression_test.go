package fbhttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/storage"
	"github.com/filebrowser/filebrowser/v2/storage/bolt"
	"github.com/filebrowser/filebrowser/v2/users"
)

func TestJWTSessionLogoutAndRenewalRotation(t *testing.T) {
	key := []byte("session-test-key")
	st := sessionTestStorage(t, key)
	server := &settings.Server{Root: t.TempDir()}
	user, err := st.Users.Get(server.Root, false, uint(1))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := st.Settings.Get()
	if err != nil {
		t.Fatal(err)
	}

	issue := func() string {
		rec := httptest.NewRecorder()
		status, err := printToken(rec, &data{store: st, settings: settings}, user, time.Hour, "")
		if err != nil || status != 0 {
			t.Fatalf("issue token status=%d err=%v", status, err)
		}
		return rec.Body.String()
	}
	request := func(handler handleFunc, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Auth", token)
		rec := httptest.NewRecorder()
		handle(handler, "", st, server).ServeHTTP(rec, req)
		return rec
	}
	protected := withUser(func(w http.ResponseWriter, _ *http.Request, _ *data) (int, error) {
		_, err := w.Write([]byte("ok"))
		return 0, err
	})

	token := issue()
	if rec := request(protected, token); rec.Code != http.StatusOK {
		t.Fatalf("issued token status=%d body=%q", rec.Code, rec.Body.String())
	}

	// A correctly signed JWT without a persisted JTI is never accepted.
	forged := &authToken{User: userInfo{ID: user.ID, Username: user.Username}, RegisteredClaims: jwt.RegisteredClaims{
		ID:        "not-persisted",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	forgedToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, forged).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(protected, forgedToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unpersisted JTI status=%d, want 401", rec.Code)
	}

	// A signed legacy-shaped token that omits JTI is also refused, even when its
	// user claims otherwise match a persisted user.
	noJTI := &authToken{User: userInfo{ID: user.ID, Username: user.Username}, RegisteredClaims: jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	noJTIToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, noJTI).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(protected, noJTIToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-JTI token status=%d, want 401", rec.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/", nil)
	logoutReq.Header.Set("X-Auth", token)
	logoutRec := httptest.NewRecorder()
	handle(logoutHandler, "", st, server).ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%q", logoutRec.Code, logoutRec.Body.String())
	}
	if rec := request(protected, token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logged out token status=%d, want 401", rec.Code)
	}

	old := issue()
	renewReq := httptest.NewRequest(http.MethodGet, "/", nil)
	renewReq.Header.Set("X-Auth", old)
	renewRec := httptest.NewRecorder()
	handle(renewHandler(time.Hour), "", st, server).ServeHTTP(renewRec, renewReq)
	if renewRec.Code != http.StatusOK {
		t.Fatalf("renew status=%d body=%q", renewRec.Code, renewRec.Body.String())
	}
	next := renewRec.Body.String()
	if next == old {
		t.Fatal("renewal reused the old JWT")
	}
	if rec := request(protected, old); rec.Code != http.StatusUnauthorized {
		t.Fatalf("rotated old token status=%d, want 401", rec.Code)
	}
	if rec := request(protected, next); rec.Code != http.StatusOK {
		t.Fatalf("rotated token status=%d body=%q", rec.Code, rec.Body.String())
	}
	// The old session was atomically consumed; a replay cannot mint a second token.
	if rec := request(renewHandler(time.Hour), old); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed renew status=%d, want 401", rec.Code)
	}
}

func sessionTestStorage(t *testing.T, key []byte) *storage.Storage {
	t.Helper()
	db, err := storm.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st, err := bolt.NewStorage(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Settings.Save(&settings.Settings{Key: key}); err != nil {
		t.Fatal(err)
	}
	if err := st.Users.Save(&users.User{Username: "session-user", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	return st
}
