# Security status of this fork

This document tracks security work in `Anigx/filebrowser`. It applies to the
current `master` branch only. The upstream project is archived and does not ship
security fixes; this fork is not a claim that every historical File Browser
advisory has been remediated.

## Supported deployment baseline

Use the source-built Docker/Compose deployment in [`deployment.md`](deployment.md):
loopback-only HTTP, unprivileged UID 1000, read-only container root filesystem,
dropped capabilities, `no-new-privileges`, bounded process/memory limits, and a
separate TLS/authentication proxy if remote access is needed. The command runner,
hooks, external symlink following, self-signup, and proxy authentication should
remain disabled unless their documented preconditions are met.

## Advisory-to-fix-to-test matrix

The rows below are the published upstream advisories addressed by the fork
commits `79e41df` through `a29e4df`. The test names are the regression evidence;
they must pass before a release. Advisory severity and wording are owned by the
upstream GitHub advisory records.

| Advisory | Fork mitigation | Regression evidence |
| --- | --- | --- |
| [GHSA-c4fr-5f24-4wrj](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-c4fr-5f24-4wrj) | Rejects directory uploads and never recursively cleans up a failed file write. | `TestResourcePostDirectoryDoesNotBypassDeletePermission` in `http/security_regression_test.go` |
| [GHSA-7w29-q235-57m9](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-7w29-q235-57m9) | Evaluates rules for both an in-scope symlink alias and its resolved in-scope target. | `TestRuleDeniesInScopeSymlinkAliasToProtectedTarget` in `http/rules_path_test.go` |
| [GHSA-39cx-23x9-5c8p](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-39cx-23x9-5c8p) | Checks execute permission before WebSocket upgrade and bounds command input. | `TestCommandEndpointRejectsBeforeWebSocketUpgrade` in `http/security_regression_test.go` |
| [GHSA-448h-jr2h-3vhp](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-448h-jr2h-3vhp) | Limits subtitle/text conversion reads and rejects non-regular or swapped sources. | `TestSubtitleFileHandlerRejectsOversizedSource` and `TestSubtitleFileHandlerRejectsObjectReplacedAfterStat` in `http/security_regression_test.go` |
| [GHSA-8q5j-8wcr-8v2v](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-8q5j-8wcr-8v2v) | Skips FIFOs during archive creation. | FIFO guard in `http/resource.go`, covered by the `http` package suite |
| [GHSA-r6pg-pg54-rcr5](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-r6pg-pg54-rcr5) | Revokes public shares rooted at a deleted resource, including TUS deletion. | Share deletion and TUS lifecycle coverage in `http/share_test.go` and `http/tus_lifecycle_regression_test.go` |
| [GHSA-m8v4-4w34-rrvf](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-m8v4-4w34-rrvf) | Revokes public shares rooted at a successfully renamed resource. | Share lifecycle coverage in `http/share_test.go` |
| [GHSA-4r8p-gqj2-mwgm](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-4r8p-gqj2-mwgm) | Serializes upload lifecycle mutations, validates object identity, and caps chunk progression at declared length. | `TestTusPatchEnforcesUploadLength`, `TestTusUnknownLengthChunkRollbackAndBoundary`, and `TestTusLifecycleMutationsCannotRebindReplacement` |
| [GHSA-v7vv-5wj2-gfcj](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-v7vv-5wj2-gfcj) | Persists per-token JTI sessions, validates them on each request, supports single-token logout, atomic renewal rotation, and user session epochs. | `TestJWTSessionLogoutAndRenewalRotation` and `TestSessionsPersistAndRotateAtomically` |
| [GHSA-xqp3-jq6g-x3qm](https://github.com/filebrowser/filebrowser/security/advisories/GHSA-xqp3-jq6g-x3qm) | Accepts proxy-auth identity headers only from explicitly configured peer IP/CIDR ranges. | `auth/proxy_test.go` and `http/auth_test.go` |

## Command and hook execution

The supplied runtime image intentionally does **not** include a sandbox launcher.
Therefore command execution, event hooks, and authentication hooks are unusable
there even if they are configured: `runner.NewCommand` fails closed. This is a
security property, not a missing environment variable.

Do not enable execution merely by adding a command to settings. An operator who
has a real need for it must build and independently assess a dedicated image
that includes an isolation launcher and configure `executionSandbox` as a
persisted application setting. The launcher must be an absolute command ending
in `--`; File Browser appends the requested command only after that delimiter.
The launcher must enforce all of the following itself: unprivileged identity,
no network by default, minimal read-only mounts, an explicit writable work area,
process and time limits, and no capability escalation. The hardened Compose
profile also needs to be reviewed with that launcher: many namespace-based
launchers are incompatible with `no-new-privileges`, dropped capabilities, or a
restrictive Docker seccomp profile.

There is deliberately no `EXECUTION_SANDBOX_*` Compose environment variable:
settings are persisted application state, and an environment variable that
appeared to enable a sandbox without updating those settings would be unsafe and
misleading.

## Residual risk and review boundary

- The underlying upstream has 62 published advisories. The matrix is an
  evidence list for this fork's targeted fixes, not a blanket closure statement.
- `uploadObjectID` requires filesystem `Dev` and `Ino`. Filesystems that do not
  expose a stable identity fail TUS safely rather than falling back to a
  path-only cache. Test an intended NFS, SMB, OverlayFS, or non-Linux deployment
  before enabling resumable uploads.
- For non-existent filesystem paths, rule evaluation is lexical by design: no
  target exists to resolve. Creation remains subject to the normal scope,
  permission, and parent-path checks.
- Every release must record source commit, immutable image ID, architecture, and
  the exact unit/browser/restore tests executed. A local image tag alone is not
  provenance.

## Release gate

```sh
go build ./...
go vet ./...
go test ./...
docker compose --env-file .env.example config --quiet
```

For a deployable image, also run the browser and restore acceptance tests in
[`tests/e2e/README.md`](../tests/e2e/README.md) against a disposable local
instance. Do not treat a green unit suite as a production deployment approval.
