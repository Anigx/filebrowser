package fbhttp

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	keyChallengeLimit = 1024
	keyProofTimeout   = time.Minute
)

type keyChallenge struct {
	purpose, action, sessionID, keyID, label string
	actorID, targetID                        uint
	actorEpoch, targetEpoch                  uint64
	handle                                   []byte
	session                                  webauthn.SessionData
	expires                                  time.Time
}

type keyAttempt struct {
	num     int
	expires time.Time
}

type keyChallengeCache struct {
	mu       sync.Mutex
	entries  map[string]keyChallenge
	attempts map[string]keyAttempt
	now      func() time.Time
}

func newKeyChallengeCache() *keyChallengeCache {
	return &keyChallengeCache{
		entries: make(map[string]keyChallenge), attempts: make(map[string]keyAttempt), now: time.Now,
	}
}

func (c *keyChallengeCache) put(state keyChallenge, ttl time.Duration) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.entries) >= keyChallengeLimit {
		for id, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, id)
			}
		}
		if len(c.entries) >= keyChallengeLimit {
			return "", errKeyChallengeCapacity
		}
	}
	state.expires = now.Add(ttl)
	c.entries[id] = state
	return id, nil
}

var errKeyChallengeCapacity = errors.New("too many security-key requests")

func (c *keyChallengeCache) take(id string, match func(keyChallenge) bool) (keyChallenge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.entries[id]
	if !ok {
		return keyChallenge{}, false
	}
	if !c.now().Before(state.expires) {
		delete(c.entries, id)
		return keyChallenge{}, false
	}
	if match != nil && !match(state) {
		return keyChallenge{}, false
	}
	delete(c.entries, id)
	return state, true
}

func (c *keyChallengeCache) allowAnonymous(peer string) bool {
	ip, _, err := net.SplitHostPort(peer)
	if err != nil {
		ip = peer
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	attempt := c.attempts[ip]
	if !now.Before(attempt.expires) {
		if len(c.attempts) >= keyChallengeLimit {
			for key, value := range c.attempts {
				if !now.Before(value.expires) {
					delete(c.attempts, key)
				}
			}
			if len(c.attempts) >= keyChallengeLimit {
				return false
			}
		}
		attempt = keyAttempt{expires: now.Add(time.Minute)}
	}
	if attempt.num >= 60 {
		return false
	}
	attempt.num++
	c.attempts[ip] = attempt
	return true
}
