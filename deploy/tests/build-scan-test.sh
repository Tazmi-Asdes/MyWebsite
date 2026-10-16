#!/usr/bin/env bash
set -Eeuo pipefail

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
temp_root=$(mktemp -d)
trap 'rm -rf -- "$temp_root"' EXIT
mock_bin="$temp_root/bin"
mock_project="$temp_root/project"
mkdir -p -- "$mock_bin" "$mock_project"
mock_bin_path=$(cd -- "$mock_bin" && pwd)
mock_project=$(cd -- "$mock_project" && pwd)

cat >"$mock_bin/docker" <<'MOCK_DOCKER'
#!/usr/bin/env bash
set -eu
printf 'docker %s\n' "$*" >>"$MOCK_DOCKER_LOG"

case "$1" in
  login)
    cat >/dev/null
    ;;
  build)
    ;;
  push)
    image_ref=$2
    case "$image_ref" in
      */app:*)
        if [ "${MOCK_FAIL_APP_PUSH_ONCE:-false}" = true ] && [ ! -e "$MOCK_APP_PUSH_MARKER" ]; then
          touch "$MOCK_APP_PUSH_MARKER"
          exit 7
        fi
        ;;
      */site-web:*)
        if [ "${MOCK_FAIL_SITE_PUSH_ALL:-false}" = true ]; then exit 8; fi
        ;;
    esac
    ;;
  image)
    [ "$2" = inspect ] || exit 40
    image_tag=${*: -1}
    case "$image_tag" in
      */app:*)
        printf '%s\n' \
          'registry.example/other/app@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
          'registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
          'registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        ;;
      */admin-web:*)
        printf '%s\n' \
          'registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
          'registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
        ;;
      */site-web:*)
        if [ "${MOCK_SITE_WRONG_REPO_DIGEST:-false}" = true ]; then
          printf '%s\n' \
            'registry.example/other/site-web@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd'
        else
          printf '%s\n' \
            'registry.example/group/project/site-web@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
        fi
        ;;
      *)
        exit 41
        ;;
    esac
    ;;
  *)
    exit 42
    ;;
esac
MOCK_DOCKER

cat >"$mock_bin/sleep" <<'MOCK_SLEEP'
#!/usr/bin/env bash
set -eu
printf 'sleep %s\n' "$*" >>"$MOCK_DOCKER_LOG"
MOCK_SLEEP

cat >"$mock_bin/trivy" <<'MOCK_TRIVY'
#!/usr/bin/env bash
set -eu
printf 'trivy %s\n' "$*" >>"$MOCK_TRIVY_LOG"

if [ "$1" = registry ] && [ "$2" = login ]; then
  cat >/dev/null
  exit 0
fi

[ "$1" = image ] || exit 50
args=("$@")
report=''
gate=false
download_db=false
db_repository=''
image_ref=${args[${#args[@]}-1]}
for ((i = 0; i < ${#args[@]}; i++)); do
  case ${args[$i]} in
    --output)
      report=${args[$((i + 1))]}
      ;;
    --db-repository)
      db_repository=${args[$((i + 1))]}
      ;;
    --download-db-only)
      download_db=true
      ;;
    --exit-code)
      gate=true
      ;;
  esac
done

if [ "$download_db" = true ]; then
  [ -n "$db_repository" ] || exit 52
  if [ "${TRIVY_FAIL_DB_ALL:-false}" = true ] || [ "${TRIVY_FAIL_DB_REPOSITORY:-}" = "$db_repository" ]; then
    printf 'mock Trivy database download failed for %s\n' "$db_repository" >&2
    exit 9
  fi
  exit 0
fi

if [ "$gate" = false ]; then
  [ -n "$report" ] || exit 51
  [[ " ${args[*]} " == *' --skip-db-update '* ]] || exit 53
  printf '{"image":"%s","results":[]}\n' "$image_ref" >"$report"
  if [ "${TRIVY_FAIL_SCAN_FOR:-}" = "$image_ref" ]; then
    exit 8
  fi
elif [ "${TRIVY_FAIL_GATE_FOR:-}" = "$image_ref" ]; then
  exit 1
fi
MOCK_TRIVY

chmod +x "$mock_bin/docker" "$mock_bin/sleep" "$mock_bin/trivy"
export PATH="$mock_bin_path:$PATH"
export CI_PROJECT_DIR="$mock_project"
export CI_COMMIT_SHA=0123456789abcdef0123456789abcdef01234567
export CI_JOB_ID=mock-job-123
export CI_REGISTRY=registry.example
export CI_REGISTRY_IMAGE=registry.example/group/project
export CI_REGISTRY_USER=mock-user
export CI_REGISTRY_PASSWORD=mock-password
export DOCKER_CONFIG="docker-config"
export PULL_BASE_IMAGES=false
export MOCK_DOCKER_LOG="../docker.log"
export MOCK_APP_PUSH_MARKER="../app-push-failed-once"
export MOCK_FAIL_APP_PUSH_ONCE=true
export MOCK_TRIVY_LOG="$temp_root/trivy.log"

bash "$project_root/deploy/scripts/build-images.sh"

export MOCK_DOCKER_LOG="$temp_root/docker.log"
[[ $(grep -c '^docker login ' "$MOCK_DOCKER_LOG") -eq 1 ]]
[[ $(grep -c '^docker build ' "$MOCK_DOCKER_LOG") -eq 3 ]]
grep -Fq -- '--target app-runtime' "$MOCK_DOCKER_LOG"
grep -Fq -- '--target admin-web' "$MOCK_DOCKER_LOG"
grep -Fq -- '--target site-runtime' "$MOCK_DOCKER_LOG"
[[ $(grep -c '^docker push registry.example/group/project/app:' "$MOCK_DOCKER_LOG") -eq 2 ]]
[[ $(grep -c '^docker push registry.example/group/project/admin-web:' "$MOCK_DOCKER_LOG") -eq 1 ]]
[[ $(grep -c '^docker push registry.example/group/project/site-web:' "$MOCK_DOCKER_LOG") -eq 1 ]]
grep -Fq 'APP_IMAGE_REF=registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$CI_PROJECT_DIR/release.env"
grep -Fq 'ADMIN_WEB_IMAGE_REF=registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$CI_PROJECT_DIR/release.env"
grep -Fq 'SITE_WEB_IMAGE_REF=registry.example/group/project/site-web@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' "$CI_PROJECT_DIR/release.env"
grep -Fq "RELEASE_COMMIT=$CI_COMMIT_SHA" "$CI_PROJECT_DIR/release.env"
if grep -Fq ':latest' "$MOCK_DOCKER_LOG"; then
  printf '%s\n' 'build script must not use latest tags' >&2
  exit 1
fi

failed_project="$temp_root/project-site-push-failure"
mkdir -p -- "$failed_project"
failed_project=$(cd -- "$failed_project" && pwd)
export CI_PROJECT_DIR="$failed_project"
export MOCK_DOCKER_LOG="../docker-site-push-failure.log"
export MOCK_FAIL_SITE_PUSH_ALL=true
if bash "$project_root/deploy/scripts/build-images.sh"; then
  printf '%s\n' 'build script must fail when all site-web push attempts fail' >&2
  exit 1
fi
unset MOCK_FAIL_SITE_PUSH_ALL
export MOCK_DOCKER_LOG="$temp_root/docker-site-push-failure.log"
[[ $(grep -c '^docker push registry.example/group/project/site-web:' "$MOCK_DOCKER_LOG") -eq 3 ]]
[[ ! -e "$CI_PROJECT_DIR/release.env" ]]

failed_project="$temp_root/project-site-digest-failure"
mkdir -p -- "$failed_project"
failed_project=$(cd -- "$failed_project" && pwd)
export CI_PROJECT_DIR="$failed_project"
export MOCK_DOCKER_LOG="../docker-site-digest-failure.log"
export MOCK_SITE_WRONG_REPO_DIGEST=true
if bash "$project_root/deploy/scripts/build-images.sh"; then
  printf '%s\n' 'build script must reject a site-web RepoDigest from another repository' >&2
  exit 1
fi
unset MOCK_SITE_WRONG_REPO_DIGEST
export MOCK_DOCKER_LOG="$temp_root/docker-site-digest-failure.log"
[[ ! -e "$CI_PROJECT_DIR/release.env" ]]
grep -Fq -- 'docker image inspect --format ' "$MOCK_DOCKER_LOG"
grep -Fq 'registry.example/group/project/site-web:' "$MOCK_DOCKER_LOG"

export CI_PROJECT_DIR="$mock_project"
export MOCK_DOCKER_LOG="$temp_root/docker.log"

export APP_IMAGE_REF=registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export ADMIN_WEB_IMAGE_REF=registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
export TRIVY_CACHE_DIR="$temp_root/.cache/trivy"
export MYWEBSITE_TRIVY_DB_REPOSITORIES='mock.registry/first:2 mock.registry/second:2'
export MYWEBSITE_TRIVY_DB_DOWNLOAD_TIMEOUT=17s
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-success"
sh "$project_root/deploy/scripts/scan-images.sh"

[[ -s "$DEPLOY_DIAGNOSTICS_DIR/app-vulnerabilities.json" ]]
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/admin-web-vulnerabilities.json" ]]
[[ $(grep -c '^trivy image ' "$MOCK_TRIVY_LOG") -eq 5 ]]
[[ $(grep -c -- '--download-db-only' "$MOCK_TRIVY_LOG") -eq 1 ]]
[[ $(grep -c -- '--db-repository mock.registry/first:2' "$MOCK_TRIVY_LOG") -eq 1 ]]
[[ $(grep -c -- '--timeout 17s' "$MOCK_TRIVY_LOG") -eq 1 ]]
[[ $(grep -c -- '--skip-db-update' "$MOCK_TRIVY_LOG") -eq 4 ]]
grep -Fq -- '--image-src remote' "$MOCK_TRIVY_LOG"
grep -Fq -- '--severity HIGH,CRITICAL' "$MOCK_TRIVY_LOG"
grep -Fq -- '--ignore-unfixed' "$MOCK_TRIVY_LOG"
grep -Fq -- '--exit-code 1' "$MOCK_TRIVY_LOG"

export MOCK_TRIVY_LOG="$temp_root/trivy-db-failover.log"
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-db-failover"
export TRIVY_FAIL_DB_REPOSITORY='mock.registry/first:2'
sh "$project_root/deploy/scripts/scan-images.sh"
unset TRIVY_FAIL_DB_REPOSITORY
[[ $(grep -c '^trivy image ' "$MOCK_TRIVY_LOG") -eq 6 ]]
[[ $(grep -c -- '--download-db-only' "$MOCK_TRIVY_LOG") -eq 2 ]]
[[ $(grep -c -- '--skip-db-update' "$MOCK_TRIVY_LOG") -eq 4 ]]
[[ $(grep -c -- '--db-repository mock.registry/first:2' "$MOCK_TRIVY_LOG") -eq 1 ]]
[[ $(grep -c -- '--db-repository mock.registry/second:2' "$MOCK_TRIVY_LOG") -eq 1 ]]
first_db_attempt=$(grep -nF -- '--db-repository mock.registry/first:2' "$MOCK_TRIVY_LOG" | cut -d: -f1)
second_db_attempt=$(grep -nF -- '--db-repository mock.registry/second:2' "$MOCK_TRIVY_LOG" | cut -d: -f1)
[[ $first_db_attempt -lt $second_db_attempt ]]
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/app-vulnerabilities.json" ]]
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/admin-web-vulnerabilities.json" ]]

export MOCK_TRIVY_LOG="$temp_root/trivy-db-failure.log"
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-db-failure"
export TRIVY_FAIL_DB_ALL=true
if sh "$project_root/deploy/scripts/scan-images.sh"; then
  printf '%s\n' 'scan script must fail when every Trivy database source fails' >&2
  exit 1
fi
unset TRIVY_FAIL_DB_ALL
[[ $(grep -c -- '--download-db-only' "$MOCK_TRIVY_LOG") -eq 2 ]]
! grep -Fq -- '--format json' "$MOCK_TRIVY_LOG"
! grep -Fq -- '--exit-code 1' "$MOCK_TRIVY_LOG"
[[ ! -e "$DEPLOY_DIAGNOSTICS_DIR/app-vulnerabilities.json" ]]
[[ ! -e "$DEPLOY_DIAGNOSTICS_DIR/admin-web-vulnerabilities.json" ]]

export MOCK_TRIVY_LOG="$temp_root/trivy-db-empty.log"
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-db-empty"
export MYWEBSITE_TRIVY_DB_REPOSITORIES='   '
if sh "$project_root/deploy/scripts/scan-images.sh"; then
  printf '%s\n' 'scan script must reject an empty Trivy database source list' >&2
  exit 1
fi
[[ ! -s "$MOCK_TRIVY_LOG" ]]
unset MYWEBSITE_TRIVY_DB_REPOSITORIES

export MOCK_TRIVY_LOG="$temp_root/trivy-gate-failure.log"
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-gate-failure"
export TRIVY_FAIL_GATE_FOR="$APP_IMAGE_REF"
if sh "$project_root/deploy/scripts/scan-images.sh"; then
  printf '%s\n' 'scan script must fail when the blocking gate finds a vulnerability' >&2
  exit 1
fi
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/app-vulnerabilities.json" ]]
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/admin-web-vulnerabilities.json" ]]
json_scan_count=$(grep -c -- '--format json' "$MOCK_TRIVY_LOG")
[[ $json_scan_count -eq 2 ]]
[[ $(grep -c -- '--download-db-only' "$MOCK_TRIVY_LOG") -eq 1 ]]
[[ $(grep -c -- '--skip-db-update' "$MOCK_TRIVY_LOG") -eq 3 ]]
grep -Fq -- '--ignore-unfixed' "$MOCK_TRIVY_LOG"
grep -Fq -- '--severity HIGH,CRITICAL' "$MOCK_TRIVY_LOG"

printf '%s\n' 'build and scan mock checks passed'
