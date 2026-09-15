> [!WARNING]
> 
> **File Browser is archived on 2026-09-01**. The last planned release has already shipped. There will be no further releases, bug fixes, or security fixes.   

<p align="center">
  <img src="./branding/banner.png" width="550"/>
</p>

File Browser provides a file managing interface within a specified directory and it can be used to upload, delete, preview and edit your files. It is a **create-your-own-cloud**-kind of software where you can just install it on your server, direct it to a path and access your files through a nice web interface.

**Background:** [Goodbye File Browser, for Real This Time](https://hacdias.com/2026/07/28/filebrowser/), July 2026.

## Security

Upstream File Browser is archived. This fork maintains a documented, targeted
hardening branch; it does **not** claim to replace an actively maintained
upstream project. The precise advisory-to-fix-to-test mapping, residual risks,
and release gate are in [`docs/security-status.md`](docs/security-status.md).

Published upstream advisories remain available from
[GitHub Security Advisories](https://github.com/filebrowser/filebrowser/security/advisories).
Report an issue in this fork using the included GitHub issue template; do not
report fork-specific fixes to the archived upstream project.

For deployment:

- **Do not expose File Browser directly to the internet.** Use a separate TLS
  proxy with its own authentication and configure trusted proxy IPs explicitly.
- **Keep commands, event hooks, and authentication hooks disabled.** The
  supplied hardened image has no execution sandbox launcher and fails closed.
  See [`docs/command-execution.md`](docs/command-execution.md).
- **Run it unprivileged in the supplied hardened Compose profile**, mounting only
  the directory intended for service. See [`docs/deployment.md`](docs/deployment.md).
- **Treat a source commit, image ID and acceptance evidence as one release.** A
  mutable local tag alone is not release provenance.

## Documentation

Documentation on how to install, configure, and build this project lives in [`docs`](docs) in this repository.

[CONTRIBUTING.md](CONTRIBUTING.md) documents how to build and develop the project, which remains useful to anyone forking it.

## License

[Apache License 2.0](LICENSE) © File Browser Contributors
