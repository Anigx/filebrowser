package users

import (
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const MaxSecurityKeys = 5

var (
	ErrStaleSecurityState = errors.New("account changed; try again")
	ErrKeyOnlyAccount     = errors.New("account uses security keys")
)

// SecurityKey is private authentication state, not part of a user API record.
type SecurityKey struct {
	Label      string
	CreatedAt  time.Time
	Credential webauthn.Credential
}

type SecurityState struct {
	Epoch       uint64
	Handle      []byte
	Keys        []SecurityKey
	RetiredHash string
}

type SecurityStore interface {
	Get(userID uint) (SecurityState, error)
	Any() (bool, error)
	Change(userID uint, epoch uint64, apply func(*User, *SecurityState) error) error
	UpdateCredential(userID uint, epoch uint64, previous, verified webauthn.Credential) error
}
