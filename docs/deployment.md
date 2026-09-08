# Local Docker deployment

## Build from this checkout

Requirements: Docker Engine with BuildKit and Docker Compose v2 or newer. Run
commands from the repository root. The default Dockerfile builds the frontend
with Node 24 and the committed pnpm lockfile, then builds a static Go binary with
`go.mod`/`go.sum`, and finally copies it into the existing small BusyBox/tini
runtime. Host `frontend/dist`, `node_modules`, and `filebrowser` binaries are
excluded from the build context. No prebuilt application image is required.

```sh
REV=$(git rev-parse --short HEAD)
docker build --pull=false --build-arg COMMIT_SHA="$REV" \
  --build-arg VERSION="develop-$REV" \
  -t "anigx/filebrowser:develop-$REV" -t anigx/filebrowser:develop-local .
cp .env.example .env
# Set FILEBROWSER_IMAGE in .env to the immutable revision tag just built.
docker compose config --quiet
```

Build only after source edits/tests are complete. A revision tag identifies a
clean checkout; use a distinct tag for local uncommitted modifications. The build
uses frozen dependency manifests, but rolling base tags and Alpine packages do
not promise byte-identical builds. For controlled rebuilds, pin approved base
image digests (`NODE_IMAGE` and `GO_IMAGE` build args; Alpine and BusyBox `FROM`
lines), preserve the package mirror, and record the final image ID. Go may download
a newer toolchain when a locked dependency requires one. `Dockerfile.release` is
only for GoReleaser's precompiled-binary packaging; `Dockerfile.s6` remains a
separate release variant, not the source-build deployment path.

## Initialize permissions and start

```sh
# A host bind directory must exist before Docker starts. Do not use chmod 777.
sudo install -d -m 0750 -o 1000 -g 1000 srv
docker compose up -d --wait
docker compose ps
curl --fail --silent --show-error http://127.0.0.1:8000/health
docker compose exec -T filebrowser id
```

The runtime uses UID/GID 1000:1000. Docker initializes new named volumes with the
image's `/config` and `/database` permissions. Existing volumes retain their
permissions; repair ownership deliberately while the service is stopped, rather
than running the server as root. The bind-mounted `srv` contains served files;
`/database` holds users/settings/credentials; `/config/settings.json` contains
startup settings. All three are persistent and need backups. The image copies
default settings only if absent and initializes a missing database on first run.
Retrieve the generated bootstrap password privately from local container logs
and change it immediately; never paste full logs into tickets or automation output.

Compose keeps the port on loopback, runs with a read-only root filesystem and
writable data mounts, drops all capabilities, enables no-new-privileges, limits
memory/processes, and rotates logs. There is no Redis or embedded credential.
Expose it remotely only through a separately configured trusted HTTPS proxy or
tunnel. Do not enable command execution, unsafe symlink following, or trust
arbitrary proxy headers. These protections are application defaults, not inert
Compose environment variables.

### Custom health endpoints

The bundled healthcheck reads the **default JSON config** and `FB_PORT`,
`FB_ADDRESS`, and `FB_BASE_URL`. It supports wildcard addresses and base paths.
Docker executes healthchecks separately from the entrypoint: it cannot infer
custom server CLI flags or a different `--config` file. When changing those,
explicitly set `FB_HEALTHCHECK_URL` in a Compose override, for example:

```yaml
services:
  filebrowser:
    command: ["--config", "/config/custom.json", "--port", "8080"]
    environment:
      FB_HEALTHCHECK_URL: http://127.0.0.1:8080/files/health
    ports: !override
      - "127.0.0.1:8000:8080"
```

This override assumes `custom.json` sets `baseURL` to `/files`. `!override`
requires Compose 2.24.4 or newer; otherwise edit the original port mapping rather
than merging another one. The URL must point to this container's actual endpoint,
not an external proxy. For TLS, use a hostname/certificate trusted by the runtime;
do not disable certificate validation. For a Unix socket, supply an appropriate
custom healthcheck. `FB_HEALTHCHECK_URL` controls only the probe, not the server.
`--config` is a CLI flag, not an `FB_CONFIG` setting.

## Consistent backup

Stop the application before copying Bolt DB; do not tar a live database. From the
same deployment directory/project and with the same `.env`:

```sh
install -d -m 0700 backups
umask 077
docker compose stop filebrowser
# tar runs as UID 1000, with the service's same three persistent mounts.
# -T is essential: binary archives must not pass through a pseudo-terminal.
if docker compose run --rm --no-deps -T --entrypoint tar filebrowser \
    -czf - -C / srv config database > backups/state.tar.gz.partial; then
  mv backups/state.tar.gz.partial backups/state.tar.gz
else
  rm -f backups/state.tar.gz.partial
  echo 'Backup failed; restarting the service without replacing the previous backup.' >&2
  docker compose up -d --wait
  exit 1
fi
docker compose up -d --wait
# Validate before storing an encrypted off-host copy.
gzip -t backups/state.tar.gz
sha256sum backups/state.tar.gz > backups/state.tar.gz.sha256
```

Use a unique backup name each time instead of overwriting your last known good
backup. Save the image ID/tag, Compose files, and `.env` separately alongside the
encrypted backup. Archives contain credentials and private files: restrict access,
encrypt at rest/off-host, and never commit them. `srv/`, `backups/`, and `.env` are
ignored by Git and excluded from the Docker build context.

## Restore into a fresh deployment

Restore is not a merge. Use a **new empty** deployment directory and a new Compose
project name with the same image and three mount destinations. Do not run `down
-v` against an existing installation. Keep the original stopped/intact until the
restore is verified. Copy the trusted Compose file, `.env`, and archive into the
new directory, load the saved image if needed, then run there:

```sh
# A different project name selects fresh config/database volumes.
export COMPOSE_PROJECT_NAME=filebrowser-restore
sudo install -d -m 0750 -o 1000 -g 1000 srv
sha256sum -c backups/state.tar.gz.sha256
gzip -t backups/state.tar.gz
# This creates fresh named volumes but does not start File Browser first.
docker compose run --rm --no-deps -T --entrypoint tar filebrowser \
  -xzf - --no-same-owner -C / < backups/state.tar.gz
docker compose up -d --wait
curl --fail --silent --show-error http://127.0.0.1:8000/health
docker compose exec -T filebrowser id
```

Only extract a trusted archive created by the backup procedure. Confirm the
restored login, settings, permissions, and representative file checksums before
reopening access. Stop the old instance first (the default loopback port cannot
be shared), or use a separate port for the restore drill. If old files have
nonstandard ownership, normalize them to the runtime UID/GID deliberately; do not
relax permissions globally. The archive restores files/settings, not the image,
Compose configuration, external proxy, or environment secrets.

### Local verification without production effects

`sh docker/test-healthcheck.sh IMAGE_TAG` checks probe URL resolution and failure
propagation in the BusyBox runtime using a stubbed HTTP client (not network
availability). Its disposable test-only `/tmp` allows executing that stub; the
production Compose `/tmp` remains `noexec`.

`sh docker/test-deployment.sh IMAGE_TAG` runs the backup/restore tar workflow using
a disposable Compose project, temporary bind directory, synthetic data, and fresh
named volumes, with **no published ports**. It validates the resulting file and
ownership and removes only its own resources. Run it with the newly built image
before deployment. This archive test complements (does not replace) a full
`up --wait`, HTTP/login, and real-backup restore drill.

## Transfer an image without a registry

```sh
IMAGE=anigx/filebrowser:develop-REVISION
# Use the exact built tag; the receiving host must use a compatible platform.
docker image inspect "$IMAGE" --format '{{.Id}} {{.Os}}/{{.Architecture}}'
docker save "$IMAGE" | gzip > filebrowser-image.tar.gz
sha256sum filebrowser-image.tar.gz > filebrowser-image.tar.gz.sha256
scp filebrowser-image.tar.gz filebrowser-image.tar.gz.sha256 user@host:/safe/path/
# On the destination, in /safe/path:
sha256sum -c filebrowser-image.tar.gz.sha256
gzip -dc filebrowser-image.tar.gz | docker load
# Set FILEBROWSER_IMAGE in the deployment's untracked .env to that exact tag.
docker compose config --quiet
docker compose up -d --wait --pull never
```

`docker save/load` transfers image metadata and layers, not volumes or bind data;
use the separate backup/restore procedure for those. Keep a checksum and verify
the loaded image ID against the source over a trusted channel.

---

## Self-Registration (Signup)

File Browser allows you to enable user self-registration (signup). This can be enabled via **Settings → Global Settings**, or with `filebrowser config set --signup`. Self-registered users inherit the configured **user defaults**, including the scope.

> [!WARNING]
>
> By default, the user scope is the server's root, so a self-registered user could read,
> modify, and delete every file File Browser serves. To prevent this, either:
>
> a. Enable `createUserDir` so each user gets their own directory; or
> b. If users are meant to share files, set the default scope to something other than the root.

## Fail2ban

File Browser does not natively support protection against brute force attacks. Therefore, we suggest using something like [fail2ban](https://github.com/fail2ban/fail2ban), which takes care of that by tracking the logs of your File Browser instance. For more information on how fail2ban works, please refer to their [wiki](https://github.com/fail2ban/fail2ban/wiki).

### Filter Configuration

An example filter configuration targeted at matching File Browser's logs.

```ini
[INCLUDES]
before = common.conf

[Definition]
datepattern = `^%%Y\/%%m\/%%d %%H:%%M:%%S`
failregex   = `\/api\/login: 403 <HOST> *`
```

### Jail Configuration

An example jail configuration. You should fill it with the path of the logs of File Browser, as well as the port where it is running at.

```ini
[filebrowser]

enabled = true
port = [your_port]
filter = filebrowser
logpath = [your_log_path]
maxretry = 10
bantime = 10m
findtime = 10m
banaction = iptables-allports
banaction_allports = iptables-allports
```
