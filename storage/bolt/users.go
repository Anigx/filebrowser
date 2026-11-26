package bolt

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	bolt "go.etcd.io/bbolt"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/users"
)

type usersBackend struct {
	db *storm.DB
}

func (st usersBackend) GetBy(i interface{}) (user *users.User, err error) {
	err = st.db.Bolt.View(func(tx *bolt.Tx) error {
		user, err = st.getBy(tx, i)
		if err != nil {
			return err
		}
		return hydrateUserSecurity(tx, user)
	})
	return user, err
}

func (st usersBackend) getBy(tx *bolt.Tx, i interface{}) (user *users.User, err error) {
	user = &users.User{}

	var arg string
	switch i.(type) {
	case uint:
		arg = "ID"
	case string:
		arg = "Username"
	default:
		return nil, fberrors.ErrInvalidDataType
	}

	err = st.db.WithTransaction(tx).One(arg, i, user)

	if err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil, fberrors.ErrNotExist
		}
		return nil, err
	}

	return
}

func hydrateUserSecurity(tx *bolt.Tx, user *users.User) error {
	state, err := readUserSecurity(tx, user.ID)
	if err != nil {
		return err
	}
	user.SessionVersion = state.Epoch
	user.KeyOnly = len(state.Keys) != 0
	return nil
}

func (st usersBackend) GetByScope(scope string) (*users.User, error) {
	user := &users.User{}
	// Match case-insensitively: on a case-insensitive filesystem two scopes
	// that differ only in case (e.g. /users/Alice and /users/alice) resolve to
	// the same home directory, so they must be treated as a collision.
	pattern := "(?i)^" + regexp.QuoteMeta(scope) + "$"
	err := st.db.Bolt.View(func(tx *bolt.Tx) error {
		if err := st.db.WithTransaction(tx).Select(q.Re("Scope", pattern)).First(user); err != nil {
			return err
		}
		return hydrateUserSecurity(tx, user)
	})
	if err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil, fberrors.ErrNotExist
		}
		return nil, err
	}

	return user, nil
}

func (st usersBackend) Gets() ([]*users.User, error) {
	var allUsers []*users.User
	err := st.db.Bolt.View(func(tx *bolt.Tx) error {
		if err := st.db.WithTransaction(tx).All(&allUsers); err != nil {
			return err
		}
		for _, user := range allUsers {
			if err := hydrateUserSecurity(tx, user); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, storm.ErrNotFound) {
		return nil, fberrors.ErrNotExist
	}

	if err != nil {
		return allUsers, err
	}

	return allUsers, err
}

func (st usersBackend) Update(user *users.User, fields ...string) error {
	return st.updateApproved(user, nil, fields...)
}

func (st usersBackend) updateApproved(user *users.User, approval *users.WriteApproval, fields ...string) error {
	var next *users.User
	var epoch uint64
	var keyOnly bool
	err := st.db.Bolt.Update(func(tx *bolt.Tx) error {
		if err := st.checkApproval(tx, approval, user.ID); err != nil {
			return err
		}
		current, err := st.getBy(tx, user.ID)
		if err != nil {
			return err
		}
		state, err := readUserSecurity(tx, user.ID)
		if err != nil {
			return err
		}
		if approval != nil && approval.TargetEpoch != state.Epoch {
			return users.ErrStaleSecurityState
		}
		oldPassword := current.Password
		next = current
		keyOnly = len(state.Keys) != 0
		if len(fields) == 0 {
			if user.SessionVersion != state.Epoch {
				return users.ErrStaleSecurityState
			}
			next = user
		} else if err := mergeUserFields(next, user, fields); err != nil {
			return err
		}
		if keyOnly && next.Password != oldPassword {
			return users.ErrKeyOnlyAccount
		}
		if keyOnly && len(next.Username) > users.MaxSecurityKeyUsernameBytes {
			return fberrors.ErrInvalidDataType
		}
		if err := st.saveUser(tx, next); err != nil {
			return err
		}
		if securityRelevantUserFields(fields) {
			state.Epoch++
		}
		epoch = state.Epoch
		return writeUserSecurity(tx, user.ID, state)
	})
	if err == nil {
		user.SessionVersion, user.KeyOnly = epoch, keyOnly
	}
	return err
}

func securityRelevantUserFields(fields []string) bool {
	for _, field := range fields {
		switch field {
		case "Locale", "HideDotfiles", "SingleClick", "RedirectAfterCopyMove", "DateFormat", "AceEditorTheme", "ViewMode", "Sorting":
		default:
			return true
		}
	}
	return len(fields) == 0
}

func mergeUserFields(dst, src *users.User, fields []string) error {
	target, source := reflect.ValueOf(dst).Elem(), reflect.ValueOf(src).Elem()
	for _, field := range fields {
		sf, ok := target.Type().FieldByName(field)
		if !ok || sf.PkgPath != "" || sf.Tag.Get("json") == "-" || field == "ID" {
			return fmt.Errorf("invalid field: %s", field)
		}
		target.FieldByIndex(sf.Index).Set(source.FieldByIndex(sf.Index))
	}
	return nil
}

func (st usersBackend) Save(user *users.User) error {
	if user.ID != 0 {
		return st.Update(user)
	}
	return st.db.Bolt.Update(func(tx *bolt.Tx) error {
		return st.saveUser(tx, user)
	})
}

func (st usersBackend) saveUser(tx *bolt.Tx, user *users.User) error {
	err := st.db.WithTransaction(tx).Save(user)
	if errors.Is(err, storm.ErrAlreadyExists) {
		return fberrors.ErrExist
	}
	return err
}

func (st usersBackend) DeleteByID(id uint) error {
	return st.deleteBy(id)
}

func (st usersBackend) DeleteByUsername(username string) error {
	return st.deleteBy(username)
}

func (st usersBackend) deleteBy(id any) error {
	return st.deleteApproved(id, nil)
}

func (st usersBackend) deleteApproved(id any, approval *users.WriteApproval) error {
	return st.db.Bolt.Update(func(tx *bolt.Tx) error {
		user, err := st.getBy(tx, id)
		if err != nil {
			return err
		}
		if err := st.checkApproval(tx, approval, user.ID); err != nil {
			return err
		}
		if approval != nil {
			if err := hydrateUserSecurity(tx, user); err != nil {
				return err
			}
			if user.SessionVersion != approval.TargetEpoch {
				return users.ErrStaleSecurityState
			}
			if user.Perm.Admin {
				count, err := st.countAdmins(tx)
				if err != nil {
					return err
				}
				if count <= 1 {
					return fberrors.ErrRootUserDeletion
				}
			}
		}
		if err := st.db.WithTransaction(tx).DeleteStruct(user); err != nil {
			return err
		}
		if bucket := tx.Bucket(userSecurityBucket); bucket != nil {
			return bucket.Delete(userSecurityID(user.ID))
		}
		return nil
	})
}

func (st usersBackend) CountAdmins() (int, error) {
	count := 0
	err := st.db.Bolt.View(func(tx *bolt.Tx) error {
		var err error
		count, err = st.countAdmins(tx)
		return err
	})
	return count, err
}

func (st usersBackend) countAdmins(tx *bolt.Tx) (int, error) {
	count := 0
	bucket := tx.Bucket([]byte(reflect.TypeOf(users.User{}).Name()))
	if bucket == nil {
		return 0, nil
	}
	c := bucket.Cursor()
	for _, v := c.First(); v != nil; _, v = c.Next() {
		var u users.User
		if err := st.db.Codec().Unmarshal(v, &u); err != nil {
			return 0, err
		}
		if u.Perm.Admin {
			count++
		}
	}
	return count, nil
}
