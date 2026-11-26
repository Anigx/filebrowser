package bolt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	bolt "go.etcd.io/bbolt"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/users"
)

var userSecurityBucket = []byte("user_security")

type userSecurityBackend struct{ usersBackend }

func userSecurityID(id uint) []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(id))
}

func readUserSecurity(tx *bolt.Tx, id uint) (state users.SecurityState, err error) {
	if bucket := tx.Bucket(userSecurityBucket); bucket != nil {
		if value := bucket.Get(userSecurityID(id)); value != nil {
			err = json.Unmarshal(value, &state)
		}
	}
	return state, err
}

func writeUserSecurity(tx *bolt.Tx, id uint, state users.SecurityState) error {
	bucket, err := tx.CreateBucketIfNotExists(userSecurityBucket)
	if err != nil {
		return err
	}
	value, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return bucket.Put(userSecurityID(id), value)
}

func (s userSecurityBackend) Get(id uint) (state users.SecurityState, err error) {
	err = s.db.Bolt.View(func(tx *bolt.Tx) error {
		if _, err := s.getBy(tx, id); err != nil {
			return err
		}
		state, err = readUserSecurity(tx, id)
		return err
	})
	return state, err
}

func (s userSecurityBackend) Any() (ok bool, err error) {
	err = s.db.Bolt.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(userSecurityBucket)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for _, raw := cursor.First(); raw != nil; _, raw = cursor.Next() {
			var state users.SecurityState
			if err := json.Unmarshal(raw, &state); err != nil {
				return err
			}
			if len(state.Keys) != 0 {
				ok = true
				break
			}
		}
		return nil
	})
	return ok, err
}

func (s userSecurityBackend) Change(id uint, epoch uint64, apply func(*users.User, *users.SecurityState) error) error {
	return s.change(id, epoch, nil, apply)
}

func (s userSecurityBackend) ChangeApproved(approval users.WriteApproval, id uint, apply func(*users.User, *users.SecurityState) error) error {
	return s.change(id, approval.TargetEpoch, &approval, apply)
}

func (s userSecurityBackend) change(id uint, epoch uint64, approval *users.WriteApproval, apply func(*users.User, *users.SecurityState) error) error {
	return s.db.Bolt.Update(func(tx *bolt.Tx) error {
		if err := s.checkApproval(tx, approval, id); err != nil {
			return err
		}
		user, err := s.getBy(tx, id)
		if err != nil {
			return err
		}
		state, err := readUserSecurity(tx, id)
		if err != nil {
			return err
		}
		if state.Epoch != epoch {
			return users.ErrStaleSecurityState
		}
		previousHash := user.Password
		previousState := state
		if err := apply(user, &state); err != nil {
			return err
		}
		if user.ID != id || len(state.Keys) > users.MaxSecurityKeys {
			return fberrors.ErrInvalidDataType
		}
		switch {
		case len(state.Keys) == 0:
			if len(previousState.Keys) == 0 || user.Password == "" ||
				user.Password == previousHash || user.Password == previousState.RetiredHash {
				return fberrors.ErrEmptyPassword
			}
			state.Handle, state.RetiredHash = nil, ""
		case len(previousState.Keys) == 0:
			random, err := users.RandomPwd(32)
			if err != nil {
				return err
			}
			user.Password, err = users.HashPwd(random)
			if err != nil {
				return err
			}
			state.RetiredHash = previousHash
		default:
			user.Password = previousHash
			state.Handle, state.RetiredHash = previousState.Handle, previousState.RetiredHash
		}
		if len(state.Keys) != 0 {
			if len(state.Handle) != 32 || state.RetiredHash == "" || len(user.Username) > users.MaxSecurityKeyUsernameBytes {
				return fberrors.ErrInvalidDataType
			}
			seen := make(map[string]bool, len(state.Keys))
			for _, key := range state.Keys {
				id := string(key.Credential.ID)
				if id == "" || len(key.Credential.PublicKey) == 0 || seen[id] {
					return fberrors.ErrInvalidDataType
				}
				seen[id] = true
			}
		}
		state.Epoch = epoch + 1
		if err := s.saveUser(tx, user); err != nil {
			return err
		}
		return writeUserSecurity(tx, id, state)
	})
}

func (s usersBackend) checkApproval(tx *bolt.Tx, approval *users.WriteApproval, target uint) error {
	if approval == nil {
		return nil
	}
	bucket := tx.Bucket(sessionBucket)
	if bucket == nil || approval.SessionID == "" || approval.ActorID == 0 {
		return fberrors.ErrPermissionDenied
	}
	raw := bucket.Get([]byte(approval.SessionID))
	if raw == nil {
		return fberrors.ErrPermissionDenied
	}
	var session storedSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return err
	}
	if session.UserID != approval.ActorID || session.ExpiresAt <= time.Now().Unix() {
		return fberrors.ErrPermissionDenied
	}
	actor, err := s.getBy(tx, approval.ActorID)
	if err != nil {
		return err
	}
	if err := hydrateUserSecurity(tx, actor); err != nil {
		return err
	}
	if actor.SessionVersion != approval.ActorEpoch || (actor.ID != target && !actor.Perm.Admin) {
		return fberrors.ErrPermissionDenied
	}
	return nil
}

func (s userSecurityBackend) InsertApproved(approval users.WriteApproval, user *users.User) error {
	if user.ID != 0 {
		return fberrors.ErrInvalidDataType
	}
	if err := user.Clean("", false); err != nil {
		return err
	}
	return s.db.Bolt.Update(func(tx *bolt.Tx) error {
		if err := s.checkApproval(tx, &approval, 0); err != nil {
			return err
		}
		return s.saveUser(tx, user)
	})
}

func (s userSecurityBackend) UpdateApproved(approval users.WriteApproval, user *users.User, fields ...string) error {
	if err := user.Clean("", false, fields...); err != nil {
		return err
	}
	return s.updateApproved(user, &approval, fields...)
}

func (s userSecurityBackend) DeleteApproved(approval users.WriteApproval, id uint) error {
	return s.deleteApproved(id, &approval)
}

func (s userSecurityBackend) UpdateCredential(id uint, epoch uint64, previous, verified webauthn.Credential) error {
	if !bytes.Equal(previous.ID, verified.ID) || verified.Authenticator.SignCount < previous.Authenticator.SignCount {
		return fberrors.ErrInvalidDataType
	}
	return s.db.Bolt.Update(func(tx *bolt.Tx) error {
		state, err := readUserSecurity(tx, id)
		if err != nil {
			return err
		}
		if state.Epoch != epoch {
			return users.ErrStaleSecurityState
		}
		for i, item := range state.Keys {
			if !bytes.Equal(item.Credential.ID, previous.ID) {
				continue
			}
			if !reflect.DeepEqual(item.Credential, previous) {
				return users.ErrStaleSecurityState
			}
			state.Keys[i].Credential = verified
			return writeUserSecurity(tx, id, state)
		}
		return errors.New("security key not found")
	})
}
