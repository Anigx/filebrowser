#!/bin/sh
set -eu
cd "$(dirname "$0")"

fail() { printf '%s\n' "$*" >&2; exit 1; }
assert_rejected() { if "$@" >/dev/null 2>&1; then fail "Unexpected success: $*"; fi; }

test "$(sh package-release.sh validate-tag v2.64.0)" = 2.64.0
assert_rejected sh package-release.sh validate-tag main
assert_rejected sh package-release.sh validate-tag v2.64.0-rc1
assert_rejected sh package-release.sh validate-tag v02.64.0

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/gh" <<'SH'
#!/bin/sh
case "$1 $2" in
  'release view')
    case "$FIXTURE" in
      missing) exit 1;;
      draft) printf '%s\n' true;;
      *) printf '%s\n' false;;
    esac;;
  'run list') printf '%s\n' 123;;
  'run view')
    sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    if [ "$FIXTURE" = mismatch ]; then sha=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb; fi
    ci=success
    if [ "$FIXTURE" = acceptance-failed ]; then ci=failure; fi
    ref=v2.64.0
    if [ "$FIXTURE" = ref-mismatch ]; then ref=master; fi
    printf '{"headSha":"%s","headBranch":"%s","conclusion":"success","jobs":[{"name":"Deployment and browser acceptance","conclusion":"%s"}]}\n' "$sha" "$ref" "$ci";;
  *) exit 1;;
esac
SH
chmod +x "$tmp/gh"
export PATH="$tmp:$PATH"
sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

export FIXTURE=good
test "$(sh package-release.sh verify-ci v2.64.0 "$sha")" = 123
for FIXTURE in missing draft mismatch ref-mismatch acceptance-failed; do
  assert_rejected sh package-release.sh verify-ci v2.64.0 "$sha"
done
printf '%s\n' 'Package release verification tests passed.'
