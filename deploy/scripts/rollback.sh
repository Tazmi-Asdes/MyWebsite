#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

usage() {
    printf 'usage: %s <previous-git-tag> <backup-batch-id>\n' "$(basename "$0")" >&2
}

[[ $# -eq 2 ]] || { usage; exit 2; }
TAG=$1
BATCH_ID=$2
[[ "$TAG" =~ ^[A-Za-z0-9._/-]+$ ]] || die 'invalid previous Git tag'
[[ "$BATCH_ID" =~ ^[A-Za-z0-9._-]+$ ]] || die 'invalid backup batch id'

load_real_server_config

# The marker must stay in place throughout a rollback.  The helpers are kept
# here as reviewed structure and are not invoked by the skeleton branch.
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
        printf 'rollback plan only; no local or remote action will run\n'
        printf 'previous_tag=%s\n' "$TAG"
        printf 'backup_batch=%s\n' "$BATCH_ID"
        printf '%s\n' 'next steps: keep maintenance marker, stop candidate/stable, restore only a verified compatible schema, start the previous tag, check readiness, then remove marker.'
        ;;
    execute)
        [[ ${MYWEBSITE_CONFIRM_ROLLBACK:-} == I_UNDERSTAND ]] || die 'execute mode requires MYWEBSITE_CONFIRM_ROLLBACK=I_UNDERSTAND'
        die 'rollback execution is intentionally a skeleton; complete server-specific commands after preflight review'
        ;;
    *)
        die 'MYWEBSITE_DEPLOY_MODE must be plan or execute'
        ;;
esac
