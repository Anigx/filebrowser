package fbhttp

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/filebrowser/filebrowser/v2/auth"
	fberrors "github.com/filebrowser/filebrowser/v2/errors"
	"github.com/filebrowser/filebrowser/v2/users"
)

type securityKeyAuth struct {
	wa     *webauthn.WebAuthn
	origin string
	cache  *keyChallengeCache
}

type webAuthnUser struct {
	name  string
	state users.SecurityState
}

func (u webAuthnUser) WebAuthnID() []byte          { return u.state.Handle }
func (u webAuthnUser) WebAuthnName() string        { return u.name }
func (u webAuthnUser) WebAuthnDisplayName() string { return u.name }
func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	keys := make([]webauthn.Credential, 0, len(u.state.Keys))
	for _, key := range u.state.Keys {
		keys = append(keys, key.Credential)
	}
	return keys
}

func (a *securityKeyAuth) guard(fn handleFunc, anonymous bool) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		w.Header().Set("Cache-Control", "no-store")
		if a == nil || d.settings.AuthMethod != auth.MethodJSONAuth {
			return http.StatusNotFound, nil
		}
		if origin := r.Header.Get("Origin"); origin != "" && !a.matchesOrigin(origin) {
			return http.StatusForbidden, nil
		}
		if anonymous && !a.cache.allowAnonymous(r.RemoteAddr) {
			return http.StatusTooManyRequests, nil
		}
		d.authNoRenew = true
		return fn(w, r, d)
	}
}

func (a *securityKeyAuth) matchesOrigin(origin string) bool {
	normalized, _, err := auth.NormalizeWebAuthnOrigin(origin)
	return err == nil && normalized == a.origin
}

func decodeKeyRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Body == nil {
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	return dec.Decode(out) == nil && dec.Decode(new(any)) == io.EOF
}

type keyFinishRequest struct {
	ID         string          `json:"id"`
	Credential json.RawMessage `json:"credential"`
}

type keyStartResponse struct {
	ID        string `json:"id"`
	PublicKey any    `json:"publicKey"`
}

func (a *securityKeyAuth) start(w http.ResponseWriter, r *http.Request, challenge keyChallenge, options any) (int, error) {
	id, err := a.cache.put(challenge, auth.WebAuthnTimeout)
	if errors.Is(err, errKeyChallengeCapacity) {
		return http.StatusTooManyRequests, nil
	}
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return renderJSON(w, r, keyStartResponse{ID: id, PublicKey: options})
}

func (a *securityKeyAuth) beginAssertion(w http.ResponseWriter, r *http.Request, c keyChallenge, user webAuthnUser) (int, error) {
	options, session, err := a.wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return http.StatusForbidden, nil
	}
	c.session = *session
	return a.start(w, r, c, options.Response)
}

func (a *securityKeyAuth) loginStart(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var req struct {
		Username  string `json:"username"`
		ReCaptcha string `json:"recaptcha"`
	}
	if !decodeKeyRequest(w, r, &req) || req.Username == "" || len(req.Username) > users.MaxSecurityKeyUsernameBytes {
		return http.StatusBadRequest, nil
	}
	provider, err := d.store.Auth.Get(auth.MethodJSONAuth)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	pwAuth, ok := provider.(*auth.JSONAuth)
	if !ok {
		return http.StatusInternalServerError, nil
	}
	if valid, err := pwAuth.CheckReCaptcha(req.ReCaptcha); err != nil || !valid {
		return http.StatusForbidden, nil
	}
	account, err := d.store.Users.Get(d.server.Root, d.server.FollowExternalSymlinks, req.Username)
	if err != nil || !account.KeyOnly {
		return http.StatusForbidden, nil
	}
	state, err := d.store.SecurityKeys.Get(account.ID)
	if err != nil || state.Epoch != account.SessionVersion || len(state.Keys) == 0 {
		return http.StatusForbidden, nil
	}
	c := keyChallenge{purpose: "login", actorID: account.ID, actorEpoch: state.Epoch}
	return a.beginAssertion(w, r, c, webAuthnUser{account.Username, state})
}

func (a *securityKeyAuth) assertion(d *data, c keyChallenge, raw []byte) (*users.User, bool) {
	user, err := d.store.Users.Get(d.server.Root, d.server.FollowExternalSymlinks, c.actorID)
	if err != nil || !user.KeyOnly || user.SessionVersion != c.actorEpoch {
		return nil, false
	}
	state, err := d.store.SecurityKeys.Get(user.ID)
	if err != nil || state.Epoch != c.actorEpoch {
		return nil, false
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil || !a.matchesOrigin(parsed.Response.CollectedClientData.Origin) {
		return nil, false
	}
	updated, err := a.wa.ValidateLogin(webAuthnUser{user.Username, state}, c.session, parsed)
	if err != nil || updated == nil || updated.Authenticator.CloneWarning {
		return nil, false
	}
	for _, key := range state.Keys {
		if bytes.Equal(key.Credential.ID, updated.ID) {
			err := d.store.SecurityKeys.UpdateCredential(user.ID, c.actorEpoch, key.Credential, *updated)
			return user, err == nil
		}
	}
	return nil, false
}

func (a *securityKeyAuth) loginFinish(tokenExpiry time.Duration) handleFunc {
	return func(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
		var req keyFinishRequest
		if !decodeKeyRequest(w, r, &req) {
			return http.StatusBadRequest, nil
		}
		c, ok := a.cache.take(req.ID, func(c keyChallenge) bool { return c.purpose == "login" })
		if !ok {
			return http.StatusForbidden, nil
		}
		account, ok := a.assertion(d, c, req.Credential)
		if !ok {
			return http.StatusForbidden, nil
		}
		return printToken(w, d, account, tokenExpiry, "")
	}
}

type keyAction struct {
	Action   string `json:"action"`
	TargetID uint   `json:"targetID"`
	KeyID    string `json:"keyID"`
}

func keyOperationAllowed(user *users.User, op keyAction) bool {
	switch op.Action {
	case "create-user":
		return op.TargetID == 0 && op.KeyID == "" && user.Perm.Admin
	case "delete-user", "update-user":
		return op.TargetID != 0 && op.KeyID == "" && (op.TargetID == user.ID || user.Perm.Admin)
	case "add-key":
		return op.TargetID == user.ID && op.KeyID == ""
	case "reset-keys":
		return op.TargetID != 0 && op.KeyID == "" && (op.TargetID == user.ID || user.Perm.Admin)
	case "remove-key":
		return op.TargetID != 0 && op.KeyID != "" && (op.TargetID == user.ID || user.Perm.Admin)
	default:
		return false
	}
}

func keyTargetEpoch(d *data, target uint) (uint64, error) {
	if target == 0 {
		return 0, nil
	}
	s, err := d.store.SecurityKeys.Get(target)
	return s.Epoch, err
}

func (a *securityKeyAuth) verifyStart(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var op keyAction
	if !decodeKeyRequest(w, r, &op) || len(op.KeyID) > 2048 {
		return http.StatusBadRequest, nil
	}
	if !d.user.KeyOnly || !keyOperationAllowed(d.user, op) {
		return http.StatusForbidden, nil
	}
	state, err := d.store.SecurityKeys.Get(d.user.ID)
	if err != nil || state.Epoch != d.user.SessionVersion {
		return http.StatusForbidden, nil
	}
	targetEpoch, err := keyTargetEpoch(d, op.TargetID)
	if err != nil {
		return http.StatusForbidden, nil
	}
	challenge := keyChallenge{
		purpose: "verify", action: op.Action, keyID: op.KeyID, sessionID: d.sessionID,
		actorID: d.user.ID, actorEpoch: state.Epoch, targetID: op.TargetID, targetEpoch: targetEpoch,
	}
	return a.beginAssertion(w, r, challenge, webAuthnUser{d.user.Username, state})
}

func matchKeyActor(d *data, c keyChallenge, purpose string) bool {
	return c.purpose == purpose && c.actorID == d.user.ID && c.actorEpoch == d.user.SessionVersion &&
		c.sessionID == d.sessionID
}

func accountWriteApproval(d *data, targetEpoch uint64) users.WriteApproval {
	return users.WriteApproval{
		ActorID: d.user.ID, ActorEpoch: d.user.SessionVersion, SessionID: d.sessionID, TargetEpoch: targetEpoch,
	}
}

func (a *securityKeyAuth) verifyFinish(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var req keyFinishRequest
	if !decodeKeyRequest(w, r, &req) {
		return http.StatusBadRequest, nil
	}
	c, ok := a.cache.take(req.ID, func(c keyChallenge) bool { return matchKeyActor(d, c, "verify") })
	if !ok {
		return http.StatusForbidden, nil
	}
	if _, ok := a.assertion(d, c, req.Credential); !ok {
		return http.StatusForbidden, nil
	}
	epoch, err := keyTargetEpoch(d, c.targetID)
	if err != nil || epoch != c.targetEpoch {
		return http.StatusForbidden, nil
	}
	c.purpose, c.session = "proof", webauthn.SessionData{}
	proof, err := a.cache.put(c, keyProofTimeout)
	if err != nil {
		return http.StatusTooManyRequests, nil
	}
	return renderJSON(w, r, map[string]string{"proof": proof})
}

func (a *securityKeyAuth) fresh(d *data, op keyAction, epoch uint64, password, proof string) bool {
	if !d.user.KeyOnly {
		return users.CheckPwd(password, d.user.Password)
	}
	if a == nil || !keyOperationAllowed(d.user, op) {
		return false
	}
	_, ok := a.cache.take(proof, func(c keyChallenge) bool {
		return matchKeyActor(d, c, "proof") && c.action == op.Action && c.targetID == op.TargetID &&
			c.targetEpoch == epoch && c.keyID == op.KeyID
	})
	return ok
}

func (a *securityKeyAuth) registerStart(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var req struct {
		Label           string `json:"label"`
		CurrentPassword string `json:"currentPassword"`
		Proof           string `json:"proof"`
	}
	if !decodeKeyRequest(w, r, &req) {
		return http.StatusBadRequest, nil
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || len(label) > 80 {
		return http.StatusBadRequest, nil
	}
	if len(d.user.Username) > users.MaxSecurityKeyUsernameBytes {
		return http.StatusBadRequest, errors.New("username is too long for security-key login")
	}
	state, err := d.store.SecurityKeys.Get(d.user.ID)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if state.Epoch != d.user.SessionVersion || d.user.LockPassword && !d.user.KeyOnly ||
		!a.fresh(d, keyAction{Action: "add-key", TargetID: d.user.ID}, state.Epoch, req.CurrentPassword, req.Proof) {
		return http.StatusForbidden, nil
	}
	if len(state.Keys) >= users.MaxSecurityKeys {
		return http.StatusBadRequest, nil
	}
	if len(state.Handle) == 0 {
		state.Handle = make([]byte, 32)
		if _, err := rand.Read(state.Handle); err != nil {
			return http.StatusInternalServerError, err
		}
	}
	excluded := make([]protocol.CredentialDescriptor, 0, len(state.Keys))
	for _, key := range state.Keys {
		excluded = append(excluded, key.Credential.Descriptor())
	}
	options, session, err := a.wa.BeginRegistration(webAuthnUser{d.user.Username, state}, webauthn.WithExclusions(excluded))
	if err != nil {
		return http.StatusInternalServerError, nil
	}
	c := keyChallenge{purpose: "register", actorID: d.user.ID, actorEpoch: state.Epoch,
		sessionID: d.sessionID, label: label, session: *session, handle: state.Handle}
	return a.start(w, r, c, options.Response)
}

func (a *securityKeyAuth) registerFinish(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var req keyFinishRequest
	if !decodeKeyRequest(w, r, &req) {
		return http.StatusBadRequest, nil
	}
	c, ok := a.cache.take(req.ID, func(c keyChallenge) bool { return matchKeyActor(d, c, "register") })
	if !ok {
		return http.StatusForbidden, nil
	}
	state, err := d.store.SecurityKeys.Get(d.user.ID)
	if err != nil || state.Epoch != c.actorEpoch {
		return http.StatusForbidden, nil
	}
	state.Handle = c.handle
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil || !a.matchesOrigin(parsed.Response.CollectedClientData.Origin) {
		return http.StatusForbidden, nil
	}
	credential, err := a.wa.CreateCredential(webAuthnUser{d.user.Username, state}, c.session, parsed)
	if err != nil {
		return http.StatusForbidden, nil
	}
	err = d.store.SecurityKeys.ChangeApproved(accountWriteApproval(d, c.actorEpoch), d.user.ID, func(current *users.User, state *users.SecurityState) error {
		if current.LockPassword && len(state.Keys) == 0 {
			return fberrors.ErrPermissionDenied
		}
		state.Handle = c.handle
		state.Keys = append(state.Keys, users.SecurityKey{Label: c.label, CreatedAt: time.Now().UTC(), Credential: *credential})
		return nil
	})
	if err != nil {
		return securityKeyChangeError(err)
	}
	return http.StatusNoContent, nil
}

func (a *securityKeyAuth) list(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	state, err := d.store.SecurityKeys.Get(d.raw.(uint))
	if err != nil {
		return errToStatus(err), err
	}
	type keySummary struct {
		ID        string    `json:"id"`
		Label     string    `json:"label"`
		CreatedAt time.Time `json:"createdAt"`
	}
	result := make([]keySummary, 0, len(state.Keys))
	for _, key := range state.Keys {
		result = append(result, keySummary{base64.RawURLEncoding.EncodeToString(key.Credential.ID), key.Label, key.CreatedAt})
	}
	return renderJSON(w, r, map[string][]keySummary{"keys": result})
}

func (a *securityKeyAuth) remove(w http.ResponseWriter, r *http.Request, d *data) (int, error) {
	var req struct {
		KeyID           string `json:"keyID"`
		CurrentPassword string `json:"currentPassword"`
		Proof           string `json:"proof"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeKeyRequest(w, r, &req) {
		return http.StatusBadRequest, nil
	}
	target := d.raw.(uint)
	state, err := d.store.SecurityKeys.Get(target)
	if err != nil {
		return errToStatus(err), err
	}
	action := "remove-key"
	if req.KeyID == "" {
		action = "reset-keys"
	}
	if !a.fresh(d, keyAction{Action: action, TargetID: target, KeyID: req.KeyID}, state.Epoch, req.CurrentPassword, req.Proof) {
		return http.StatusForbidden, nil
	}
	if len(state.Keys) == 0 {
		return http.StatusBadRequest, nil
	}
	var passwordHash string
	var removeID []byte
	if req.KeyID == "" {
		passwordHash, err = users.NewPasswordAfterKeyRemoval(req.NewPassword, state.RetiredHash, d.settings.MinimumPasswordLength)
		if err != nil {
			return http.StatusBadRequest, err
		}
	} else {
		removeID, err = base64.RawURLEncoding.DecodeString(req.KeyID)
		if err != nil || len(state.Keys) < 2 || req.NewPassword != "" {
			return http.StatusBadRequest, nil
		}
	}
	err = d.store.SecurityKeys.ChangeApproved(accountWriteApproval(d, state.Epoch), target, func(current *users.User, state *users.SecurityState) error {
		if req.KeyID == "" {
			if target == d.user.ID && current.LockPassword {
				return fberrors.ErrPermissionDenied
			}
			current.Password = passwordHash
			state.Keys = nil
			return nil
		}
		for i, key := range state.Keys {
			if bytes.Equal(key.Credential.ID, removeID) {
				state.Keys = append(state.Keys[:i], state.Keys[i+1:]...)
				return nil
			}
		}
		return fberrors.ErrNotExist
	})
	if err != nil {
		return securityKeyChangeError(err)
	}
	return http.StatusNoContent, nil
}

func securityKeyChangeError(err error) (int, error) {
	switch {
	case errors.Is(err, users.ErrStaleSecurityState):
		return http.StatusConflict, nil
	case errors.Is(err, fberrors.ErrPermissionDenied):
		return http.StatusForbidden, nil
	case errors.Is(err, fberrors.ErrNotExist):
		return http.StatusNotFound, nil
	case errors.Is(err, fberrors.ErrInvalidDataType):
		return http.StatusBadRequest, nil
	}
	return http.StatusInternalServerError, err
}
