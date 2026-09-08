package bolt

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/asdine/storm/v3"
	bolt "go.etcd.io/bbolt"

	"github.com/filebrowser/filebrowser/v2/sessions"
)

var sessionBucket = []byte("jwt_sessions")

type sessionBackend struct {
	db *storm.DB
}

type storedSession struct {
	UserID    uint  `json:"userID"`
	ExpiresAt int64 `json:"expiresAt"`
}

func (s sessionBackend) Create(session sessions.Session) error {
	if session.ID == "" || session.UserID == 0 || session.ExpiresAt.IsZero() {
		return errors.New("invalid session")
	}
	return s.db.Bolt.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(sessionBucket)
		if err != nil {
			return err
		}
		if bucket.Get([]byte(session.ID)) != nil {
			return fmt.Errorf("session already exists")
		}
		encoded, err := json.Marshal(storedSession{UserID: session.UserID, ExpiresAt: session.ExpiresAt.Unix()})
		if err != nil {
			return err
		}
		return bucket.Put([]byte(session.ID), encoded)
	})
}

func (s sessionBackend) Valid(id string, userID uint, now time.Time) (bool, error) {
	if id == "" || userID == 0 {
		return false, nil
	}
	valid := false
	err := s.db.Bolt.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(sessionBucket)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(id))
		if value == nil {
			return nil
		}
		var session storedSession
		if err := json.Unmarshal(value, &session); err != nil {
			return err
		}
		valid = session.UserID == userID && session.ExpiresAt > now.Unix()
		return nil
	})
	return valid, err
}

func (s sessionBackend) Revoke(id string) error {
	if id == "" {
		return nil
	}
	return s.db.Bolt.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(sessionBucket)
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(id))
	})
}

func (s sessionBackend) Rotate(oldID string, next sessions.Session, now time.Time) (bool, error) {
	if oldID == "" || next.ID == "" || next.UserID == 0 || next.ExpiresAt.IsZero() {
		return false, nil
	}
	rotated := false
	err := s.db.Bolt.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(sessionBucket)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(oldID))
		if value == nil {
			return nil
		}
		var old storedSession
		if err := json.Unmarshal(value, &old); err != nil {
			return err
		}
		if old.UserID != next.UserID || old.ExpiresAt <= now.Unix() || bucket.Get([]byte(next.ID)) != nil {
			return nil
		}
		encoded, err := json.Marshal(storedSession{UserID: next.UserID, ExpiresAt: next.ExpiresAt.Unix()})
		if err != nil {
			return err
		}
		if err := bucket.Put([]byte(next.ID), encoded); err != nil {
			return err
		}
		if err := bucket.Delete([]byte(oldID)); err != nil {
			return err
		}
		rotated = true
		return nil
	})
	return rotated, err
}
