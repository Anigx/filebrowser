package users

import (
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const MaxSecurityKeys = 5
const MaxSecurityKeyUsernameBytes = 256

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
	ChangeApproved(approval WriteApproval, id uint, apply func(*User, *SecurityState) error) error
	UpdateApproved(approval WriteApproval, user *User, fields ...string) error
	DeleteApproved(approval WriteApproval, id uint) error
	InsertApproved(approval WriteApproval, user *User) error
}

type WriteApproval struct {
	ActorID     uint
	ActorEpoch  uint64
	SessionID   string
	TargetEpoch uint64
}
