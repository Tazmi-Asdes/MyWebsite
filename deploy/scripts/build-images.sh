#!/usr/bin/env bash
set -Eeuo pipefail

die() {
  printf 'image build: %s\n' "$*" >&2
  exit 2
}

project_dir=${CI_PROJECT_DIR:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}
cd -- "$project_dir"

[[ ${CI_COMMIT_SHA:-} =~ ^[[:xdigit:]]{40}$ ]] || die 'CI_COMMIT_SHA must be a full 40-character Git SHA'
[[ -n ${CI_REGISTRY:-} ]] || die 'CI_REGISTRY is required'
[[ -n ${CI_REGISTRY_IMAGE:-} ]] || die 'CI_REGISTRY_IMAGE is required'
[[ -n ${CI_REGISTRY_USER:-} ]] || die 'CI_REGISTRY_USER is required'
[[ -n ${CI_REGISTRY_PASSWORD:-} ]] || die 'CI_REGISTRY_PASSWORD is required'
[[ -n ${DOCKER_CONFIG:-} ]] || die 'DOCKER_CONFIG must be job-specific'

case ${PULL_BASE_IMAGES:-false} in
  false) pull_args=() ;;
  true) pull_args=(--pull) ;;
  *) die 'PULL_BASE_IMAGES must be true or false' ;;
esac

mkdir -p -- "$DOCKER_CONFIG"
printf '%s' "$CI_REGISTRY_PASSWORD" | docker login \
  --username "$CI_REGISTRY_USER" \
  --password-stdin "$CI_REGISTRY"

build_args=(
  --build-arg "GO_VERSION=${GO_VERSION:-1.27.1}"
  --build-arg "NODE_VERSION=${NODE_VERSION:-24.16.0}"
  --build-arg "GOPROXY=${GOPROXY:-https://goproxy.cn,direct}"
  --build-arg "GOSUMDB=${GOSUMDB:-sum.golang.org}"
  --build-arg "NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY:-https://registry.npmjs.org}"
)

# BuildKit treats these built-in proxy arguments specially, keeping their
# values out of image history and cache metadata.
for proxy_name in HTTP_PROXY HTTPS_PROXY FTP_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy ftp_proxy all_proxy no_proxy; do
  proxy_value=${!proxy_name:-}
  if [[ -n $proxy_value ]]; then
    build_args+=(--build-arg "$proxy_name=$proxy_value")
  fi
done

app_repository="${CI_REGISTRY_IMAGE}/app"
admin_repository="${CI_REGISTRY_IMAGE}/admin-web"
site_repository="${CI_REGISTRY_IMAGE}/site-web"
app_tag="${app_repository}:${CI_COMMIT_SHA}"
admin_tag="${admin_repository}:${CI_COMMIT_SHA}"
site_tag="${site_repository}:${CI_COMMIT_SHA}"

printf 'Building app image for commit %s\n' "$CI_COMMIT_SHA"
docker build "${pull_args[@]}" "${build_args[@]}" \
  --target app-runtime \
  --tag "$app_tag" \
  .

printf 'Building admin-web image for commit %s\n' "$CI_COMMIT_SHA"
docker build "${pull_args[@]}" "${build_args[@]}" \
  --target admin-web \
  --tag "$admin_tag" \
  .

printf 'Building site-web image for commit %s\n' "$CI_COMMIT_SHA"
docker build "${pull_args[@]}" "${build_args[@]}" \
  --target site-runtime \
  --tag "$site_tag" \
  .

push_with_retry() {
  local image_ref=$1
  local attempt=1
  local status

  while (( attempt <= 3 )); do
    printf 'Pushing %s (attempt %s/3)\n' "$image_ref" "$attempt"
    if docker push "$image_ref"; then
      return 0
    else
      status=$?
    fi

    if (( attempt == 3 )); then
      printf 'Push failed after 3 attempts (exit code %s)\n' "$status" >&2
      return "$status"
    fi

    sleep "$((attempt * 2))"
    attempt=$((attempt + 1))
  done
}

image_digest_ref() {
  local image_tag=$1
  local repository=$2
  local repo_digests candidate digest selected=''

  repo_digests=$(docker image inspect \
    --format '{{range .RepoDigests}}{{println .}}{{end}}' \
    "$image_tag") || die "could not inspect pushed image $image_tag"

  while IFS= read -r candidate; do
    [[ $candidate == "${repository}"@sha256:* ]] || continue
    digest=${candidate#"${repository}"@sha256:}
    [[ $digest =~ ^[[:xdigit:]]{64}$ ]] || continue
    if [[ -n $selected && $selected != "$digest" ]]; then
      die "multiple different RepoDigests found for $repository"
    fi
    selected=$digest
  done <<< "$repo_digests"

  [[ -n $selected ]] || die "no valid RepoDigest found for $repository after push"
  printf '%s@sha256:%s' "$repository" "$selected"
}

push_with_retry "$app_tag"
push_with_retry "$admin_tag"
push_with_retry "$site_tag"

app_image_ref=$(image_digest_ref "$app_tag" "$app_repository")
admin_web_image_ref=$(image_digest_ref "$admin_tag" "$admin_repository")
site_web_image_ref=$(image_digest_ref "$site_tag" "$site_repository")
release_env_tmp="${project_dir}/.release.env.tmp.${CI_JOB_ID:-$$}"
trap 'rm -f -- "$release_env_tmp"' EXIT
umask 077
{
  printf 'RELEASE_COMMIT=%s\n' "$CI_COMMIT_SHA"
  printf 'APP_IMAGE_REF=%s\n' "$app_image_ref"
  printf 'ADMIN_WEB_IMAGE_REF=%s\n' "$admin_web_image_ref"
  printf 'SITE_WEB_IMAGE_REF=%s\n' "$site_web_image_ref"
} >"$release_env_tmp"
mv -f -- "$release_env_tmp" "$project_dir/release.env"
trap - EXIT

printf 'Built immutable release %s\n' "$CI_COMMIT_SHA"
printf 'APP_IMAGE_REF=%s\n' "$app_image_ref"
printf 'ADMIN_WEB_IMAGE_REF=%s\n' "$admin_web_image_ref"
printf 'SITE_WEB_IMAGE_REF=%s\n' "$site_web_image_ref"
