# Local release acceptance checks

Run only against a **disposable local test instance**, never production data.
The browser scripts require loopback HTTP, a Docker CLI, system Chromium at
`/usr/bin/chromium`, Python 3.11+, and Playwright:

```sh
python3 -m venv /tmp/filebrowser-qa-venv
/tmp/filebrowser-qa-venv/bin/pip install playwright
```

The browser scripts obtain the random bootstrap admin password from test-container
logs, without printing it. If the container has been recreated, supply a private
`QA_PASSWORD_FILE` (mode 0600) containing the test password. Never commit that file.
Defaults: `QA_CONTAINER=filebrowser-filebrowser-1`,
`QA_BASE_URL=http://127.0.0.1:8000`.

```sh
# 110 MiB real browser upload, optional cross-tab rotation during upload,
# request body size checks, SHA-256 download verification, UI logout revocation.
QA_ROTATE_DURING_UPLOAD=1 /tmp/filebrowser-qa-venv/bin/python tests/e2e/browser_smoke.py

# Concurrent tab initialization / rotation and cross-tab logout.
/tmp/filebrowser-qa-venv/bin/python tests/e2e/auth_tabs.py

# Cold tar backup/restore of a real initialized database, user/session and file.
# Creates only temporary directories and an isolated localhost container.
QA_IMAGE=anigx/filebrowser:preproduction-rc python3 tests/e2e/restore.py
```

Browser tests create randomly named `qa-browser-*` files and delete them. Screenshot
output defaults to `/tmp/filebrowser-qa`, overridable via `QA_OUTPUT_DIR`. The restore
test tears down its own container and temporary backup. It does not touch the running
Compose deployment. No real Cloudflare proxy or production server is contacted.
