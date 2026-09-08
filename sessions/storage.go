// Package sessions persists individually revocable JWT sessions.
package sessions

import "time"

// Session is the server-side state bound to a JWT ID (JTI).
type Session struct {
	ID        string
	UserID    uint
	ExpiresAt time.Time
}

// StorageBackend provides atomic session operations.
type StorageBackend interface {
	Create(Session) error
	Valid(id string, userID uint, now time.Time) (bool, error)
	Revoke(id string) error
	Rotate(oldID string, next Session, now time.Time) (bool, error)
}

// Storage is a server-side session store.
type Storage struct {
	back StorageBackend
}

func NewStorage(back StorageBackend) *Storage { return &Storage{back: back} }

func (s *Storage) Create(session Session) error { return s.back.Create(session) }

// Valid verifies that the JTI is still active and belongs to the authenticated user.
func (s *Storage) Valid(id string, userID uint) (bool, error) {
	return s.back.Valid(id, userID, time.Now())
}

// Revoke invalidates one token without affecting the user's other sessions.
func (s *Storage) Revoke(id string) error { return s.back.Revoke(id) }

// Rotate atomically replaces oldID with next. A replayed old token cannot win a
// second rotation because the old session is removed in the same transaction.
func (s *Storage) Rotate(oldID string, next Session) (bool, error) {
	return s.back.Rotate(oldID, next, time.Now())
}
