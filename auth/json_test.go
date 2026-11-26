package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

type singleJSONUserStore struct {
	users.Store
	user *users.User
}

func (s singleJSONUserStore) Get(string, bool, interface{}) (*users.User, error) {
	return s.user, nil
}

func TestJSONAuthRejectsKeyOnlyAccount(t *testing.T) {
	const body = `{"username":"alice","password":"former-password"}`
	hash, err := users.HashPwd("former-password")
	require.NoError(t, err)
	account := &users.User{ID: 1, Username: "alice", Password: hash}
	store := singleJSONUserStore{user: account}
	login := func() (*users.User, error) {
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		return (JSONAuth{}).Auth(r, store, &settings.Settings{}, &settings.Server{})
	}
	got, err := login()
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)

	account.KeyOnly = true
	got, err = login()
	require.ErrorIs(t, err, os.ErrPermission)
	require.Nil(t, got)
}
