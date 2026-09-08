#!/bin/sh
# Isolated tar backup/restore verification. Never starts the application server.
# Container shell, not host shell, must expand the single-quoted scripts.
# shellcheck disable=SC2016
set -eu
IMAGE=${1:?Usage: sh docker/test-deployment.sh IMAGE_TAG}
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
PROJECT="filebrowser-archive-check-$(basename "$TMP" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-')"
export FILEBROWSER_IMAGE="$IMAGE"
compose() {
  docker compose --env-file "$ROOT/.env.example" -p "$PROJECT" \
    -f "$ROOT/compose.yaml" -f "$TMP/override.yaml" "$@"
}
cleanup() {
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
mkdir "$TMP/srv"
cat > "$TMP/override.yaml" <<EOF
services:
  filebrowser:
    ports: !reset []
    volumes:
      - $TMP/srv:/srv
EOF
# Only this temporary bind directory is touched by the root helper.
docker run --rm --network none --read-only --cap-drop ALL --cap-add CHOWN \
  --user 0:0 --mount "type=bind,src=$TMP/srv,dst=/srv" \
  --entrypoint sh "$IMAGE" -c 'chown 1000:1000 /srv'
compose config --quiet
compose run --rm --no-deps -T --entrypoint sh filebrowser -ec '
  test "$(id -u):$(id -g)" = 1000:1000
  printf "%s\n" archive-test-file > /srv/verify.txt
  printf "%s\n" synthetic-database > /database/verify.txt
  printf "%s\n" synthetic-config > /config/verify.txt
' >/dev/null
umask 077
compose run --rm --no-deps -T --entrypoint tar filebrowser \
  -czf - -C / srv config database > "$TMP/state.tar.gz"
gzip -t "$TMP/state.tar.gz"
# Remove only disposable test data, then recreate fresh named volumes.
compose run --rm --no-deps -T --entrypoint sh filebrowser \
  -ec 'rm /srv/verify.txt' >/dev/null
compose down --volumes --remove-orphans >/dev/null
compose run --rm --no-deps -T --entrypoint tar filebrowser \
  -xzf - --no-same-owner -C / < "$TMP/state.tar.gz"
compose run --rm --no-deps -T --entrypoint sh filebrowser -ec '
  test "$(cat /srv/verify.txt)" = archive-test-file
  test "$(cat /database/verify.txt)" = synthetic-database
  test "$(cat /config/verify.txt)" = synthetic-config
  for file in /srv/verify.txt /database/verify.txt /config/verify.txt; do
    test "$(stat -c %u:%g "$file")" = 1000:1000
  done
  rm /srv/verify.txt
' >/dev/null
printf '%s\n' 'PASS: isolated backup/restore of srv, config, database; runtime and restored ownership 1000:1000.'
