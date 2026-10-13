#!/bin/sh
set -eu

fail() {
  printf 'image scan: %s\n' "$*" >&2
  exit 2
}

project_dir=${CI_PROJECT_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)}
app_image_ref=${APP_IMAGE_REF:-}
admin_web_image_ref=${ADMIN_WEB_IMAGE_REF:-}
cache_dir=${TRIVY_CACHE_DIR:-"$project_dir/.cache/trivy"}
report_dir=${DEPLOY_DIAGNOSTICS_DIR:-"$project_dir/test-results/deploy"}
db_repositories=${MYWEBSITE_TRIVY_DB_REPOSITORIES-'registry.gitlab.com/gitlab-org/security-products/dependencies/trivy-db:2 ghcr.io/aquasecurity/trivy-db:2'}
db_download_timeout=${MYWEBSITE_TRIVY_DB_DOWNLOAD_TIMEOUT-3m}

[ -n "$app_image_ref" ] || fail 'APP_IMAGE_REF is required'
[ -n "$admin_web_image_ref" ] || fail 'ADMIN_WEB_IMAGE_REF is required'
case "$app_image_ref$admin_web_image_ref" in
  *[[:space:]]*) fail 'image references must not contain whitespace' ;;
esac

mkdir -p "$cache_dir" "$report_dir"

[ -n "$db_download_timeout" ] || fail 'MYWEBSITE_TRIVY_DB_DOWNLOAD_TIMEOUT must not be empty'

# Split the configurable whitespace-separated repository list without allowing
# repository names to expand as filesystem globs.
set -f
old_ifs=$IFS
IFS=$(printf ' \t\n_')
IFS=${IFS%_}
set -- $db_repositories
IFS=$old_ifs
[ "$#" -gt 0 ] || fail 'MYWEBSITE_TRIVY_DB_REPOSITORIES must contain at least one repository'

db_ready=false
while [ "$#" -gt 0 ]; do
  repository=$1
  shift
  printf 'Prewarming Trivy vulnerability database from %s (timeout %s)\n' \
    "$repository" "$db_download_timeout"
  if trivy image \
    --cache-dir "$cache_dir" \
    --db-repository "$repository" \
    --timeout "$db_download_timeout" \
    --download-db-only \
    --no-progress; then
    db_ready=true
    break
  else
    if [ "$#" -gt 0 ]; then
      printf 'Trivy database download from %s failed; switching to %s\n' \
        "$repository" "$1" >&2
    else
      printf 'Trivy database download from %s failed; no configured sources remain\n' \
        "$repository" >&2
    fi
  fi
done
[ "$db_ready" = true ] || fail 'unable to download the Trivy vulnerability database from any configured repository'

scan_json() {
  image_ref=$1
  report_file=$2
  printf 'Saving full vulnerability JSON for %s\n' "$image_ref"
  trivy image \
    --cache-dir "$cache_dir" \
    --skip-db-update \
    --image-src remote \
    --scanners vuln \
    --format json \
    --output "$report_file" \
    "$image_ref"
  [ -s "$report_file" ] || fail "Trivy did not write $report_file"
}

gate_image() {
  image_ref=$1
  printf 'Applying HIGH/CRITICAL fixed-vulnerability gate to %s\n' "$image_ref"
  trivy image \
    --cache-dir "$cache_dir" \
    --skip-db-update \
    --image-src remote \
    --scanners vuln \
    --severity HIGH,CRITICAL \
    --ignore-unfixed \
    --exit-code 1 \
    "$image_ref"
}

# Save both complete reports before applying either blocking gate. A scan
# failure is returned immediately so infrastructure or registry errors cannot
# be mistaken for a clean result.
scan_json "$app_image_ref" "$report_dir/app-vulnerabilities.json"
scan_json "$admin_web_image_ref" "$report_dir/admin-web-vulnerabilities.json"

gate_image "$app_image_ref"
gate_image "$admin_web_image_ref"
