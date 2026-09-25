#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

usage() {
    printf 'usage: %s <git-tag>\n' "$(basename "$0")" >&2
}

[[ $# -eq 1 ]] || { usage; exit 2; }
TAG=$1
[[ "$TAG" =~ ^[A-Za-z0-9._/-]+$ ]] || die "invalid Git tag"

load_real_server_config

# These helpers document the only supported maintenance transition.  They are
# deliberately not called by this skeleton's execute branch until a reviewed
# server-specific command sequence is supplied.
enter_maintenance() {
    local site_root=${CONFIG_VALUES[SITE_ROOT]}
    local marker="$site_root/maintenance.flag"
    local temporary="$site_root/.maintenance.flag.$$"
    printf '%s\n' 'maintenance' >"$temporary"
    mv -f -- "$temporary" "$marker"
}

leave_maintenance() {
    rm -f -- "${CONFIG_VALUES[SITE_ROOT]}/maintenance.flag"
}

MODE=${MYWEBSITE_DEPLOY_MODE:-plan}
case "$MODE" in
    plan)
        printf 'release plan only; no local or remote action will run\n'
        printf 'tag=%s\n' "$TAG"
        printf 'candidate_ports=%s/%s\n' "${CONFIG_VALUES[CANDIDATE_APP_PORT]:-28080}" "${CONFIG_VALUES[CANDIDATE_ADMIN_PORT]:-28081}"
        printf 'stable_ports=%s/%s\n' "${CONFIG_VALUES[STABLE_APP_PORT]:-18080}" "${CONFIG_VALUES[STABLE_ADMIN_PORT]:-18081}"
        printf '%s\n' 'next steps: build immutable images, smoke-test candidate, enter maintenance, create backup batch, migrate, start stable, verify, then remove marker.'
        ;;
    execute)
        [[ ${MYWEBSITE_CONFIRM_RELEASE:-} == I_UNDERSTAND ]] || die 'execute mode requires MYWEBSITE_CONFIRM_RELEASE=I_UNDERSTAND'
        die 'release execution is intentionally a skeleton; complete server-specific commands after preflight review'
        ;;
    *)
        die 'MYWEBSITE_DEPLOY_MODE must be plan or execute'
        ;;
esac
