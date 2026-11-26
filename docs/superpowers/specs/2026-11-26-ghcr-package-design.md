# Public File Browser OCI package

Publish the hardened File Browser image with passwordless FIDO2 from the exact
existing `v2.64.0` source tag at `ghcr.io/anigx/filebrowser:2.64.0`. It should
work with the existing Compose file without weakening any runtime defaults.
Keep the existing GitHub release and tag intact; do not deploy the package.

## Approaches

- **GitHub Container Registry (selected):** Install with Docker or Compose,
  using GitHub Packages in the fork owner's namespace. No reference to upstream
  publishing accounts is required.
- Native Debian and Windows installers do not support the existing Compose
  deployment and are outside this package request.

## Design and publication

- Add a reusable, manually dispatched fork-only workflow for an existing
  public GitHub release. Resolve the release tag and require a matching
  successful `ci.yaml` run with deployment and browser acceptance. Build the
  hardened image using the tagged source and existing Dockerfile.
- Build and smoke-test `linux/amd64` and `linux/arm64` on native Linux runners.
  Promote the tested images by digest into one versioned tag; do not publish a
  mutable `latest` alias. For a tag already in the registry, fail rather than
  replace it. Record the source commit and platform and index digests.
- Document `docker pull` and the Compose `FILEBROWSER_IMAGE` setting. Verify the
  final manifest and unauthenticated registry access. If GitHub initializes
  the first package as private, use the supported GitHub package settings to
  change its visibility to public; never claim anonymous access before testing
  it. No credentials or changes to package visibility outside this package.
