#!/usr/bin/env bash
set -uo pipefail
export LC_ALL=C

warn() {
    printf 'image cleanup: warning: %s\n' "$*" >&2
}

valid_sha() {
    [[ ${#1} -eq 40 && "$1" =~ ^[[:xdigit:]]{40}$ ]]
}

valid_ref() {
    local ref=$1
    local repo=$2
    local tag
    [[ "$ref" == "$repo:"* ]] || return 1
    tag=${ref#"$repo:"}
    valid_sha "$tag"
}

write_history() {
    local state_path=$1
    local current_app=$2
    local current_admin=$3
    local previous_app=$4
    local previous_admin=$5
    local directory temporary

    if [[ "$state_path" == */* ]]; then
        directory=${state_path%/*}
        [[ -n "$directory" ]] || directory=/
    else
        directory=.
    fi

    temporary=$(mktemp "$directory/.last-successful-images.XXXXXX" 2>/dev/null) || return 1
    if ! printf '%s\n' "$current_app" "$current_admin" "$previous_app" "$previous_admin" >"$temporary"; then
        rm -f -- "$temporary" 2>/dev/null || true
        return 1
    fi
    if ! mv -f -- "$temporary" "$state_path" 2>/dev/null; then
        rm -f -- "$temporary" 2>/dev/null || true
        return 1
    fi
}

read_history() {
    local state_path=$1
    local registry_root=$2
    HISTORY=()

    [[ -f "$state_path" && -r "$state_path" ]] || return 1
    mapfile -t HISTORY <"$state_path" || return 1
    [[ ${#HISTORY[@]} -eq 4 ]] || return 1
    valid_ref "${HISTORY[0]}" "$registry_root/app" || return 1
    valid_ref "${HISTORY[1]}" "$registry_root/admin-web" || return 1
    if [[ -z ${HISTORY[2]} && -z ${HISTORY[3]} ]]; then
        return 0
    fi
    valid_ref "${HISTORY[2]}" "$registry_root/app" || return 1
    valid_ref "${HISTORY[3]}" "$registry_root/admin-web" || return 1
}

bootstrap_history() {
    local registry_root=$1
    local state_path=$2
    local app_container=$3
    local admin_container=$4
    local live_url=$5
    local ready_url=$6
    local admin_url=$7
    local app_ref admin_ref

    if [[ -e "$state_path" ]]; then
        warn 'history already exists; bootstrap skipped'
        return 0
    fi
    if [[ -z "$registry_root" || -z "$app_container" || -z "$admin_container" ]]; then
        warn 'previous service containers or registry path are unavailable; bootstrap skipped'
        return 0
    fi
    if ! curl --fail --silent --show-error --max-time 5 -o /dev/null "$live_url" >/dev/null 2>&1 ||
       ! curl --fail --silent --show-error --max-time 5 -o /dev/null "$ready_url" >/dev/null 2>&1 ||
       ! curl --fail --silent --show-error --max-time 5 -o /dev/null "$admin_url" >/dev/null 2>&1; then
        warn 'previous service health checks failed; bootstrap skipped'
        return 0
    fi
    if ! app_ref=$(docker inspect --format '{{.Config.Image}}' "$app_container" 2>/dev/null) ||
       ! admin_ref=$(docker inspect --format '{{.Config.Image}}' "$admin_container" 2>/dev/null); then
        warn 'previous service image references could not be inspected; bootstrap skipped'
        return 0
    fi
    if ! valid_ref "$app_ref" "$registry_root/app" ||
       ! valid_ref "$admin_ref" "$registry_root/admin-web"; then
        warn 'previous service image references are not exact commit-SHA tags; bootstrap skipped'
        return 0
    fi
    if ! write_history "$state_path" "$app_ref" "$admin_ref" '' ''; then
        warn 'history could not be written; bootstrap skipped'
        return 0
    fi
    printf 'Recorded healthy previous deployment as the initial image history.\n'
}

is_kept_ref() {
    local ref=$1
    local current_app=$2
    local current_admin=$3
    local previous_app=$4
    local previous_admin=$5
    [[ "$ref" == "$current_app" || "$ref" == "$current_admin" ||
       ( -n "$previous_app" && "$ref" == "$previous_app" ) ||
       ( -n "$previous_admin" && "$ref" == "$previous_admin" ) ]]
}

cleanup_images() {
    local registry_root=$1
    local commit_sha=$2
    local state_path=$3
    local new_app new_admin previous_app previous_admin
    local image_rows containers line repository tag extra ref image_id
    local container_id container_record container_image_id container_config_ref
    local candidate_index container_index kept_index used allow_shared
    local -a candidates=() candidate_ids=() container_ids=() container_image_ids=()
    local -a container_config_refs=() kept_refs=() kept_ids=()
    local -a next_history=()

    if [[ -z "$registry_root" ]] || ! valid_sha "$commit_sha"; then
        warn 'registry path or commit SHA is invalid; cleanup skipped'
        return 0
    fi
    new_app="$registry_root/app:$commit_sha"
    new_admin="$registry_root/admin-web:$commit_sha"

    if [[ ! -e "$state_path" ]]; then
        if ! write_history "$state_path" "$new_app" "$new_admin" '' ''; then
            warn 'initial history could not be written; no images were removed'
            return 0
        fi
        warn 'no previous successful history exists; recorded this deployment as a baseline and skipped deletion'
        return 0
    fi
    if ! read_history "$state_path" "$registry_root"; then
        warn 'history is unreadable or invalid; no images were removed'
        return 0
    fi

    if [[ ${HISTORY[0]} == "$new_app" && ${HISTORY[1]} == "$new_admin" ]]; then
        previous_app=${HISTORY[2]}
        previous_admin=${HISTORY[3]}
    else
        previous_app=${HISTORY[0]}
        previous_admin=${HISTORY[1]}
    fi
    next_history=("$new_app" "$new_admin" "$previous_app" "$previous_admin")

    if ! write_history "$state_path" "${next_history[0]}" "${next_history[1]}" "${next_history[2]}" "${next_history[3]}"; then
        warn 'updated successful history could not be written; no images were removed'
        return 0
    fi

    if ! image_rows=$(docker image ls --format '{{.Repository}} {{.Tag}}' 2>/dev/null); then
        warn 'local image list could not be read; no images were removed'
        return 0
    fi
    if ! containers=$(docker ps -aq 2>/dev/null); then
        warn 'container list could not be read; no images were removed'
        return 0
    fi

    kept_refs=("$new_app" "$new_admin")
    [[ -n "$previous_app" ]] && kept_refs+=("$previous_app")
    [[ -n "$previous_admin" ]] && kept_refs+=("$previous_admin")
    for ref in "${kept_refs[@]}"; do
        if ! image_id=$(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null) ||
           [[ ! "$image_id" =~ ^sha256:[[:xdigit:]]{64}$ ]]; then
            warn "retained image $ref could not be verified; no images were removed"
            return 0
        fi
        kept_ids+=("$image_id")
    done

    while IFS= read -r container_id; do
        [[ -n "$container_id" ]] || continue
        if ! container_record=$(docker inspect --format '{{.Image}}|{{.Config.Image}}' "$container_id" 2>/dev/null) ||
           [[ "$container_record" != *'|'* ]]; then
            warn "container $container_id could not be inspected; no images were removed"
            return 0
        fi
        container_image_id=${container_record%%|*}
        container_config_ref=${container_record#*|}
        if [[ ! "$container_image_id" =~ ^sha256:[[:xdigit:]]{64}$ || -z "$container_config_ref" ]]; then
            warn "container $container_id returned incomplete image metadata; no images were removed"
            return 0
        fi
        container_ids+=("$container_id")
        container_image_ids+=("$container_image_id")
        container_config_refs+=("$container_config_ref")
    done <<<"$containers"

    while IFS=' ' read -r repository tag extra; do
        [[ -n "$repository" && -n "$tag" && -z "$extra" ]] || continue
        [[ "$repository" == "$registry_root/app" || "$repository" == "$registry_root/admin-web" ]] || continue
        valid_sha "$tag" || continue
        ref="$repository:$tag"
        is_kept_ref "$ref" "$new_app" "$new_admin" "$previous_app" "$previous_admin" && continue
        if ! image_id=$(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null) ||
           [[ ! "$image_id" =~ ^sha256:[[:xdigit:]]{64}$ ]]; then
            warn "candidate image $ref could not be inspected; no images were removed"
            return 0
        fi
        candidates+=("$ref")
        candidate_ids+=("$image_id")
    done <<<"$image_rows"

    for candidate_index in "${!candidates[@]}"; do
        ref=${candidates[$candidate_index]}
        image_id=${candidate_ids[$candidate_index]}
        used=0
        for container_index in "${!container_ids[@]}"; do
            if [[ ${container_config_refs[$container_index]} == "$ref" ]]; then
                used=1
                break
            fi
            if [[ ${container_image_ids[$container_index]} == "$image_id" ]]; then
                allow_shared=0
                for kept_index in "${!kept_ids[@]}"; do
                    if [[ ${kept_ids[$kept_index]} == "$image_id" ]]; then
                        allow_shared=1
                        break
                    fi
                done
                if [[ $allow_shared -ne 1 ]]; then
                    used=1
                    break
                fi
            fi
        done
        if [[ $used -eq 1 ]]; then
            warn "candidate $ref is referenced by a container; kept it"
            continue
        fi
        if docker image rm --no-prune "$ref" >/dev/null 2>&1; then
            printf 'Removed old local image tag %s.\n' "$ref"
        else
            warn "could not remove old local image tag $ref"
        fi
    done
}

usage() {
    printf 'usage: %s bootstrap <registry-root> <state-path> <app-container> <admin-container> <live-url> <ready-url> <admin-url>\n' "${0##*/}" >&2
    printf '       %s cleanup <registry-root> <commit-sha> <state-path>\n' "${0##*/}" >&2
}

mode=${1:-}
case "$mode" in
    bootstrap)
        [[ $# -eq 8 ]] || { usage; exit 2; }
        bootstrap_history "$2" "$3" "$4" "$5" "$6" "$7" "$8"
        ;;
    cleanup)
        [[ $# -eq 4 ]] || { usage; exit 2; }
        cleanup_images "$2" "$3" "$4"
        ;;
    *)
        usage
        exit 2
        ;;
esac
