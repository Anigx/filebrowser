# Configure WebAuthn at startup

Let a Compose operator set `FB_WEBAUTHN_ORIGIN` and start File Browser normally,
without a maintenance container. This configures the origin, not a user's key;
users continue to enroll their own keys through their profile.

## Approaches

- **Native startup configuration (selected):** File Browser reads a root
  `--webauthn.origin` option through its existing flag/environment/config
  precedence. The server already owns the Bolt database lock, so it can safely
  apply and persist the validated origin before serving.
- **Docker entrypoint command:** An extra process would need to distinguish
  first startup from an existing database and reproduce the same validation.
  It would not help non-Docker users and would couple the feature to an image.

## Behavior

- A nonempty value applies to new and existing installations using JSON auth.
  Reuse exact WebAuthn origin normalization and the existing configuration
  guard for enrolled accounts. Invalid, incompatible or unsafe changes stop
  startup, without replacing the stored value or changing auth mode.
- Missing or empty input does not change the database. Setting the same
  canonical origin is idempotent. Removing the variable does not unenroll
  keys or disable WebAuthn. Existing instances with no startup setting retain
  all prior behavior.
- Keep the current release and Docker package immutable. Commit and push the
  change, run Linux/race, browser and deployment checks, and publish patch
  release `v2.64.1` and Docker tag `2.64.1` for Linux AMD64 and ARM64 only after
  the checks pass. Do not deploy to the user's server.
