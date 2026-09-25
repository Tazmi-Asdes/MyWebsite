#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

usage() {
    printf 'usage: %s <git-tag>\n' "$(basename "$0")" >&2
}

[[ $# -eq 1 ]] || { usage; exit 2; }
GIT_TAG=$1
[[ "$GIT_TAG" =~ ^[A-Za-z0-9._/-]+$ ]] || die 'invalid Git tag'

load_real_server_config

MODE=${MYWEBSITE_BACKUP_MODE:-plan}
if [[ "$MODE" == plan ]]; then
    printf 'backup-batch plan only; no dump, archive, or filesystem mutation will run\n'
    printf 'git_tag=%s\n' "$GIT_TAG"
    printf 'backup_root=%s\n' "${CONFIG_VALUES[BACKUP_ROOT]}"
    printf 'uploads=%s\n' "${CONFIG_VALUES[UPLOAD_DIR]}"
    printf '%s\n' 'planned artifacts: mysql.sql, uploads.tar.gz, manifest.tsv, COMPLETE; publish by an atomic ready/current pointer switch.'
    exit 0
fi

[[ "$MODE" == execute ]] || die 'MYWEBSITE_BACKUP_MODE must be plan or execute'
[[ ${MYWEBSITE_CONFIRM_BACKUP:-} == I_UNDERSTAND ]] || die 'execute mode requires MYWEBSITE_CONFIRM_BACKUP=I_UNDERSTAND'

# This path is intentionally explicit and server-config driven.  Nothing in
# the repository can provide the required real values.
command -v mysqldump >/dev/null 2>&1 || die 'mysqldump is not installed on the verified server'
command -v sha256sum >/dev/null 2>&1 || die 'sha256sum is not installed on the verified server'
command -v tar >/dev/null 2>&1 || die 'tar is not installed on the verified server'

BACKUP_ROOT=${CONFIG_VALUES[BACKUP_ROOT]}
UPLOAD_DIR=${CONFIG_VALUES[UPLOAD_DIR]}
DB_PASSWORD_FILE=${CONFIG_VALUES[DB_PASSWORD_FILE]}
SCHEMA_VERSION_FILE=${CONFIG_VALUES[SCHEMA_VERSION_FILE]}
[[ -d "$UPLOAD_DIR" ]] || die "uploads directory is missing: $UPLOAD_DIR"
[[ -f "$DB_PASSWORD_FILE" ]] || die 'database password file is missing'
[[ -f "$SCHEMA_VERSION_FILE" ]] || die 'schema version file is missing'

BATCH_ID=$(date -u +%Y%m%dT%H%M%SZ)-$$
STAGING_ROOT="$BACKUP_ROOT/.staging"
READY_ROOT="$BACKUP_ROOT/ready"
TEMP_BATCH="$STAGING_ROOT/$BATCH_ID"
READY_BATCH="$READY_ROOT/$BATCH_ID"
mkdir -p "$STAGING_ROOT" "$READY_ROOT"
cleanup() {
    rm -rf -- "$TEMP_BATCH" "$READY_ROOT/.current.$BATCH_ID"
}
trap cleanup EXIT
mkdir "$TEMP_BATCH"

# Use a temporary defaults file so a password never appears in argv or the
# repository.  It is removed by the EXIT trap even when a command fails.
MYSQL_DEFAULTS=$(mktemp "$TEMP_BATCH/mysql-defaults.XXXXXX.cnf")
chmod 600 "$MYSQL_DEFAULTS"
{
    printf '[client]\n'
    printf 'host=%s\n' "${CONFIG_VALUES[DB_HOST]}"
    printf 'port=%s\n' "${CONFIG_VALUES[DB_PORT]:-3306}"
    printf 'user=%s\n' "${CONFIG_VALUES[DB_USER]}"
    printf 'password=%s\n' "$(cat -- "$DB_PASSWORD_FILE")"
} >"$MYSQL_DEFAULTS"

mysqldump --defaults-extra-file="$MYSQL_DEFAULTS" \
    --single-transaction --skip-lock-tables --triggers --routines --events \
    --databases "${CONFIG_VALUES[DB_NAME]}" >"$TEMP_BATCH/mysql.sql"
tar -C "$(dirname -- "$UPLOAD_DIR")" -czf "$TEMP_BATCH/uploads.tar.gz" "$(basename -- "$UPLOAD_DIR")"
rm -f -- "$MYSQL_DEFAULTS"

SCHEMA_VERSION=$(tr -d '\r\n' <"$SCHEMA_VERSION_FILE")
[[ -n "$SCHEMA_VERSION" ]] || die 'schema version file is empty'
{
    printf 'key\tvalue\n'
    printf 'batch_id\t%s\n' "$BATCH_ID"
    printf 'git_tag\t%s\n' "$GIT_TAG"
    printf 'schema_version\t%s\n' "$SCHEMA_VERSION"
    for artifact in mysql.sql uploads.tar.gz; do
        size=$(stat -c '%s' "$TEMP_BATCH/$artifact")
        digest=$(sha256sum "$TEMP_BATCH/$artifact" | awk '{print $1}')
        printf 'artifact\t%s\tsize=%s\tsha256=%s\n' "$artifact" "$size" "$digest"
    done
} >"$TEMP_BATCH/manifest.tsv"
printf '%s\n' 'complete' >"$TEMP_BATCH/COMPLETE"
rm -f -- "$TEMP_BATCH/mysql-defaults."*.cnf

# Move the finished batch into ready and atomically replace the current
# symlink.  Readers never observe the partially written staging directory.
mv -- "$TEMP_BATCH" "$READY_BATCH"
ln -s -- "$BATCH_ID" "$READY_ROOT/.current.$BATCH_ID"
mv -Tf -- "$READY_ROOT/.current.$BATCH_ID" "$READY_ROOT/current"
trap - EXIT
printf 'backup batch ready: %s\n' "$BATCH_ID"
