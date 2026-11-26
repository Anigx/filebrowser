# Passwordless security-key login

## Intent and scope

Let users of the `Anigx/filebrowser` fork replace their File Browser password
with a FIDO2 security key, such as a compatible YubiKey. Enrollment is opt-in per
account. Unenrolled accounts and existing deployments must keep working as they
do today. Enrolled accounts must not have a password-only fallback. Turning off
security-key login requires explicitly setting a different password that meets
the instance's password policy.

## Approaches

- **Native WebAuthn with standard FIDO2 credentials (selected).** File Browser
  manages enrollment and validates security-key sign-in. Users can register a
  compatible backup from another vendor. The browser may offer other compatible
  authenticators; File Browser does not certify the physical device or its brand.
- **Native WebAuthn with Yubico attestation enforcement.** This would add vendor
  trust anchors and reject some browsers, models, or replacements. The user chose
  interoperability instead of a Yubico-only restriction.
- **External proxy login.** An identity provider could perform WebAuthn via the
  fork's existing trusted-proxy support, but File Browser would not manage the
  key or the switch back to a new password. This does not meet the native-login
  requirement. Yubico OTP and password-plus-key MFA are also out of scope.

## User experience

- The operator enables WebAuthn for the built-in JSON authentication mode with
  one explicitly configured public HTTPS origin. The relying-party ID is its
  exact hostname; no wildcard, forwarded-header inference, or path in the
  origin. Support `http://localhost` with an optional port for local tests.
- In **Personal settings → Security keys**, the signed-in user confirms their
  current password and registers a key. Explain before enrollment that this
  replaces password login, and recommend adding a backup. Failed or canceled
  enrollment leaves their account unchanged. A password lock prevents
  self-enrollment or switching back regardless of role, as in the profile UI;
  it does not prevent managing backup keys while staying in key-only mode.
- At **Login**, enter the username and choose **Sign in with a security key**;
  the browser handles key interaction and user verification (e.g. YubiKey PIN).
  Discoverable credentials are not required. The standard password form remains
  for unenrolled accounts. A password must never sign in an enrolled account.
- An enrolled user can list, add and remove their own keys, using a fresh
  assertion from an enrolled key for any change. Removing the last key requires
  the explicit **Switch back to password** operation: verify an enrolled key,
  set and confirm a new password, and remove all credentials as one operation.
  A successful change to credentials signs out existing sessions, including the
  tab performing the change, so the user signs in again with the new method.
  Revocation applies to subsequent authenticated requests and renewals; this
  change does not introduce active connection teardown.
- Administrators can remove another user's key from the user-management UI.
  If other keys remain, the account stays key-only. To remove the last key
  (or reset all keys at once), the administrator must set and confirm a new
  password for the affected account. The admin must freshly verify their own
  sign-in method (current password or registered key); a session alone cannot
  authorize these actions. The action revokes the affected user's sessions.
  Admins cannot enroll a key on someone else's behalf.
- If all keys are lost and no other administrator can help, a local operator with
  database access can use a dedicated CLI recovery command against the stopped
  instance. A new password is required, read without exposing it on the command
  line. There is no recovery-code fallback. Enrollment, switching back and both
  recovery paths are covered by the deployment documentation along with backups,
  origin changes and how to convey an admin-set password privately.

## Authentication and state

- Use a maintained Go WebAuthn library rather than custom cryptographic
  verification. Require user verification on registration and assertions.
  Verify challenge, configured origin and RP ID, credential ownership, and
  signature. Handle the credential counter and authenticator flags with the
  library's supported semantics; do not treat a conforming zero counter as
  proof of failure. Keep existing reCAPTCHA checks when configured.
- Keep short-lived, bounded, one-time server-side challenges and proofs. Bind
  them to the purpose and immutable account ID or stable opaque user handle;
  for authenticated flows, also bind them to the live session and account epoch.
  Reject expired, mismatched, replayed and stale requests. Bound anonymous
  work and input. Return generic login errors and do not expose an account-status
  lookup; the username-based login-start ceremony itself sends credential
  descriptors to the browser and is not a guarantee of enrollment privacy.
- Store registered public credentials, metadata and an opaque user handle in
  private Bolt state, never in generic user API payloads or user exports. Allow
  a small bounded number of keys. Atomically transition between password and
  key-only modes, update relevant credential state and advance the account
  epoch. Do not leave a usable legacy password in the normal user record:
  replace it with a hash of a discarded random value to preserve the existing
  user-record invariant. Keep the retired hash in private state only to reject
  it as the new password on deactivation or recovery, then discard it.
- Use the existing JWT/JTI session path only after a validated assertion.
  Existing API password/profile writes and CLI `users update` must not make
  an enrolled account password-capable or overwrite private authentication
  state. Sensitive operations that currently require the caller's password
  must still require fresh proof from an enrolled caller; use a one-time
  assertion bound to the action, target and session, not a session-only
  bypass. Unenrolled callers keep their existing password checks.
- Prevent supported configuration commands from removing the origin, changing
  its hostname or leaving JSON auth while any keys are enrolled. A same-host
  HTTPS port change is allowed. If WebAuthn is unavailable in a running JSON
  instance, enrolled accounts stay key-only and unenrolled accounts keep
  password access. An incompatible non-JSON configuration with enrolled users
  must fail closed rather than give them a different login path.
- Reject user exports that would silently drop enrollments; direct the operator
  to a cold database backup instead. Reject bulk imports that would replace or
  overwrite enrolled users before writing changes. Unrelated imports still
  work. Normal account deletion removes that account's credentials. Database
  restores preserve enrollments; older binaries are not supported against the
  upgraded database.

## Acceptance and publication

- Test the credential and password lifecycle, admin removal and reset, access to
  sensitive operations, backup keys, existing login modes, challenge replay/expiry, origin and user
  binding, persisted credentials across restarts, and revocation of stale
  sessions. Verify browser registration and login with Chromium's virtual
  FIDO2 authenticator in a disposable instance. A real YubiKey test is a
  separate hardware check; report explicitly whether it was performed.
- Run the fork's release gate, frontend tests and build, and disposable
  deployment/browser/restore checks required by `docs/security-status.md`
  before publishing a deployable artifact. Capture provenance and actual
  checks run. Commit and push the verified changes, then publish a new minor
  release on **Anigx/filebrowser** with downloadable binaries, checksums, and
  notes. Make the tagged workflow depend on frontend tests as well as the other
  CI checks. Do not use or publish to upstream Docker Hub or Homebrew targets,
  and do not deploy to a live service.
