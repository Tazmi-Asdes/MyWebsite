#!/usr/bin/env bash
set -Eeuo pipefail

# Shared fail-closed helpers.  Configuration files are parsed as plain
# key/value text; they are never sourced as shell code.

die() {
    printf 'deployment skeleton: %s\n' "$*" >&2
    exit 2
}

require_file() {
    local path=$1
    [[ -f "$path" ]] || die "required file is missing: $path"
}

config_value() {
    local config=$1
    local key=$2
    awk -v wanted="$key" '
        /^[[:space:]]*#/ { next }
        {
            split($0, pair, "=")
            key = pair[1]
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)
            if (key == wanted) {
                value = substr($0, index($0, "=") + 1)
                sub(/^[[:space:]]+/, "", value)
                sub(/[[:space:]]+$/, "", value)
                print value
                exit
            }
        }
    ' "$config"
}

require_config_value() {
    local config=$1
    local key=$2
    local value
    value=$(config_value "$config" "$key")
    [[ -n "$value" ]] || die "missing $key in server config"
    printf '%s' "$value"
}

reject_placeholder() {
    local key=$1
    local value=$2
    case "$value" in
        *'<'*|*'>'*|*TODO*|*REPLACE*|*placeholder*|*from-read-only*|*example.invalid*)
            die "$key still contains a placeholder: $value"
            ;;
    esac
}

load_real_server_config() {
    SERVER_CONFIG=${MYWEBSITE_SERVER_CONFIG:-}
    [[ -n "$SERVER_CONFIG" ]] || die "set MYWEBSITE_SERVER_CONFIG to a completed, repository-external preflight file"
    require_file "$SERVER_CONFIG"

    local key
    for key in SERVER_CONTEXT SITE_ROOT OPENRESTY_COMPOSE MYSQL_COMPOSE MYSQL_NETWORK_NAME UPLOAD_DIR BACKUP_ROOT SCHEMA_VERSION_FILE DB_HOST DB_NAME DB_USER DB_PASSWORD_FILE; do
        CONFIG_VALUES["$key"]=$(require_config_value "$SERVER_CONFIG" "$key")
        reject_placeholder "$key" "${CONFIG_VALUES[$key]}"
    done
}

declare -Ag CONFIG_VALUES=()
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
