# WebAuthn Origin Startup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a Compose environment setting configure WebAuthn at startup for new and existing installations.

**Architecture:** Bind `--webauthn.origin` on the root command to the usual Viper inputs. Validate before a fresh database can be created, then apply the normalized value with the server's existing Bolt lock and enrollment guard.

**Tech Stack:** Go, Cobra/Viper, Storm/Bolt, Docker Compose, GitHub Actions.

**Spec:** [Startup origin design](../specs/2026-11-27-webauthn-origin-startup-design.md)

## Global Constraints

- `FB_WEBAUTHN_ORIGIN` must not change the authentication mode or enroll a key.
- Empty or unset does not erase the saved origin. Incompatible settings cannot overwrite the database.
- Reuse the existing public-origin validation and enrollment hostname protection.
- Do not modify the `v2.64.0` tag or existing package, nor deploy to a live service.

## Review Focus

- `FB_WEBAUTHN_ORIGIN` must resolve the dotted CLI flag using the existing precedence.
- Invalid first-run configuration must fail before creating a partial database.
- Existing registered keys must not be locked out by a hostname change.
- If the environment variable disappears, a restart must still support existing keys.
- Publish only the exact commit and container images whose checks passed.

### Task 1: Startup origin and tests

**Files:** `cmd/root.go`, `cmd/utils.go`, `cmd/webauthn.go`, `cmd/webauthn_test.go`, `tests/e2e/security_key.py`.

- [ ] Test `initViper` with `FB_WEBAUTHN_ORIGIN` and expect `v.GetString("webauthn.origin") == "https://FILES.example:443"`. Test `stageStartupWebAuthn(st, raw)` with an existing database: normalize that value to `https://files.example`, preserve it on empty or invalid input, and refuse another hostname once enrolled.
- [ ] Test that `validateStartupWebAuthn(v, false)` rejects non-HTTPS remote origins and quick-setup `--noauth` with an origin. Test the startup wrapper on a non-existent database path to prove a rejected input does not create it.
- [ ] Run `go test ./cmd -run 'TestStartupWebAuthn'`; expect red before implementation.
- [ ] Add `validateStartupWebAuthn(v *viper.Viper, databaseExists bool) error` as a `storeOptions.validateBeforeOpen` hook for the root command and recheck initialized settings under the database lock. Add `stageStartupWebAuthn(st *store, raw string) (*settings.Settings, error)` to prepare changes accepted by `validateWebAuthnConfig`. Build the handler with staged settings and persist them only after listener and handler validation, before serving. Add the `webauthn.origin` root flag and verify flag, environment, config file, and empty-value precedence.
- [ ] In the Chromium test, initialize an existing DB without a WebAuthn origin, start with `FB_WEBAUTHN_ORIGIN`, register keys, then restart the cold-restored database without the variable and sign in with a key. Keep all current virtual-key assertions.
- [ ] Run Go focused tests, build/vet, and Chromium virtual-key acceptance. Document any existing local Windows-only failures separately from Linux CI.

### Task 2: Documentation and patch release

**Files:** `.env.example`, `compose.yaml`, `docs/authentication.md`, `docs/deployment.md`, `docs/cli/filebrowser.md`, `docs/releases/v2.64.1.md`.

- [ ] Document Compose `FB_WEBAUTHN_ORIGIN` passthrough, remove/empty semantics and the manual disable route. Include only a commented example value in `.env.example`. Generate the CLI flag docs and check Compose with and without the variable.
- [ ] Verify checks and diff, commit and push to `master`, then run CI and require all six Linux gates on that SHA. Create and push `v2.64.1` only after that. Let the tag push trigger the release workflow, and check for existing runs before using manual dispatch to avoid duplicate GitHub uploads.
- [ ] Verify the public release, assets and a downloaded SHA-256 checksum. Trigger `package.yaml` for `v2.64.1`; verify both native jobs, the public image index and both plans' source labels before reporting the Docker tag.
