#!/usr/bin/env bash
set -uo pipefail
umask 077

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=${CI_PROJECT_DIR:-}
DEPLOY_ROOT=${DEPLOY_ROOT:-/opt/mywebsite}
STATE_DIR=${DEPLOY_STATE_DIR:-$DEPLOY_ROOT/deploy-state}
REGISTRY_ROOT=${CI_REGISTRY_IMAGE:-}
COMPOSE_PROJECT_NAME=${COMPOSE_PROJECT_NAME:-mywebsite}
MYSQL_NETWORK_NAME=${MYSQL_NETWORK_NAME:-1panel-network}
APP_PORT=${APP_PORT:-18080}
ADMIN_WEB_PORT=${ADMIN_WEB_PORT:-18081}
PRODUCTION_URL=${PRODUCTION_URL:-https://tazmi.online}
ENV_SOURCE="$DEPLOY_ROOT/deploy/env.production"
ENV_TARGET="${PROJECT_DIR:+$PROJECT_DIR/deploy/env.production}"
DIAG_DIR="${PROJECT_DIR:+$PROJECT_DIR/test-results/deploy}"
DIAG_PATH=
DIAG_RESULT=failed
DIAG_REASON=not_started
COPIED_ENV=0
UP_ATTEMPTED=0
AUTO_ROLLBACK_ALLOWED=1
RUNTIME_TMP=
CURRENT_EXISTS=0
CURRENT_FILE="$STATE_DIR/current"
PREVIOUS_FILE="$STATE_DIR/previous"
declare -Ag CURRENT=() TARGET=() DEPLOY_TARGET=()

usage() {
    printf 'usage: %s deploy|rollback\n' "${0##*/}" >&2
}

diagnostic() {
    local event=$1
    [[ -n "$DIAG_DIR" ]] || return 0
    mkdir -p -- "$DIAG_DIR" 2>/dev/null || return 0
    if [[ -z "$DIAG_PATH" ]]; then
        local suffix=${CI_PIPELINE_ID:-local}
        [[ "$suffix" =~ ^[A-Za-z0-9._-]+$ ]] || suffix=local
        DIAG_PATH="$DIAG_DIR/deploy-$suffix.txt"
        : >"$DIAG_PATH" 2>/dev/null || { DIAG_PATH=; return 0; }
        chmod 600 "$DIAG_PATH" 2>/dev/null || true
    fi
    printf '%s\n' "$event" >>"$DIAG_PATH" 2>/dev/null || true
}

finish() {
    local status=$?
    trap - EXIT
    if [[ $COPIED_ENV -eq 1 && -n "$ENV_TARGET" && "$ENV_TARGET" != "$ENV_SOURCE" ]]; then
        rm -f -- "$ENV_TARGET" 2>/dev/null || true
    fi
    if [[ -n "$RUNTIME_TMP" ]]; then
        rm -rf -- "$RUNTIME_TMP" 2>/dev/null || true
    fi
    if [[ -n "$DIAG_DIR" ]]; then
        diagnostic "result=$DIAG_RESULT"
        diagnostic "reason=$DIAG_REASON"
    fi
    exit "$status"
}
trap finish EXIT

die() {
    DIAG_RESULT=failed
    DIAG_REASON=$2
    printf 'deployment: %s\n' "$1" >&2
    exit 1
}

warn() {
    printf 'deployment: warning: %s\n' "$1" >&2
    diagnostic "warning=$2"
}

valid_sha() {
    [[ ${#1} -eq 40 && "$1" =~ ^[[:xdigit:]]{40}$ ]]
}

valid_hex64() {
    [[ ${#1} -eq 64 && "$1" =~ ^[[:xdigit:]]{64}$ ]]
}

valid_name() {
    [[ -n "$1" && "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]
}

valid_port() {
    [[ "$1" =~ ^[0-9]{1,5}$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535))
}

hash_fields() {
    printf '%s\n' "$@" | sha256sum | awk '{print $1}'
}

hash_file() {
    sha256sum -- "$1" | awk '{print $1}'
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1" command_missing
}

compose() {
    docker compose \
        --project-directory "$PROJECT_DIR" \
        -f "$PROJECT_DIR/compose.yaml" \
        -f "$PROJECT_DIR/deploy/compose.onepanel.example.yaml" \
        -f "$PROJECT_DIR/deploy/compose.redis.onepanel.yaml" \
        -f "$PROJECT_DIR/deploy/compose.images.yaml" \
        "$@"
}

copy_production_env() {
    [[ -n "$PROJECT_DIR" && -d "$PROJECT_DIR/deploy" ]] || die 'CI_PROJECT_DIR/deploy is unavailable' workspace_missing
    [[ -r "$ENV_SOURCE" && -f "$ENV_SOURCE" ]] || die 'server deploy/env.production is missing or unreadable' production_env_missing
    [[ -n "$ENV_TARGET" ]] || die 'CI env target path is unavailable' workspace_missing
    if [[ "$ENV_TARGET" != "$ENV_SOURCE" ]]; then
        install -m 0600 -- "$ENV_SOURCE" "$ENV_TARGET" || die 'could not copy production environment into the CI workspace' env_copy_failed
        COPIED_ENV=1
    fi
}

validate_runtime_inputs() {
    [[ "$PROJECT_DIR" = /* && -d "$PROJECT_DIR" ]] || die 'CI_PROJECT_DIR must be an existing absolute path' workspace_missing
    [[ "$DEPLOY_ROOT" = /* && -d "$DEPLOY_ROOT" ]] || die 'DEPLOY_ROOT must be an existing absolute path' deploy_root_missing
    [[ "$STATE_DIR" = /* ]] || die 'deployment state path must be absolute' invalid_state_path
    [[ -n "$REGISTRY_ROOT" && "$REGISTRY_ROOT" =~ ^[A-Za-z0-9._:/-]+$ && "$REGISTRY_ROOT" != */ ]] || die 'CI_REGISTRY_IMAGE is missing or invalid' invalid_registry
    valid_name "$COMPOSE_PROJECT_NAME" || die 'COMPOSE_PROJECT_NAME is invalid' invalid_project_name
    valid_name "$MYSQL_NETWORK_NAME" || die 'MYSQL_NETWORK_NAME is invalid' invalid_network_name
    valid_port "$APP_PORT" || die 'APP_PORT is invalid' invalid_app_port
    valid_port "$ADMIN_WEB_PORT" || die 'ADMIN_WEB_PORT is invalid' invalid_admin_port
    [[ "$PRODUCTION_URL" =~ ^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?/?$ ]] || die 'PRODUCTION_URL must be a production HTTPS origin' invalid_production_url
    local file
    for file in compose.yaml deploy/compose.onepanel.example.yaml deploy/compose.redis.onepanel.yaml deploy/compose.images.yaml; do
        [[ -r "$PROJECT_DIR/$file" ]] || die "required production Compose file is missing: $file" compose_file_missing
    done
}

validate_secret_inputs() {
    local db_password="$DEPLOY_ROOT/secrets/db_password"
    local redis_password="$DEPLOY_ROOT/secrets/redis_password"
    [[ -r "$db_password" && -s "$db_password" ]] || die 'database password file is missing or empty' db_password_missing
    [[ -r "$redis_password" && -s "$redis_password" ]] || die 'Redis password file is missing or empty' redis_password_missing
    awk 'NR > 1 { multiple=1 } END { exit multiple ? 1 : 0 }' "$db_password" || die 'database password file must contain one line' db_password_multiline
    awk 'NR > 1 { multiple=1 } END { exit multiple ? 1 : 0 }' "$redis_password" || die 'Redis password file must contain one line' redis_password_multiline
    [[ -d "$DEPLOY_ROOT/data/uploads" ]] || die 'uploads directory is missing' uploads_missing
    docker network inspect "$MYSQL_NETWORK_NAME" >/dev/null 2>&1 || die 'configured external MySQL network is unavailable' mysql_network_missing

    local redis_config_error
    redis_config_error=$(awk '
        /^[[:space:]]*(#|$)/ { next }
        /^[[:space:]]*(export[[:space:]]+)?REDIS_(ADDR|URL|PASSWORD_FILE|DB)[[:space:]]*=/ {
          line = $0
          sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line)
          split(line, fields, "=")
          key = fields[1]
          gsub(/[[:space:]]/, "", key)
          if (key == "REDIS_ADDR") addr = 1
          if (key == "REDIS_URL") url = 1
          if (key == "REDIS_PASSWORD_FILE") password_file = 1
          if (key == "REDIS_DB") db = 1
        }
        END {
          if ((password_file || db) && !addr && !url) print "missing Redis address"
          if (addr && url) print "both Redis address forms are set"
        }
    ' "$ENV_TARGET") || die 'could not inspect Redis configuration in env.production' redis_config_parse_failed
    [[ -z "$redis_config_error" ]] || die 'Redis configuration has conflicting or incomplete address settings' redis_config_invalid
}

assert_main_pipeline() {
    local mode=$1
    [[ -n "${CI_DEFAULT_BRANCH:-}" && "$CI_DEFAULT_BRANCH" =~ ^[A-Za-z0-9._/-]+$ ]] || die 'CI_DEFAULT_BRANCH is missing or invalid' invalid_default_branch
    [[ "${CI_COMMIT_BRANCH:-}" == "$CI_DEFAULT_BRANCH" ]] || die 'deployment commands are allowed only on the default branch' not_default_branch
    [[ "${CI_COMMIT_SHA:-}" =~ ^[[:xdigit:]]{40}$ ]] || die 'CI_COMMIT_SHA is missing or invalid' invalid_pipeline_commit
    local checkout_commit
    checkout_commit=$(git -C "$PROJECT_DIR" rev-parse --verify 'HEAD^{commit}' 2>/dev/null) || die 'CI checkout does not resolve to a commit' invalid_checkout
    [[ "$checkout_commit" == "$CI_COMMIT_SHA" ]] || die 'CI checkout commit differs from CI_COMMIT_SHA' checkout_commit_mismatch
    if [[ "$mode" == rollback && "${CI_PIPELINE_SOURCE:-}" != web ]]; then
        die 'rollback is available only from a new web pipeline on the default branch' rollback_requires_web_pipeline
    fi
    if [[ "$mode" == deploy && "${CI_PIPELINE_SOURCE:-}" != push && "${CI_PIPELINE_SOURCE:-}" != web ]]; then
        die 'automatic deployment accepts default-branch push pipelines; manual release exceptions use a web pipeline' deploy_pipeline_source_invalid
    fi
    [[ "${CI_COMMIT_REF_PROTECTED:-}" == true ]] || die 'production deployment requires a protected default-branch ref' unprotected_release_ref
    assert_remote_default_head
}

assert_remote_default_head() {
    local remote_line remote_head expected_ref
    expected_ref="refs/heads/$CI_DEFAULT_BRANCH"
    remote_line=$(GIT_TERMINAL_PROMPT=0 git -c http.lowSpeedLimit=1000 -c http.lowSpeedTime=10 -C "$PROJECT_DIR" \
        ls-remote --exit-code origin "$expected_ref" 2>/dev/null) || die 'could not verify the remote default branch head; no production change was made' remote_head_unavailable
    remote_head=$(printf '%s\n' "$remote_line" | awk -v ref="$expected_ref" '$2 == ref { if (count++) exit 1; print $1 } END { if (count != 1) exit 1 }') || die 'remote default branch head response was invalid' remote_head_invalid
    [[ "$remote_head" =~ ^[[:xdigit:]]{40}$ ]] || die 'remote default branch head response was invalid' remote_head_invalid
    [[ "$remote_head" == "$CI_COMMIT_SHA" ]] || die 'this pipeline is outdated because the remote default branch has advanced; run a new pipeline' outdated_pipeline
}

git_commit_exists() {
    local commit=$1 resolved
    valid_sha "$commit" || return 1
    resolved=$(git -C "$PROJECT_DIR" rev-parse --verify "$commit^{commit}" 2>/dev/null) || return 1
    [[ "$resolved" == "$commit" ]]
}

git_object_for_path() {
    local commit=$1 path=$2 object
    object=$(git -C "$PROJECT_DIR" rev-parse --verify "$commit:$path" 2>/dev/null) || return 1
    valid_sha "$object" || return 1
    printf '%s' "$object"
}

compute_source_fingerprints() {
    local commit=$1
    CTX_MIGRATION=$(git_object_for_path "$commit" db/migrations) || return 1
    CTX_COMPOSE=$(git_object_for_path "$commit" compose.yaml) || return 1
    CTX_NETWORK=$(git_object_for_path "$commit" deploy/compose.onepanel.example.yaml) || return 1
    CTX_REDIS=$(git_object_for_path "$commit" deploy/compose.redis.onepanel.yaml) || return 1
    CTX_ENV=$(hash_file "$ENV_TARGET") || return 1
    CTX_RUNTIME=$(hash_fields "deploy_root=$DEPLOY_ROOT" "project=$COMPOSE_PROJECT_NAME" "network=$MYSQL_NETWORK_NAME" "app_port=$APP_PORT" "admin_port=$ADMIN_WEB_PORT" "production_url=${PRODUCTION_URL%/}") || return 1
    CTX_CONFIG=$(hash_fields "migration=$CTX_MIGRATION" "compose=$CTX_COMPOSE" "network=$CTX_NETWORK" "redis=$CTX_REDIS" "env=$CTX_ENV" "runtime=$CTX_RUNTIME") || return 1
    valid_hex64 "$CTX_ENV" && valid_hex64 "$CTX_RUNTIME" && valid_hex64 "$CTX_CONFIG"
}

record_config_fingerprint() {
    local migration=$1 compose_blob=$2 network_blob=$3 redis_blob=$4 env_hash=$5 runtime_hash=$6
    hash_fields "migration=$migration" "compose=$compose_blob" "network=$network_blob" "redis=$redis_blob" "env=$env_hash" "runtime=$runtime_hash"
}

record_runtime_fingerprint() {
    local deploy_root=$1 project=$2 network=$3 app_port=$4 admin_port=$5 production_url=$6
    hash_fields "deploy_root=$deploy_root" "project=$project" "network=$network" "app_port=$app_port" "admin_port=$admin_port" "production_url=$production_url"
}

declare -ar RECORD_KEYS=(format release_commit app_digest admin_web_digest app_sha_tag admin_web_sha_tag migration_tree compose_blob mysql_overlay_blob redis_overlay_blob environment_fingerprint runtime_fingerprint config_fingerprint deploy_root compose_project mysql_network app_port admin_web_port production_url)

read_record() {
    local path=$1 output_name=$2 line key value expected_runtime expected_config allowed_key allowed
    local -n output=$output_name
    output=()
    [[ -r "$path" && -f "$path" ]] || return 1
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ "$line" == *=* ]] || return 1
        key=${line%%=*}
        value=${line#*=}
        allowed=0
        for allowed_key in "${RECORD_KEYS[@]}"; do [[ "$key" == "$allowed_key" ]] && allowed=1; done
        [[ $allowed -eq 1 ]] || return 1
        [[ -z ${output[$key]+set} ]] || return 1
        [[ "$value" != *$'\r'* && "$value" != *$'\n'* ]] || return 1
        output["$key"]=$value
    done <"$path"
    [[ ${#output[@]} -eq ${#RECORD_KEYS[@]} ]] || return 1
    local required
    for required in "${RECORD_KEYS[@]}"; do [[ -n ${output[$required]+set} ]] || return 1; done
    [[ ${output[format]} == 1 ]] || return 1
    valid_sha "${output[release_commit]}" || return 1
    valid_hex64 "${output[app_digest]}" && valid_hex64 "${output[admin_web_digest]}" || return 1
    [[ ${output[app_sha_tag]} == "$REGISTRY_ROOT/app:${output[release_commit]}" ]] || return 1
    [[ ${output[admin_web_sha_tag]} == "$REGISTRY_ROOT/admin-web:${output[release_commit]}" ]] || return 1
    valid_sha "${output[migration_tree]}" && valid_sha "${output[compose_blob]}" && valid_sha "${output[mysql_overlay_blob]}" && valid_sha "${output[redis_overlay_blob]}" || return 1
    valid_hex64 "${output[environment_fingerprint]}" && valid_hex64 "${output[runtime_fingerprint]}" && valid_hex64 "${output[config_fingerprint]}" || return 1
    [[ ${output[deploy_root]} = /* ]] || return 1
    valid_name "${output[compose_project]}" && valid_name "${output[mysql_network]}" || return 1
    valid_port "${output[app_port]}" && valid_port "${output[admin_web_port]}" || return 1
    [[ ${output[production_url]} =~ ^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?$ ]] || return 1
    expected_runtime=$(record_runtime_fingerprint "${output[deploy_root]}" "${output[compose_project]}" "${output[mysql_network]}" "${output[app_port]}" "${output[admin_web_port]}" "${output[production_url]}") || return 1
    [[ "$expected_runtime" == "${output[runtime_fingerprint]}" ]] || return 1
    expected_config=$(record_config_fingerprint "${output[migration_tree]}" "${output[compose_blob]}" "${output[mysql_overlay_blob]}" "${output[redis_overlay_blob]}" "${output[environment_fingerprint]}" "${output[runtime_fingerprint]}") || return 1
    [[ "$expected_config" == "${output[config_fingerprint]}" ]]
}

write_record_temp() {
    local temp=$1 commit=$2 app_digest=$3 admin_digest=$4 migration=$5 compose_blob=$6 network_blob=$7 redis_blob=$8 env_hash=$9
    shift 9
    local runtime_hash=$1 config_hash=$2 deploy_root=$3 project=$4 network=$5 app_port=$6 admin_port=$7 production_url=$8
    {
        printf 'format=1\n'
        printf 'release_commit=%s\n' "$commit"
        printf 'app_digest=%s\n' "$app_digest"
        printf 'admin_web_digest=%s\n' "$admin_digest"
        printf 'app_sha_tag=%s/app:%s\n' "$REGISTRY_ROOT" "$commit"
        printf 'admin_web_sha_tag=%s/admin-web:%s\n' "$REGISTRY_ROOT" "$commit"
        printf 'migration_tree=%s\ncompose_blob=%s\nmysql_overlay_blob=%s\nredis_overlay_blob=%s\n' "$migration" "$compose_blob" "$network_blob" "$redis_blob"
        printf 'environment_fingerprint=%s\nruntime_fingerprint=%s\nconfig_fingerprint=%s\n' "$env_hash" "$runtime_hash" "$config_hash"
        printf 'deploy_root=%s\ncompose_project=%s\nmysql_network=%s\napp_port=%s\nadmin_web_port=%s\nproduction_url=%s\n' \
            "$deploy_root" "$project" "$network" "$app_port" "$admin_port" "$production_url"
    } >"$temp" || return 1
    chmod 600 "$temp" || return 1
}

write_release_record() {
    local path=$1 commit=$2 app_digest=$3 admin_digest=$4 migration=$5 compose_blob=$6 network_blob=$7 redis_blob=$8 env_hash=$9
    shift 9
    local runtime_hash=$1 config_hash=$2 deploy_root=$3 project=$4 network=$5 app_port=$6 admin_port=$7 production_url=$8 temp
    temp=$(mktemp "$STATE_DIR/.release.XXXXXX") || return 1
    if ! write_record_temp "$temp" "$commit" "$app_digest" "$admin_digest" "$migration" "$compose_blob" "$network_blob" "$redis_blob" "$env_hash" \
        "$runtime_hash" "$config_hash" "$deploy_root" "$project" "$network" "$app_port" "$admin_port" "$production_url"; then
        rm -f -- "$temp"
        return 1
    fi
    if ! mv -f -- "$temp" "$path"; then
        rm -f -- "$temp"
        return 1
    fi
}

build_record_from_context() {
    local path=$1 commit=$2 app_digest=$3 admin_digest=$4
    write_release_record "$path" "$commit" "$app_digest" "$admin_digest" "$CTX_MIGRATION" "$CTX_COMPOSE" "$CTX_NETWORK" "$CTX_REDIS" "$CTX_ENV" \
        "$CTX_RUNTIME" "$CTX_CONFIG" "$DEPLOY_ROOT" "$COMPOSE_PROJECT_NAME" "$MYSQL_NETWORK_NAME" "$APP_PORT" "$ADMIN_WEB_PORT" "${PRODUCTION_URL%/}"
}

promote_target_record() {
    local target_name=$1 current_stage= previous_stage= previous_backup= previous_existed=0
    local -n promoted=$target_name
    # Retrying the same successful release must not discard its rollback target.
    if [[ $CURRENT_EXISTS -eq 1 &&
          ${promoted[release_commit]} == "${CURRENT[release_commit]}" &&
          ${promoted[app_digest]} == "${CURRENT[app_digest]}" &&
          ${promoted[admin_web_digest]} == "${CURRENT[admin_web_digest]}" &&
          ${promoted[config_fingerprint]} == "${CURRENT[config_fingerprint]}" ]]; then
        return 0
    fi
    current_stage=$(mktemp "$STATE_DIR/.current.XXXXXX") || return 1
    if ! write_record_temp "$current_stage" "${promoted[release_commit]}" "${promoted[app_digest]}" "${promoted[admin_web_digest]}" \
        "${promoted[migration_tree]}" "${promoted[compose_blob]}" "${promoted[mysql_overlay_blob]}" "${promoted[redis_overlay_blob]}" \
        "${promoted[environment_fingerprint]}" "${promoted[runtime_fingerprint]}" "${promoted[config_fingerprint]}" \
        "${promoted[deploy_root]}" "${promoted[compose_project]}" "${promoted[mysql_network]}" "${promoted[app_port]}" \
        "${promoted[admin_web_port]}" "${promoted[production_url]}"; then
        rm -f -- "$current_stage"
        return 1
    fi
    if [[ $CURRENT_EXISTS -eq 1 ]]; then
        previous_stage=$(mktemp "$STATE_DIR/.previous.XXXXXX") || { rm -f -- "$current_stage"; return 1; }
        cp -- "$CURRENT_FILE" "$previous_stage" || { rm -f -- "$current_stage" "$previous_stage"; return 1; }
        chmod 600 "$previous_stage" || { rm -f -- "$current_stage" "$previous_stage"; return 1; }
        if [[ -f "$PREVIOUS_FILE" ]]; then
            previous_backup=$(mktemp "$STATE_DIR/.previous-backup.XXXXXX") || { rm -f -- "$current_stage" "$previous_stage"; return 1; }
            cp -- "$PREVIOUS_FILE" "$previous_backup" || { rm -f -- "$current_stage" "$previous_stage" "$previous_backup"; return 1; }
            previous_existed=1
        fi
        if ! mv -f -- "$previous_stage" "$PREVIOUS_FILE"; then
            rm -f -- "$current_stage" "$previous_stage" "$previous_backup"
            return 1
        fi
    fi
    if ! mv -f -- "$current_stage" "$CURRENT_FILE"; then
        if [[ $CURRENT_EXISTS -eq 1 ]]; then
            if [[ $previous_existed -eq 1 ]]; then mv -f -- "$previous_backup" "$PREVIOUS_FILE" 2>/dev/null || true
            else rm -f -- "$PREVIOUS_FILE" 2>/dev/null || true
            fi
        fi
        rm -f -- "$current_stage" "$previous_stage" "$previous_backup"
        return 1
    fi
    rm -f -- "$previous_backup" 2>/dev/null || true
    return 0
}

read_and_verify_record() {
    local path=$1 output_name=$2 commit migration compose_blob network_blob redis_blob
    read_record "$path" "$output_name" || return 1
    local -n record=$output_name
    commit=${record[release_commit]}
    git_commit_exists "$commit" || {
        printf 'deployment: stored successful commit %s is absent from the CI checkout. Fetch that Git object (for example, fetch the recorded SHA or deepen the protected pipeline clone), then retry. Do not bypass this check with the configuration exception.\n' "$commit" >&2
        return 2
    }
    migration=$(git_object_for_path "$commit" db/migrations) || { printf 'deployment: source trees for stored commit %s could not be verified. Fetch/deepen the Git object, then retry; automatic rollback is blocked.\n' "$commit" >&2; return 2; }
    compose_blob=$(git_object_for_path "$commit" compose.yaml) || { printf 'deployment: production Compose files for stored commit %s could not be verified. Fetch/deepen the Git object, then retry.\n' "$commit" >&2; return 2; }
    network_blob=$(git_object_for_path "$commit" deploy/compose.onepanel.example.yaml) || { printf 'deployment: production network overlay for stored commit %s could not be verified. Fetch/deepen the Git object, then retry.\n' "$commit" >&2; return 2; }
    redis_blob=$(git_object_for_path "$commit" deploy/compose.redis.onepanel.yaml) || { printf 'deployment: Redis overlay for stored commit %s could not be verified. Fetch/deepen the Git object, then retry.\n' "$commit" >&2; return 2; }
    [[ "$migration" == "${record[migration_tree]}" && "$compose_blob" == "${record[compose_blob]}" &&
       "$network_blob" == "${record[mysql_overlay_blob]}" && "$redis_blob" == "${record[redis_overlay_blob]}" ]] || return 2
    return 0
}

service_container_id() {
    local service=$1 ids count
    ids=$(docker ps -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" --filter "label=com.docker.compose.service=$service" 2>/dev/null) || return 1
    count=$(printf '%s\n' "$ids" | awk 'NF { n++ } END { print n+0 }')
    [[ "$count" == 1 ]] || return 1
    printf '%s\n' "$ids" | awk 'NF { print; exit }'
}

image_id_for_ref() {
    local ref=$1 id
    id=$(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null) || return 1
    [[ "$id" =~ ^sha256:[[:xdigit:]]{64}$ ]] || return 1
    printf '%s' "$id"
}

container_image_id() {
    local id=$1 image_id
    image_id=$(docker inspect --format '{{.Image}}' "$id" 2>/dev/null) || return 1
    [[ "$image_id" =~ ^sha256:[[:xdigit:]]{64}$ ]] || return 1
    printf '%s' "$image_id"
}

verify_running_pair() {
    local app_ref=$1 admin_ref=$2 app_container admin_container expected actual
    app_container=$(service_container_id app) || return 1
    admin_container=$(service_container_id admin-web) || return 1
    expected=$(image_id_for_ref "$app_ref") || return 1
    actual=$(container_image_id "$app_container") || return 1
    [[ "$expected" == "$actual" ]] || return 1
    expected=$(image_id_for_ref "$admin_ref") || return 1
    actual=$(container_image_id "$admin_container") || return 1
    [[ "$expected" == "$actual" ]]
}

probe_local_health() {
    curl --fail --silent --connect-timeout 1 --max-time 2 -o /dev/null "http://127.0.0.1:$APP_PORT/-/live" >/dev/null 2>&1 &&
    curl --fail --silent --connect-timeout 1 --max-time 2 -o /dev/null "http://127.0.0.1:$APP_PORT/-/ready" >/dev/null 2>&1 &&
    curl --fail --silent --connect-timeout 1 --max-time 2 -o /dev/null "http://127.0.0.1:$ADMIN_WEB_PORT/admin/" >/dev/null 2>&1
}

wait_for_local_health() {
    local attempt
    for attempt in $(seq 1 12); do
        if probe_local_health; then return 0; fi
        [[ "$attempt" == 12 ]] || sleep 1
    done
    return 1
}

smoke_request() {
    local path=$1 mime=$2 marker=$3 result status content_type elapsed remainder curl_status=0 category
    local body="$RUNTIME_TMP/smoke-body" url="${PRODUCTION_URL%/}$path"
    if result=$(curl --fail --silent --show-error --connect-timeout 5 --max-time 15 -L \
        -o "$body" -w '%{http_code}|%{content_type}|%{time_total}' "$url"); then
        curl_status=0
    else
        curl_status=$?
    fi
    status=${result%%|*}
    remainder=${result#*|}
    if [[ "$remainder" == "$result" ]]; then
        content_type=
        elapsed=
    else
        content_type=${remainder%%|*}
        elapsed=${remainder##*|}
    fi
    [[ -n "$status" ]] || status=unknown
    [[ -n "$elapsed" ]] || elapsed=unknown
    if ((curl_status != 0)); then
        if [[ "$curl_status" == 22 && "$status" =~ ^[45][0-9][0-9]$ ]]; then category=http_status; else category=curl_error; fi
        report_smoke_failure "$path" "$category" "$curl_status" "$status" "$content_type" "$elapsed"
        return 1
    fi
    if [[ "$status" != 200 ]]; then
        report_smoke_failure "$path" http_status "$curl_status" "$status" "$content_type" "$elapsed"
        return 1
    fi
    case "${content_type,,}" in
        "$mime"* ) ;;
        *) report_smoke_failure "$path" mime "$curl_status" "$status" "$content_type" "$elapsed"; return 1 ;;
    esac
    if ! grep -Fq -- "$marker" "$body"; then
        report_smoke_failure "$path" marker "$curl_status" "$status" "$content_type" "$elapsed"
        return 1
    fi
}

report_smoke_failure() {
    local path=$1 category=$2 curl_status=$3 status=$4 content_type=$5 elapsed=$6
    local safe_content_type=${content_type// /_}
    diagnostic "smoke_failure path=$path category=$category curl_exit=$curl_status http_status=$status content_type=${safe_content_type:-unknown} elapsed_seconds=$elapsed"
    printf 'deployment: public smoke failed path=%s category=%s curl_exit=%s http_status=%s content_type=%s elapsed_seconds=%s\n' \
        "$path" "$category" "$curl_status" "$status" "${content_type:-unknown}" "$elapsed" >&2
}

smoke_once() {
    smoke_request / text/html 'href="/" aria-current="page">首页</a>' &&
    smoke_request /articles text/html 'href="/articles" aria-current="page">文章</a>' &&
    smoke_request /projects text/html 'href="/projects" aria-current="page">项目</a>' &&
    smoke_request /about text/html 'href="/about" aria-current="page">关于</a>' &&
    smoke_request /admin/ text/html 'id="app"' &&
    smoke_request /assets/styles.css text/css '--bg:'
}

wait_for_production_smoke() {
    local attempt
    for attempt in 1 2; do
        if smoke_once; then return 0; fi
        [[ "$attempt" == 2 ]] || sleep 1
    done
    return 1
}

verify_release() {
    local app_ref=$1 admin_ref=$2
    verify_running_pair "$app_ref" "$admin_ref" || { LAST_FAILURE=container_image_mismatch; return 1; }
    wait_for_local_health || { LAST_FAILURE=local_health_failed; return 1; }
    wait_for_production_smoke || { LAST_FAILURE=production_smoke_failed; return 1; }
    LAST_FAILURE=
    return 0
}

pull_images() {
    local app_ref=$1 admin_ref=$2 attempt status
    export APP_IMAGE_REF=$app_ref ADMIN_WEB_IMAGE_REF=$admin_ref
    for attempt in 1 2 3; do
        if compose pull app admin-web >/dev/null 2>&1; then return 0; else status=$?; fi
        [[ "$attempt" == 3 ]] || sleep "$attempt"
    done
    return "${status:-1}"
}

update_services() {
    local app_ref=$1 admin_ref=$2
    export APP_IMAGE_REF=$app_ref ADMIN_WEB_IMAGE_REF=$admin_ref
    UP_ATTEMPTED=1
    compose up --no-build --no-deps --pull never -d app admin-web >/dev/null 2>&1 || { LAST_FAILURE=compose_up_failed; return 1; }
    verify_release "$app_ref" "$admin_ref"
}

restore_current_release() {
    local app_ref="$REGISTRY_ROOT/app@sha256:${CURRENT[app_digest]}"
    local admin_ref="$REGISTRY_ROOT/admin-web@sha256:${CURRENT[admin_web_digest]}"
    printf 'Release check failed; restoring the last recorded successful image pair.\n' >&2
    diagnostic 'recovery=started'
    export APP_IMAGE_REF=$app_ref ADMIN_WEB_IMAGE_REF=$admin_ref
    if ! compose config --quiet >/dev/null 2>&1; then
        printf 'deployment: recovery could not validate the Compose configuration; current containers were left for operator review.\n' >&2
        diagnostic 'recovery=compose_validation_failed'
        return 1
    fi
    if ! pull_images "$app_ref" "$admin_ref"; then
        printf 'deployment: recovery could not pull the recorded digest pair; current containers were left for operator review.\n' >&2
        diagnostic 'recovery=pull_failed'
        return 1
    fi
    if ! compose up --no-build --no-deps --pull never -d app admin-web >/dev/null 2>&1; then
        printf 'deployment: recovery update failed; inspect both services manually. No service was deleted.\n' >&2
        diagnostic 'recovery=up_failed'
        return 1
    fi
    if ! verify_release "$app_ref" "$admin_ref"; then
        printf 'deployment: recorded image pair was reapplied, but release verification failed (%s); inspect the services manually.\n' \
            "${LAST_FAILURE:-release_verification_failed}" >&2
        diagnostic "recovery=${LAST_FAILURE:-health_failed}"
        return 1
    fi
    printf 'Last successful image pair is healthy again. This pipeline still exits with failure.\n' >&2
    diagnostic 'recovery=healthy'
    return 0
}

bootstrap_current_from_running() {
    local app_container admin_container app_config admin_config app_id admin_id app_commit admin_commit app_digest admin_digest
    local app_ref admin_ref repo tag rows row_repo row_tag row_id
    app_container=$(service_container_id app 2>/dev/null) || return 2
    admin_container=$(service_container_id admin-web 2>/dev/null) || return 2
    probe_local_health || return 2
    app_config=$(docker inspect --format '{{.Config.Image}}' "$app_container" 2>/dev/null) || return 2
    admin_config=$(docker inspect --format '{{.Config.Image}}' "$admin_container" 2>/dev/null) || return 2
    app_id=$(container_image_id "$app_container") || return 2
    admin_id=$(container_image_id "$admin_container") || return 2
    app_ref="$REGISTRY_ROOT/app"
    admin_ref="$REGISTRY_ROOT/admin-web"
    case "$app_config" in
        "$app_ref:"*) app_commit=${app_config#"$app_ref:"}; valid_sha "$app_commit" || return 2 ;;
        "$app_ref@sha256:"*)
            app_digest=${app_config#"$app_ref@sha256:"}; valid_hex64 "$app_digest" || return 2
            rows=$(docker image ls --no-trunc --format '{{.Repository}} {{.Tag}} {{.ID}}' "$app_ref" 2>/dev/null) || return 2
            app_commit=
            while read -r row_repo row_tag row_id; do
                if [[ "$row_repo" == "$app_ref" && "$row_id" == "$app_id" ]] && valid_sha "$row_tag"; then app_commit=$row_tag; break; fi
            done <<<"$rows"
            valid_sha "$app_commit" || return 2
            ;;
        *) return 2 ;;
    esac
    case "$admin_config" in
        "$admin_ref:"*) admin_commit=${admin_config#"$admin_ref:"}; valid_sha "$admin_commit" || return 2 ;;
        "$admin_ref@sha256:"*)
            admin_digest=${admin_config#"$admin_ref@sha256:"}; valid_hex64 "$admin_digest" || return 2
            rows=$(docker image ls --no-trunc --format '{{.Repository}} {{.Tag}} {{.ID}}' "$admin_ref" 2>/dev/null) || return 2
            admin_commit=
            while read -r row_repo row_tag row_id; do
                if [[ "$row_repo" == "$admin_ref" && "$row_id" == "$admin_id" ]] && valid_sha "$row_tag"; then admin_commit=$row_tag; break; fi
            done <<<"$rows"
            valid_sha "$admin_commit" || return 2
            ;;
        *) return 2 ;;
    esac
    [[ "$app_commit" == "$admin_commit" ]] || return 2
    git_commit_exists "$app_commit" || {
        printf 'deployment: running image commit %s is absent from the CI checkout. Fetch that SHA into the protected pipeline clone before adopting this release state.\n' "$app_commit" >&2
        return 3
    }
    app_digest=${app_config#"$app_ref@sha256:"}
    admin_digest=${admin_config#"$admin_ref@sha256:"}
    if [[ "$app_config" != *'@sha256:'* ]]; then
        rows=$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$app_id" 2>/dev/null) || return 2
        app_digest=
        while IFS= read -r row; do
            case "$row" in "$app_ref@sha256:"*) app_digest=${row#"$app_ref@sha256:"}; break ;; esac
        done <<<"$rows"
    fi
    if [[ "$admin_config" != *'@sha256:'* ]]; then
        rows=$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$admin_id" 2>/dev/null) || return 2
        admin_digest=
        while IFS= read -r row; do
            case "$row" in "$admin_ref@sha256:"*) admin_digest=${row#"$admin_ref@sha256:"}; break ;; esac
        done <<<"$rows"
    fi
    valid_hex64 "$app_digest" && valid_hex64 "$admin_digest" || return 2
    compute_source_fingerprints "$app_commit" || return 3
    mkdir -p -- "$STATE_DIR" || return 2
    build_record_from_context "$CURRENT_FILE" "$app_commit" "$app_digest" "$admin_digest" || return 2
    read_and_verify_record "$CURRENT_FILE" CURRENT || return 3
    CURRENT_EXISTS=1
    printf 'Recorded the healthy running image pair as the initial deployment baseline.\n'
    diagnostic 'bootstrap=running_image_pair'
}

load_current_state() {
    if [[ -e "$CURRENT_FILE" ]]; then
        local status
        read_and_verify_record "$CURRENT_FILE" CURRENT
        status=$?
        [[ $status -eq 0 ]] || die 'current release record is invalid or its Git commit is unavailable; repair the server state or fetch the recorded commit before retrying' invalid_current_record
        CURRENT_EXISTS=1
        local app_ref="$REGISTRY_ROOT/app@sha256:${CURRENT[app_digest]}"
        local admin_ref="$REGISTRY_ROOT/admin-web@sha256:${CURRENT[admin_web_digest]}"
        verify_running_pair "$app_ref" "$admin_ref" || die 'running service images differ from the recorded successful release; inspect and reconcile them before another update' running_release_mismatch
    else
        [[ ! -e "$PREVIOUS_FILE" ]] || die 'previous release exists without a current release record; reconcile the state directory before deployment' incomplete_release_state
        local app_count admin_count bootstrap_status
        app_count=$(docker ps -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" --filter 'label=com.docker.compose.service=app' 2>/dev/null | awk 'NF { n++ } END { print n+0 }') || app_count=0
        admin_count=$(docker ps -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" --filter 'label=com.docker.compose.service=admin-web' 2>/dev/null | awk 'NF { n++ } END { print n+0 }') || admin_count=0
        if [[ "$app_count" == 0 && "$admin_count" == 0 ]]; then
            mkdir -p -- "$STATE_DIR" || die 'could not create deployment state directory' state_directory_failed
            return 0
        fi
        bootstrap_current_from_running
        bootstrap_status=$?
        if [[ $bootstrap_status -eq 3 ]]; then die 'running deployment source commit is unavailable; fetch the recorded SHA before retrying' bootstrap_commit_unavailable; fi
        [[ $bootstrap_status -eq 0 ]] || die 'running services exist but cannot be verified as one healthy SHA-tag/digest release; reconcile both services before deploying' bootstrap_failed
    fi
    if [[ -e "$PREVIOUS_FILE" ]]; then
        local previous_status
        read_and_verify_record "$PREVIOUS_FILE" TARGET
        previous_status=$?
        [[ $previous_status -eq 0 ]] || die 'previous release record is invalid or its Git commit is unavailable; fetch the recorded commit or repair state before retrying' invalid_previous_record
    fi
}

build_deploy_target() {
    local app_ref=$1 admin_ref=$2 app_digest admin_digest
    app_digest=${app_ref#"$REGISTRY_ROOT/app@sha256:"}
    admin_digest=${admin_ref#"$REGISTRY_ROOT/admin-web@sha256:"}
    valid_hex64 "$app_digest" && valid_hex64 "$admin_digest" || die 'image references must be app/admin-web digest references' invalid_image_digest
    build_record_from_context "$RUNTIME_TMP/target" "$RELEASE_COMMIT" "$app_digest" "$admin_digest" || die 'could not construct the release record' record_build_failed
    read_record "$RUNTIME_TMP/target" DEPLOY_TARGET || die 'constructed release record did not pass validation' record_validation_failed
}

config_change_is_allowed() {
    [[ "${ALLOW_DEPLOY_CONFIG_CHANGE:-}" == true &&
       "${CONFIG_CHANGE_CONFIRMATION:-}" == I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY &&
       "${CI_PIPELINE_SOURCE:-}" == web &&
       "${CI_COMMIT_BRANCH:-}" == "${CI_DEFAULT_BRANCH:-}" &&
       "${CI_COMMIT_REF_PROTECTED:-}" == true ]]
}

compare_target_with_current() {
    local changed=0
    [[ ${TARGET[migration_tree]} == "${CURRENT[migration_tree]}" ]] || changed=1
    [[ ${TARGET[compose_blob]} == "${CURRENT[compose_blob]}" ]] || changed=1
    [[ ${TARGET[mysql_overlay_blob]} == "${CURRENT[mysql_overlay_blob]}" ]] || changed=1
    [[ ${TARGET[redis_overlay_blob]} == "${CURRENT[redis_overlay_blob]}" ]] || changed=1
    [[ ${TARGET[environment_fingerprint]} == "${CURRENT[environment_fingerprint]}" ]] || changed=1
    [[ ${TARGET[runtime_fingerprint]} == "${CURRENT[runtime_fingerprint]}" ]] || changed=1
    return "$changed"
}

compare_candidate_with_current() {
    local changed=0
    [[ ${DEPLOY_TARGET[migration_tree]} == "${CURRENT[migration_tree]}" ]] || changed=1
    [[ ${DEPLOY_TARGET[compose_blob]} == "${CURRENT[compose_blob]}" ]] || changed=1
    [[ ${DEPLOY_TARGET[mysql_overlay_blob]} == "${CURRENT[mysql_overlay_blob]}" ]] || changed=1
    [[ ${DEPLOY_TARGET[redis_overlay_blob]} == "${CURRENT[redis_overlay_blob]}" ]] || changed=1
    [[ ${DEPLOY_TARGET[environment_fingerprint]} == "${CURRENT[environment_fingerprint]}" ]] || changed=1
    [[ ${DEPLOY_TARGET[runtime_fingerprint]} == "${CURRENT[runtime_fingerprint]}" ]] || changed=1
    return "$changed"
}

prepare_compose() {
    local app_ref=$1 admin_ref=$2
    export COMPOSE_PROJECT_NAME MYSQL_NETWORK_NAME APP_PORT ADMIN_WEB_PORT
    export APP_IMAGE_REF=$app_ref ADMIN_WEB_IMAGE_REF=$admin_ref
    compose config --quiet >/dev/null 2>&1 || die 'Compose validation failed; running services were not changed' compose_validation_failed
}

preflight() {
    local command
    for command in docker curl install git sha256sum awk grep; do require_command "$command"; done
    docker compose version >/dev/null 2>&1 || die 'Docker Compose v2 is unavailable on the deployment runner' compose_unavailable
    copy_production_env
    validate_runtime_inputs
    validate_secret_inputs
    mkdir -p -- "$DIAG_DIR" "$STATE_DIR" || die 'could not create deployment state/diagnostic directories' state_directory_failed
    RUNTIME_TMP=$(mktemp -d) || die 'could not create a private temporary directory' temp_directory_failed
    chmod 700 "$RUNTIME_TMP" 2>/dev/null || true
    diagnostic 'phase=preflight'
}

deploy_main() {
    local app_ref=${APP_IMAGE_REF:-} admin_ref=${ADMIN_WEB_IMAGE_REF:-} changed=0 recovery_status
    [[ "${RELEASE_COMMIT:-}" =~ ^[[:xdigit:]]{40}$ ]] || die 'RELEASE_COMMIT must be a 40-character Git SHA' invalid_release_commit
    [[ "$RELEASE_COMMIT" == "$CI_COMMIT_SHA" ]] || die 'RELEASE_COMMIT must equal CI_COMMIT_SHA' release_commit_mismatch
    git_commit_exists "$RELEASE_COMMIT" || die 'RELEASE_COMMIT is absent from the CI checkout' release_commit_unavailable
    [[ "$app_ref" == "$REGISTRY_ROOT/app@sha256:"* && "$admin_ref" == "$REGISTRY_ROOT/admin-web@sha256:"* ]] || die 'APP_IMAGE_REF and ADMIN_WEB_IMAGE_REF must name this project registry digests' invalid_image_ref
    valid_hex64 "${app_ref#"$REGISTRY_ROOT/app@sha256:"}" && valid_hex64 "${admin_ref#"$REGISTRY_ROOT/admin-web@sha256:"}" || die 'APP_IMAGE_REF and ADMIN_WEB_IMAGE_REF must contain SHA-256 digests' invalid_image_ref
    compute_source_fingerprints "$RELEASE_COMMIT" || die 'release commit is missing db/migrations or a stable production Compose file; no production change was made' release_source_incomplete
    build_deploy_target "$app_ref" "$admin_ref"
    load_current_state
    if [[ $CURRENT_EXISTS -eq 1 ]]; then
        compare_candidate_with_current || changed=1
        if [[ $changed -eq 1 ]]; then
            config_change_is_allowed || die 'db/migrations, production Compose/network/Redis topology, env.production, or deployment settings changed; automatic release is blocked. After a verified backup and compatibility review, start a protected default-branch web pipeline with ALLOW_DEPLOY_CONFIG_CHANGE=true and CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY.' config_change_blocked
            AUTO_ROLLBACK_ALLOWED=0
            printf 'Configuration-change exception is active. Backup and backward compatibility were confirmed manually; no database migration or backup will run, and automatic image rollback is disabled.\n' >&2
            diagnostic 'config_change=manual_confirmation'
        fi
    fi
    prepare_compose "$app_ref" "$admin_ref"
    assert_remote_default_head
    if ! pull_images "$app_ref" "$admin_ref"; then die 'one or both immutable images could not be pulled after three attempts; running services were not changed' image_pull_failed; fi
    assert_remote_default_head
    if ! update_services "$app_ref" "$admin_ref"; then
        DIAG_REASON=${LAST_FAILURE:-release_check_failed}
        if [[ $AUTO_ROLLBACK_ALLOWED -eq 1 && $CURRENT_EXISTS -eq 1 ]]; then
            if restore_current_release; then :; else recovery_status=failed; fi
            [[ ${recovery_status:-} == failed ]] && warn 'automatic recovery did not restore a healthy previous pair' recovery_failed
        elif [[ $AUTO_ROLLBACK_ALLOWED -eq 0 ]]; then
            printf 'deployment: release failed under the configuration-change exception; no automatic old-image rollback was attempted. Operator compatibility review is required.\n' >&2
            diagnostic 'recovery=disabled_for_config_change'
        else
            printf 'deployment: no verified previous image pair exists; services were left in place for review. No down/delete command was run.\n' >&2
            diagnostic 'recovery=no_verified_previous_release'
        fi
        exit 1
    fi
    if ! promote_target_record DEPLOY_TARGET; then
        printf 'deployment: release is healthy but the success record could not be atomically updated.\n' >&2
        if [[ $AUTO_ROLLBACK_ALLOWED -eq 1 && $CURRENT_EXISTS -eq 1 ]]; then restore_current_release || warn 'state update failed and automatic recovery also failed' state_and_recovery_failed; fi
        die 'release state update failed; inspect current/previous records before retrying' state_update_failed
    fi
    CURRENT_EXISTS=1
    printf 'Deployment is healthy and the current/previous release records were updated.\n'
    diagnostic 'release=healthy'
    cleanup_images_best_effort
    DIAG_RESULT=success
    DIAG_REASON=release_complete
}

rollback_main() {
    local app_ref admin_ref changed=0
    [[ -f "$PREVIOUS_FILE" ]] || die 'no previous successful release is recorded' previous_release_missing
    load_current_state
    local checkout_commit
    checkout_commit=${CI_COMMIT_SHA}
    compute_source_fingerprints "$checkout_commit" || die 'current CI checkout is missing stable production files required for rollback' rollback_source_incomplete
    # The rollback job must use the topology currently running on production.
    [[ $CTX_COMPOSE == "${CURRENT[compose_blob]}" && $CTX_NETWORK == "${CURRENT[mysql_overlay_blob]}" && $CTX_REDIS == "${CURRENT[redis_overlay_blob]}" ]] || die 'rollback checkout changes the deployed Compose/network/Redis topology; use a pipeline with the current production topology' rollback_topology_mismatch
    [[ $CTX_ENV == "${CURRENT[environment_fingerprint]}" && $CTX_RUNTIME == "${CURRENT[runtime_fingerprint]}" ]] || die 'rollback runner environment or deployment variables differ from the current successful release' rollback_runtime_mismatch
    compare_target_with_current || changed=1
    if [[ $changed -eq 1 ]]; then
        die 'previous release is not compatible with the currently recorded migration, topology, production environment, or deployment settings; database state is never rolled back automatically' rollback_incompatible
    fi
    app_ref="$REGISTRY_ROOT/app@sha256:${TARGET[app_digest]}"
    admin_ref="$REGISTRY_ROOT/admin-web@sha256:${TARGET[admin_web_digest]}"
    prepare_compose "$app_ref" "$admin_ref"
    if ! pull_images "$app_ref" "$admin_ref"; then die 'previous immutable images could not be pulled after three attempts; running services were not changed' rollback_pull_failed; fi
    if ! update_services "$app_ref" "$admin_ref"; then
        DIAG_REASON=${LAST_FAILURE:-rollback_check_failed}
        if [[ $CURRENT_EXISTS -eq 1 ]]; then restore_current_release || warn 'rollback failed and current release recovery did not restore a healthy pair' rollback_recovery_failed; fi
        exit 1
    fi
    if ! promote_target_record TARGET; then
        if [[ $CURRENT_EXISTS -eq 1 ]]; then restore_current_release || warn 'rollback state update failed and current release recovery also failed' rollback_state_recovery_failed; fi
        die 'rollback is healthy but release state could not be updated; inspect current/previous records before retrying' rollback_state_update_failed
    fi
    CURRENT_EXISTS=1
    printf 'Rollback is healthy; current and previous release records were swapped.\n'
    diagnostic 'rollback=healthy'
    cleanup_images_best_effort
    DIAG_RESULT=success
    DIAG_REASON=rollback_complete
}

cleanup_images_best_effort() {
    local cleanup="$PROJECT_DIR/deploy/scripts/cleanup-images.sh"
    if [[ -r "$cleanup" ]]; then
        if ! bash "$cleanup" cleanup-state "$REGISTRY_ROOT" "$STATE_DIR" >/dev/null 2>&1; then
            warn 'old image cleanup failed; release remains successful' cleanup_failed
        else
            diagnostic 'cleanup=completed'
        fi
    else
        warn 'cleanup script is missing; release remains successful' cleanup_script_missing
    fi
}

main() {
    [[ $# -eq 1 ]] || { usage; exit 2; }
    local mode=$1
    case "$mode" in deploy|rollback) ;; *) usage; exit 2 ;; esac
    preflight
    assert_main_pipeline "$mode"
    if [[ "$mode" == deploy ]]; then
        deploy_main
    else
        rollback_main
    fi
}

main "$@"
