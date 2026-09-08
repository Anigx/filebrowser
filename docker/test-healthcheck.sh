#!/bin/sh
# Validate the BusyBox probe's URL resolution without starting an app or logging secrets.
set -eu
IMAGE=${1:?Usage: sh docker/test-healthcheck.sh IMAGE_TAG}
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
docker run --rm -i --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user 1000:1000 \
  --tmpfs /tmp:rw,exec,uid=1000,gid=1000 --tmpfs /config:rw,uid=1000,gid=1000 \
  --mount "type=bind,src=$ROOT/docker/alpine/healthcheck.sh,dst=/tested-healthcheck.sh,readonly" \
  --entrypoint sh "$IMAGE" -es <<'TEST'
mkdir /tmp/bin
cat > /tmp/bin/wget <<'MOCK'
#!/bin/sh
printf '%s\n' "$*"
exit "${MOCK_WGET_STATUS:-0}"
MOCK
chmod +x /tmp/bin/wget
export PATH=/tmp/bin:$PATH
printf '%s\n' '{"port":80,"address":"","baseURL":""}' > /config/settings.json
test "$(sh /tested-healthcheck.sh)" = '-q --spider -- http://127.0.0.1:80/health'
printf '%s\n' '{"port":8081,"address":"0.0.0.0","baseURL":"/files/"}' > /config/settings.json
test "$(sh /tested-healthcheck.sh)" = '-q --spider -- http://127.0.0.1:8081/files/health'
test "$(FB_PORT=8082 FB_ADDRESS=:: FB_BASE_URL=/custom sh /tested-healthcheck.sh)" = '-q --spider -- http://[::1]:8082/custom/health'
test "$(FB_ADDRESS=::1 sh /tested-healthcheck.sh)" = '-q --spider -- http://[::1]:8081/files/health'
rm /config/settings.json
test "$(FB_HEALTHCHECK_URL=http://127.0.0.1:9000/cli/health sh /tested-healthcheck.sh)" = '-q --spider -- http://127.0.0.1:9000/cli/health'
if MOCK_WGET_STATUS=1 FB_HEALTHCHECK_URL=http://127.0.0.1:9000/health sh /tested-healthcheck.sh >/dev/null; then
  printf '%s\n' 'FAIL: probe masked wget failure' >&2
  exit 1
fi
printf '%s\n' 'PASS: default, base path, env precedence, IPv6, explicit CLI/config override, and probe failure.'
TEST
