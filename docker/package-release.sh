#!/bin/sh
set -eu

repo=Anigx/filebrowser

fail() { printf '%s\n' "$*" >&2; exit 1; }

validate_tag() {
    printf '%s\n' "$1" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' ||
        fail 'Expected a stable vMAJOR.MINOR.PATCH release tag.'
    case "$1" in
        *[!0-9.v]*) fail 'Expected a single release tag.' ;;
    esac
    printf '%s\n' "${1#v}"
}

case "${1:-}" in
    validate-tag)
        [ "$#" -eq 2 ] || fail 'Usage: validate-tag TAG'
        validate_tag "$2"
        ;;
    verify-ci)
        [ "$#" -eq 3 ] || fail 'Usage: verify-ci TAG SHA'
        validate_tag "$2" >/dev/null
        printf '%s' "$3" | grep -Eq '^[0-9a-f]{40}$' || fail 'Invalid release source SHA.'
        draft=$(gh release view "$2" --repo "$repo" --json isDraft,isPrerelease --jq '.isDraft or .isPrerelease')
        [ "$draft" = false ] || fail 'Expected a public stable release.'
        ids=$(gh run list --repo "$repo" --workflow ci.yaml --branch "$2" --commit "$3" \
            --status success --limit 20 --json databaseId --jq '.[].databaseId')
        for id in $ids; do
            run=$(gh run view "$id" --repo "$repo" --json headSha,headBranch,conclusion,jobs)
            if printf '%s\n' "$run" | jq -e --arg sha "$3" --arg tag "$2" '
                .headSha == $sha and .headBranch == $tag and .conclusion == "success" and
                any(.jobs[]; .name == "Deployment and browser acceptance" and .conclusion == "success")
            ' >/dev/null; then
                printf '%s\n' "$id"
                exit 0
            fi
        done
        fail 'No successful CI and deployment acceptance found for the release tag and commit.'
        ;;
    *) fail 'Usage: package-release.sh validate-tag TAG | verify-ci TAG SHA' ;;
esac
