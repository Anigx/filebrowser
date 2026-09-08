#!/bin/sh
set -eu

# A Docker healthcheck does not inherit the entrypoint's CLI arguments. For
# custom --config/--port/--baseURL/TLS/socket deployments, set the exact local
# HTTP(S) health URL explicitly (or replace the healthcheck for Unix sockets).
if [ -n "${FB_HEALTHCHECK_URL:-}" ]; then
  exec wget -q --spider -- "$FB_HEALTHCHECK_URL"
fi

config_value() {
  sh /JSON.sh < /config/settings.json | awk -v key='["'"$1"'"]' '$1 == key {gsub(/^"|"$/, "", $2); print $2}'
}
PORT=${FB_PORT:-$(config_value port)}
ADDRESS=${FB_ADDRESS:-$(config_value address)}
BASE_URL=${FB_BASE_URL:-$(config_value baseURL)}
case "$ADDRESS" in
  ""|0.0.0.0) ADDRESS=127.0.0.1 ;;
  ::|\[::\]) ADDRESS='[::1]' ;;
  *:*) case "$ADDRESS" in \[*\]) ;; *) ADDRESS="[$ADDRESS]" ;; esac ;;
esac
BASE_URL=${BASE_URL%/}
exec wget -q --spider -- "http://$ADDRESS:${PORT:-80}${BASE_URL}/health"
