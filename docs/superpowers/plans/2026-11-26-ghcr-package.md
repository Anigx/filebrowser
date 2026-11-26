# Public OCI Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the existing `v2.64.0` release as a tested, publicly pullable Docker package.

**Architecture:** A manually initiated workflow first checks the existing release and successful tag CI, then checks out the verified source commit. Native AMD64 and ARM64 runners build and test the tagged image; the publishing job uses the pushed digests to assemble the release index.

**Tech Stack:** GitHub Actions, GitHub CLI, Docker Buildx, Github Container Registry.

**Spec:** [2026-11-26-ghcr-package-design.md](../specs/2026-11-26-ghcr-package-design.md)

## Global Constraints

- Repo/registry: `Anigx/filebrowser` and `ghcr.io/anigx/filebrowser`.
- Publish `2.64.0` from `v2.64.0`; no `latest`, no replacement and no deployment.
- Require successful `ci.yaml` job results for the source commit; package jobs run on native `linux/amd64` and `linux/arm64`.
- Record the source SHA, the image and index digests. Verify visibility without credentials.

## Review Focus

- Draft releases, missing tags and green CI from a different commit do not qualify.
- A failure on either architecture prevents the version tag from being published.
- Private registry responses and network errors are not mistaken for anonymous access or permission to overwrite.
- The version must be taken from the selected source tag, not from the workflow commit.

### Task 1: Release package workflow

**Files:** `.github/workflows/package.yaml`, `docker/package-release.sh`, `docker/test-package-release.sh`, `docs/deployment.md`.

- [ ] Write POSIX shell tests for `package-release.sh validate-tag TAG` and `package-release.sh verify-ci TAG SHA`, covering strict `vMAJOR.MINOR.PATCH`, absent/draft release, mismatched SHA, failed acceptance, and a green `ci.yaml` tag run. Test inputs are stubbed GitHub CLI responses; no writes to GitHub.
- [ ] Run `sh docker/test-package-release.sh`; expect failure because the helper does not exist.
- [ ] Add the helper and workflow input `release_tag`. Resolve tag with `git rev-parse TAG^{commit}` and require matching pushed tag CI, the public release, and the acceptance job's successful conclusion. Use the resolved SHA for both checkouts.
- [ ] On each native runner build the Dockerfile with source, revision and version OCI labels and args, run the existing healthcheck/Compose smoke tests, push by digest, and pass the digests to the publish job via artifacts. Set a shared concurrency group; resolve the version tag through GHCR, create it only on an explicit not-found, and compare both platform manifest digests afterward.
- [ ] Document Compose usage. Run `sh docker/test-package-release.sh`, `actionlint .github/workflows/package.yaml` and `git diff --check`; expect success. Commit and push the verified workflow.

### Task 2: Build, publish, and confirm visibility

**Files:** Any package evidence linked from `docs/releases/v2.64.0.md`.

- [ ] Trigger `gh workflow run package.yaml --ref master -f release_tag=v2.64.0`. Verify target SHA `d1e03084fe7c8ecdf9a14995b1f1285b43248ede` and both native smoke checks in run output.
- [ ] Inspect the version manifest. Confirm size and source/commit metadata, and query anonymous GHCR for a 200 version manifest and both target platforms. If GitHub initializes a private package, make it public using supported package settings or report the exact manual step if it cannot be performed.
- [ ] Report the immutable digest, package URL and the validation run; do not report private visibility as a public release.
