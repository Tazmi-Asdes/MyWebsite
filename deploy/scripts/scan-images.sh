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

[ -n "$app_image_ref" ] || fail 'APP_IMAGE_REF is required'
[ -n "$admin_web_image_ref" ] || fail 'ADMIN_WEB_IMAGE_REF is required'
case "$app_image_ref$admin_web_image_ref" in
  *[[:space:]]*) fail 'image references must not contain whitespace' ;;
esac

mkdir -p "$cache_dir" "$report_dir"

scan_json() {
  image_ref=$1
  report_file=$2
  printf 'Saving full vulnerability JSON for %s\n' "$image_ref"
  trivy image \
    --cache-dir "$cache_dir" \
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
