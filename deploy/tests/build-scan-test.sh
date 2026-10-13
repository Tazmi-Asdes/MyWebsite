#!/usr/bin/env bash
set -Eeuo pipefail

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
temp_root=$(mktemp -d)
trap 'rm -rf -- "$temp_root"' EXIT
mock_bin="$temp_root/bin"
mock_project="$temp_root/project"
mkdir -p -- "$mock_bin" "$mock_project"

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
image_ref=${args[${#args[@]}-1]}
for ((i = 0; i < ${#args[@]}; i++)); do
  case ${args[$i]} in
    --output)
      report=${args[$((i + 1))]}
      ;;
    --exit-code)
      gate=true
      ;;
  esac
done

if [ "$gate" = false ]; then
  [ -n "$report" ] || exit 51
  printf '{"image":"%s","results":[]}\n' "$image_ref" >"$report"
  if [ "${TRIVY_FAIL_SCAN_FOR:-}" = "$image_ref" ]; then
    exit 8
  fi
elif [ "${TRIVY_FAIL_GATE_FOR:-}" = "$image_ref" ]; then
  exit 1
fi
MOCK_TRIVY

chmod +x "$mock_bin/docker" "$mock_bin/trivy"
export PATH="$mock_bin:$PATH"
export CI_PROJECT_DIR="$mock_project"
export CI_COMMIT_SHA=0123456789abcdef0123456789abcdef01234567
export CI_JOB_ID=mock-job-123
export CI_REGISTRY=registry.example
export CI_REGISTRY_IMAGE=registry.example/group/project
export CI_REGISTRY_USER=mock-user
export CI_REGISTRY_PASSWORD=mock-password
export DOCKER_CONFIG="$temp_root/docker-config"
export PULL_BASE_IMAGES=false
export MOCK_DOCKER_LOG="$temp_root/docker.log"
export MOCK_APP_PUSH_MARKER="$temp_root/app-push-failed-once"
export MOCK_FAIL_APP_PUSH_ONCE=true
export MOCK_TRIVY_LOG="$temp_root/trivy.log"

bash "$project_root/deploy/scripts/build-images.sh"

[[ $(grep -c '^docker login ' "$MOCK_DOCKER_LOG") -eq 1 ]]
[[ $(grep -c '^docker build ' "$MOCK_DOCKER_LOG") -eq 2 ]]
grep -Fq -- '--target app-runtime' "$MOCK_DOCKER_LOG"
grep -Fq -- '--target admin-web' "$MOCK_DOCKER_LOG"
[[ $(grep -c '^docker push registry.example/group/project/app:' "$MOCK_DOCKER_LOG") -eq 2 ]]
[[ $(grep -c '^docker push registry.example/group/project/admin-web:' "$MOCK_DOCKER_LOG") -eq 1 ]]
grep -Fq 'APP_IMAGE_REF=registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$CI_PROJECT_DIR/release.env"
grep -Fq 'ADMIN_WEB_IMAGE_REF=registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' "$CI_PROJECT_DIR/release.env"
grep -Fq "RELEASE_COMMIT=$CI_COMMIT_SHA" "$CI_PROJECT_DIR/release.env"
if grep -Fq ':latest' "$MOCK_DOCKER_LOG"; then
  printf '%s\n' 'build script must not use latest tags' >&2
  exit 1
fi

export APP_IMAGE_REF=registry.example/group/project/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export ADMIN_WEB_IMAGE_REF=registry.example/group/project/admin-web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
export TRIVY_CACHE_DIR="$CI_PROJECT_DIR/.cache/trivy"
export DEPLOY_DIAGNOSTICS_DIR="$temp_root/reports-success"
sh "$project_root/deploy/scripts/scan-images.sh"

[[ -s "$DEPLOY_DIAGNOSTICS_DIR/app-vulnerabilities.json" ]]
[[ -s "$DEPLOY_DIAGNOSTICS_DIR/admin-web-vulnerabilities.json" ]]
[[ $(grep -c '^trivy image ' "$MOCK_TRIVY_LOG") -eq 4 ]]
grep -Fq -- '--image-src remote' "$MOCK_TRIVY_LOG"
grep -Fq -- '--severity HIGH,CRITICAL' "$MOCK_TRIVY_LOG"
grep -Fq -- '--ignore-unfixed' "$MOCK_TRIVY_LOG"
grep -Fq -- '--exit-code 1' "$MOCK_TRIVY_LOG"

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

printf '%s\n' 'build and scan mock checks passed'
