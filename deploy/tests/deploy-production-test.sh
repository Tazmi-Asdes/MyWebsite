#!/usr/bin/env bash
# Exercise deployment orchestration with local fakes; never contact a server.
set -Eeuo pipefail

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
temp_root=$(mktemp -d)
cleanup_test_temp() {
    local status=$?
    if [[ $status -ne 0 && ${KEEP_DEPLOY_TEST_ARTIFACTS:-false} == true ]]; then
        printf 'deployment test artifacts preserved at %s\n' "$temp_root" >&2
    else
        rm -rf -- "$temp_root"
    fi
}
trap cleanup_test_temp EXIT
mkdir -p "$temp_root/bin"
export MOCK_REAL_MV
MOCK_REAL_MV=$(command -v mv)

cat >"$temp_root/common.sh" <<'COMMON'
set -eu
registry=registry.example/group/project
digest_for() {
    local commit=$1 service=$2 suffix=1
    [[ $service == admin-web ]] && suffix=2
    [[ $service == site-web ]] && suffix=3
    printf '%s%s%s' "$commit" "${commit:0:23}" "$suffix"
}
ref_for() { printf '%s/%s@sha256:%s' "$registry" "$2" "$(digest_for "$1" "$2")"; }
image_row() { awk -F '|' -v ref="$1" '$1 == ref { print; exit }' "$MOCK_FIXTURE/images"; }
add_image() {
    local ref=$1 id=$2 digest=$3
    if [[ -z $(image_row "$ref") ]]; then printf '%s|%s|%s\n' "$ref" "$id" "$digest" >>"$MOCK_FIXTURE/images"; fi
}
add_digest() {
    local ref=$1
    add_image "$ref" "sha256:${ref##*@sha256:}" "$ref"
}
COMMON
export MOCK_COMMON="$temp_root/common.sh"

cat >"$temp_root/bin/git" <<'GIT'
#!/usr/bin/env bash
set -eu
while [[ ${1:-} == -C || ${1:-} == -c ]]; do shift 2; done
case "$1" in
    ls-remote) printf '%s\trefs/heads/main\n' "${MOCK_REMOTE_HEAD:-$MOCK_HEAD}" ;;
    rev-parse)
        object=${*: -1}
        case "$object" in
            'HEAD^{commit}') printf '%s\n' "$MOCK_HEAD" ;;
            *'^{commit}') printf '%s\n' "${object%\^\{commit\}}" ;;
            *:db/migrations)
                if [[ $object == 4444444444444444444444444444444444444444:* ]]; then
                    printf '%040d\n' 9
                else printf '%040d\n' 5; fi ;;
            *:compose.yaml) printf '%040d\n' 6 ;;
            *:deploy/compose.onepanel.example.yaml) printf '%040d\n' 7 ;;
            *:deploy/compose.redis.onepanel.yaml) printf '%040d\n' 8 ;;
            *:deploy/compose.site.yaml) printf '%040d\n' 10 ;;
            *) exit 40 ;;
        esac ;;
    *) exit 41 ;;
esac
GIT

cat >"$temp_root/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
source "$MOCK_COMMON"
printf 'docker %s\n' "$*" >>"$MOCK_FIXTURE/docker.log"
case "$1" in
    network) [[ $2 == inspect ]] ;;
    ps)
        service=
        for arg in "$@"; do
            case "$arg" in label=com.docker.compose.service=*) service=${arg##*=} ;; esac
        done
        if [[ -n $service ]]; then
            [[ ! -s $MOCK_FIXTURE/running-$service ]] || printf '%s\n' "$service-container"
        else
            for service in app admin-web site-web; do
                [[ ! -s $MOCK_FIXTURE/running-$service ]] || printf '%s\n' "$service-container"
            done
        fi ;;
    inspect)
        format=$3
        service=${4%-container}
        ref=$(cat "$MOCK_FIXTURE/running-$service")
        row=$(image_row "$ref")
        [[ -n $row ]] || exit 42
        id=${row#*|}; id=${id%%|*}
        case "$format" in
            '{{.Image}}') printf '%s\n' "$id" ;;
            '{{.Config.Image}}') printf '%s\n' "$ref" ;;
            '{{.Image}}|{{.Config.Image}}') printf '%s|%s\n' "$id" "$ref" ;;
            *) exit 43 ;;
        esac ;;
    image)
        case "$2" in
            inspect)
                ref=${*: -1}
                row=$(image_row "$ref")
                if [[ -z $row && $ref == sha256:* ]]; then
                    row=$(awk -F '|' -v id="$ref" '$2 == id { print; exit }' "$MOCK_FIXTURE/images")
                fi
                [[ -n $row ]] || exit 44
                rest=${row#*|}; id=${rest%%|*}; digest=${rest#*|}
                case "$4" in
                    '{{.Id}}') printf '%s\n' "$id" ;;
                    '{{range .RepoDigests}}{{println .}}{{end}}') printf '%s\n' "$digest" ;;
                    *) exit 45 ;;
                esac ;;
            ls)
                format=
                repo=
                full=false
                shift 2
                while [[ $# -gt 0 ]]; do
                    case "$1" in
                        --no-trunc) full=true; shift ;;
                        --format) format=$2; shift 2 ;;
                        *) repo=$1; shift ;;
                    esac
                done
                while IFS='|' read -r ref id digest; do
                    [[ $ref != *@* ]] || continue
                    repository=${ref%:*}; tag=${ref##*:}
                    [[ -z $repo || $repo == "$repository" ]] || continue
                    if [[ $format == *'{{.ID}}'* ]]; then
                        [[ $full == true ]] || id=${id:7:12}
                        printf '%s %s %s\n' "$repository" "$tag" "$id"
                    else printf '%s %s\n' "$repository" "$tag"; fi
                done <"$MOCK_FIXTURE/images" ;;
            rm)
                [[ $3 == --no-prune ]] || exit 46
                printf '%s\n' "$4" >>"$MOCK_FIXTURE/removed"
                awk -F '|' -v ref="$4" '$1 != ref' "$MOCK_FIXTURE/images" >"$MOCK_FIXTURE/images.next"
                mv "$MOCK_FIXTURE/images.next" "$MOCK_FIXTURE/images" ;;
            *) exit 47 ;;
        esac ;;
    compose)
        shift
        while [[ $# -gt 0 && $1 == -* ]]; do
            case "$1" in --project-directory|-f) shift 2 ;; *) exit 48 ;; esac
        done
        case "$1" in
            version) printf 'Docker Compose mock\n' ;;
            config) [[ ${APP_IMAGE_REF:-} != "${MOCK_CONFIG_FAIL_REF:-unmatched}" ]] ;;
            pull)
                [[ $APP_IMAGE_REF != "${MOCK_PULL_FAIL_REF:-unmatched}" ]] || exit 49
                [[ $ADMIN_WEB_IMAGE_REF != "${MOCK_PULL_FAIL_REF:-unmatched}" ]] || exit 49
                [[ $SITE_WEB_IMAGE_REF != "${MOCK_PULL_FAIL_REF:-unmatched}" ]] || exit 49
                add_digest "$APP_IMAGE_REF"
                add_digest "$ADMIN_WEB_IMAGE_REF"
                add_digest "$SITE_WEB_IMAGE_REF" ;;
            up)
                printf '%s|%s|%s\n' "$APP_IMAGE_REF" "$ADMIN_WEB_IMAGE_REF" "$SITE_WEB_IMAGE_REF" >>"$MOCK_FIXTURE/up.log"
                printf '%s\n' "$APP_IMAGE_REF" >"$MOCK_FIXTURE/running-app"
                if [[ $APP_IMAGE_REF == "${MOCK_UP_FAIL_REF:-unmatched}" && ! -e $MOCK_FIXTURE/up-failed ]]; then
                    touch "$MOCK_FIXTURE/up-failed"
                    exit 50
                fi
                printf '%s\n' "$ADMIN_WEB_IMAGE_REF" >"$MOCK_FIXTURE/running-admin-web"
                if [[ $ADMIN_WEB_IMAGE_REF == "${MOCK_UP_FAIL_REF:-unmatched}" && ! -e $MOCK_FIXTURE/up-failed ]]; then
                    touch "$MOCK_FIXTURE/up-failed"
                    exit 50
                fi
                printf '%s\n' "$SITE_WEB_IMAGE_REF" >"$MOCK_FIXTURE/running-site-web"
                if [[ $SITE_WEB_IMAGE_REF == "${MOCK_UP_FAIL_REF:-unmatched}" && ! -e $MOCK_FIXTURE/up-failed ]]; then
                    touch "$MOCK_FIXTURE/up-failed"
                    exit 50
                fi ;;
            *) exit 51 ;;
        esac ;;
    *) exit 52 ;;
esac
DOCKER

cat >"$temp_root/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -eu
body= output=false
connect_timeout= max_time= show_error=false
url=${*: -1}
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) body=$2; shift 2 ;;
        -w) output=true; shift 2 ;;
        --connect-timeout) connect_timeout=$2; shift 2 ;;
        --max-time) max_time=$2; shift 2 ;;
        --show-error) show_error=true; shift ;;
        *) shift ;;
    esac
done
printf 'url=%s connect=%s max=%s show_error=%s\n' "$url" "$connect_timeout" "$max_time" "$show_error" >>"$MOCK_FIXTURE/curl.log"
if [[ $url == http://127.0.0.1:18082/* ]]; then
    ref=$(cat "$MOCK_FIXTURE/running-site-web")
    if [[ $ref == "${MOCK_SITE_HEALTH_FAIL_REF:-unmatched}" ]]; then exit 22; fi
else
    ref=$(cat "$MOCK_FIXTURE/running-app")
    if [[ $url == http://127.0.0.1:18080/* && $ref == "${MOCK_HEALTH_FAIL_REF:-unmatched}" ]]; then exit 22; fi
fi
if [[ $url == https://* && ( $ref == "${MOCK_SMOKE_FAIL_REF:-unmatched}" || ${MOCK_SMOKE_FAIL_ALL:-false} == true ) && ( -z ${MOCK_SMOKE_FAIL_PATH:-} || $url == *"$MOCK_SMOKE_FAIL_PATH" ) ]]; then
    fail_exit=${MOCK_SMOKE_FAIL_EXIT:-22}
    fail_status=${MOCK_SMOKE_FAIL_STATUS:-503}
    printf '%s\n' 'MOCK_PRIVATE_RESPONSE_SECRET' >"$body"
    if [[ $show_error == true ]]; then printf 'curl: (%s) simulated public request failure\n' "$fail_exit" >&2; fi
    printf '%s|text/html; charset=utf-8|0.123456' "$fail_status"
    exit "$fail_exit"
fi
if [[ $output == true ]]; then
    case "$url" in
        https://*/_astro/*.css*) printf '%s\n' '--background-blur: 20px;' >"$body"; printf '200|text/css|0.025000' ;;
        */assets/styles.css) printf '%s\n' '--bg: black;' >"$body"; printf '200|text/css|0.025000' ;;
        *)
            if [[ ${MOCK_PUBLIC_SITE_MODE:-go} == astro ]]; then
                cat >"$body" <<'HTML'
<meta name="public-site-renderer" content="astro">
<link rel="stylesheet" href="/_astro/site.123abc.css">
<a href="/" aria-current="page">首页</a>
<a href="/articles" aria-current="page">文章</a>
<a href="/projects" aria-current="page">项目</a>
<a href="/about" aria-current="page">关于</a>
<div id="app"></div>
HTML
            else
                cat >"$body" <<'HTML'
<a href="/" aria-current="page">首页</a>
<a href="/articles" aria-current="page">文章</a>
<a href="/projects" aria-current="page">项目</a>
<a href="/about" aria-current="page">关于</a>
<div id="app"></div>
HTML
            fi
            printf '200|text/html; charset=utf-8|0.025000' ;;
    esac
fi
CURL

cat >"$temp_root/bin/mv" <<'MV'
#!/usr/bin/env bash
set -eu
destination=${*: -1}
if [[ ${MOCK_FAIL_STATE_WRITE:-false} == true && $destination == "$DEPLOY_STATE_DIR/current" && ! -e $MOCK_FIXTURE/state-write-failed ]]; then
    touch "$MOCK_FIXTURE/state-write-failed"
    exit 53
fi
exec "$MOCK_REAL_MV" "$@"
MV
printf '#!/usr/bin/env bash\nexit 0\n' >"$temp_root/bin/sleep"
chmod +x "$temp_root/bin/"*
export PATH="$temp_root/bin:$PATH"
source "$MOCK_COMMON"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*"; }
a=1111111111111111111111111111111111111111
b=2222222222222222222222222222222222222222
c=3333333333333333333333333333333333333333
d=4444444444444444444444444444444444444444
case_number=0

new_fixture() {
    case_number=$((case_number + 1))
    export MOCK_FIXTURE="$temp_root/case-$case_number"
    export CI_PROJECT_DIR="$MOCK_FIXTURE/workspace"
    export DEPLOY_ROOT="$MOCK_FIXTURE/server"
    export DEPLOY_STATE_DIR="$DEPLOY_ROOT/deploy-state"
    mkdir -p "$CI_PROJECT_DIR/deploy/scripts" "$DEPLOY_ROOT/deploy" "$DEPLOY_ROOT/secrets" "$DEPLOY_ROOT/data/uploads" "$DEPLOY_STATE_DIR"
    for path in compose.yaml deploy/compose.onepanel.example.yaml deploy/compose.redis.onepanel.yaml deploy/compose.images.yaml deploy/compose.site.yaml; do
        printf 'fixture\n' >"$CI_PROJECT_DIR/$path"
    done
    cp "$project_root/deploy/scripts/cleanup-images.sh" "$CI_PROJECT_DIR/deploy/scripts/cleanup-images.sh"
    printf 'REDIS_ADDR=Redis:6379\nREDIS_PASSWORD_FILE=/run/secrets/redis_password\n' >"$DEPLOY_ROOT/deploy/env.production"
    printf 'test-db-password\n' >"$DEPLOY_ROOT/secrets/db_password"
    printf 'test-redis-password\n' >"$DEPLOY_ROOT/secrets/redis_password"
    : >"$MOCK_FIXTURE/images"
    : >"$MOCK_FIXTURE/docker.log"
    : >"$MOCK_FIXTURE/curl.log"
    : >"$MOCK_FIXTURE/up.log"
    : >"$MOCK_FIXTURE/removed"
    : >"$MOCK_FIXTURE/running-app"
    : >"$MOCK_FIXTURE/running-admin-web"
    : >"$MOCK_FIXTURE/running-site-web"
    export CI_DEFAULT_BRANCH=main CI_COMMIT_BRANCH=main CI_COMMIT_REF_PROTECTED=true CI_PIPELINE_SOURCE=push
    export CI_REGISTRY_IMAGE=$registry CI_PIPELINE_ID=$case_number
    export COMPOSE_PROJECT_NAME=mywebsite MYSQL_NETWORK_NAME=1panel-network APP_PORT=18080 ADMIN_WEB_PORT=18081 SITE_WEB_PORT=18082 PUBLIC_SITE_MODE=go PRODUCTION_URL=https://site.example
    export MOCK_PUBLIC_SITE_MODE=go
    export ALLOW_DEPLOY_CONFIG_CHANGE=false CONFIG_CHANGE_CONFIRMATION=
    unset MOCK_REMOTE_HEAD MOCK_UP_FAIL_REF MOCK_HEALTH_FAIL_REF MOCK_SITE_HEALTH_FAIL_REF MOCK_SMOKE_FAIL_REF MOCK_SMOKE_FAIL_ALL MOCK_SMOKE_FAIL_PATH MOCK_SMOKE_FAIL_EXIT MOCK_SMOKE_FAIL_STATUS MOCK_PULL_FAIL_REF MOCK_CONFIG_FAIL_REF MOCK_FAIL_STATE_WRITE
}

tag_release() {
    local commit=$1 service ref digest
    for service in app admin-web site-web; do
        ref=$(ref_for "$commit" "$service")
        digest=$(digest_for "$commit" "$service")
        add_image "$registry/$service:$commit" "sha256:$digest" "$ref"
        add_digest "$ref"
    done
}

tag_legacy_release() {
    local commit=$1 service ref digest
    for service in app admin-web; do
        ref=$(ref_for "$commit" "$service")
        digest=$(digest_for "$commit" "$service")
        add_image "$registry/$service:$commit" "sha256:$digest" "$ref"
        add_digest "$ref"
    done
}

set_target() {
    export MOCK_HEAD=$1 CI_COMMIT_SHA=$1 RELEASE_COMMIT=$1
    export APP_IMAGE_REF ADMIN_WEB_IMAGE_REF SITE_WEB_IMAGE_REF
    APP_IMAGE_REF=$(ref_for "$1" app)
    ADMIN_WEB_IMAGE_REF=$(ref_for "$1" admin-web)
    SITE_WEB_IMAGE_REF=$(ref_for "$1" site-web)
    tag_release "$1"
}

run_release() {
    local expected=$1 mode=${2:-deploy} status=0
    bash "$project_root/deploy/scripts/deploy-production.sh" "$mode" >"$MOCK_FIXTURE/result.log" 2>&1 || status=$?
    if [[ $expected == success && $status -ne 0 || $expected == failure && $status -eq 0 ]]; then
        cat "$MOCK_FIXTURE/result.log" >&2
        local diagnostics="$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt"
        [[ ! -f $diagnostics ]] || cat "$diagnostics" >&2
        fail "expected $expected for $mode, got status=$status"
    fi
    [[ ! -e $CI_PROJECT_DIR/deploy/env.production ]] || fail 'copied production env was left in workspace'
    if grep -Rq 'test-db-password\|test-redis-password' "$CI_PROJECT_DIR/test-results"; then fail 'diagnostics contain secret fixture values'; fi
}

assert_record() { grep -Fxq "release_commit=$2" "$DEPLOY_STATE_DIR/$1" || fail "$1 record must be $2"; }
assert_pair() {
    [[ $(cat "$MOCK_FIXTURE/running-app") == "$(ref_for "$1" app)" ]] || fail 'app running image differs'
    [[ $(cat "$MOCK_FIXTURE/running-admin-web") == "$(ref_for "$1" admin-web)" ]] || fail 'admin running image differs'
    [[ $(cat "$MOCK_FIXTURE/running-site-web") == "$(ref_for "$1" site-web)" ]] || fail 'site-web running image differs'
}
snapshot_records() { cp "$DEPLOY_STATE_DIR/current" "$MOCK_FIXTURE/current.before"; }
assert_state_unchanged() { cmp "$MOCK_FIXTURE/current.before" "$DEPLOY_STATE_DIR/current" || fail 'failure advanced successful state'; }

new_fixture
set_target "$a"
unset PUBLIC_SITE_MODE
export MOCK_PUBLIC_SITE_MODE=astro
run_release success
grep -Fxq 'public_site_mode=astro' "$DEPLOY_STATE_DIR/current" || fail 'standalone default did not select Astro mode'
pass 'standalone deployment defaults to Astro when no mode is supplied'

new_fixture
set_target "$a"
run_release success
assert_record current "$a"
[[ ! -e $DEPLOY_STATE_DIR/previous ]] || fail 'first deployment invented a previous release'
grep -Fxq 'format=2' "$DEPLOY_STATE_DIR/current" || fail 'new deployments must write format=2 state'
grep -Fq 'site_web_digest=' "$DEPLOY_STATE_DIR/current" || fail 'format=2 state omitted site-web digest'
assert_pair "$a"
grep -Fq 'url=https://site.example/ connect=5 max=15 show_error=true' "$MOCK_FIXTURE/curl.log" || fail 'public smoke did not use the configured timeout and show-error options'
grep -Fq 'url=http://127.0.0.1:18080/-/live connect=1 max=2 show_error=false' "$MOCK_FIXTURE/curl.log" || fail 'local health probe timeout changed unexpectedly'
grep -Fq 'url=http://127.0.0.1:18082/-/ready connect=1 max=2 show_error=false' "$MOCK_FIXTURE/curl.log" || fail 'site-web readiness probe did not use the configured loopback port'
pass 'first deployment writes a valid current record'

set_target "$b"
run_release success
assert_record current "$b"
assert_record previous "$a"
run_release success
assert_record previous "$a"
pass 'successful deployment rotates records; retry preserves the rollback target'

set_target "$c"
run_release success
assert_record current "$c"
assert_record previous "$b"
assert_pair "$c"
grep -Fxq "$registry/app:$a" "$MOCK_FIXTURE/removed" || fail 'cleanup did not remove old app tag'
grep -Fxq "$registry/admin-web:$a" "$MOCK_FIXTURE/removed" || fail 'cleanup did not remove old admin tag'
grep -Fxq "$registry/site-web:$a" "$MOCK_FIXTURE/removed" || fail 'cleanup did not remove old site-web tag'
[[ -n $(image_row "$registry/app:$b") && -n $(image_row "$registry/app:$c") && -n $(image_row "$registry/site-web:$b") && -n $(image_row "$registry/site-web:$c") ]] || fail 'cleanup removed a retained release'
pass 'third deployment preserves both retained versions and cleans older project tags'

export CI_PIPELINE_SOURCE=web
run_release success rollback
assert_pair "$b"
assert_record current "$b"
assert_record previous "$c"
snapshot_records
export MOCK_UP_FAIL_REF
MOCK_UP_FAIL_REF=$(ref_for "$c" app)
run_release failure rollback
assert_pair "$b"
assert_state_unchanged
assert_record previous "$c"
pass 'manual rollback swaps records; failed rollback restores current images'

for initial_ref_type in tag digest; do
    new_fixture
    set_target "$b"
    tag_legacy_release "$a"
    for service in app admin-web; do
        if [[ $initial_ref_type == tag ]]; then ref="$registry/$service:$a"; else ref=$(ref_for "$a" "$service"); fi
        printf '%s\n' "$ref" >"$MOCK_FIXTURE/running-$service"
    done
    export CI_PIPELINE_SOURCE=web ALLOW_DEPLOY_CONFIG_CHANGE=false CONFIG_CHANGE_CONFIRMATION=
    : >"$MOCK_FIXTURE/up.log"
    run_release failure
    grep -Fq 'config_change_blocked' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'legacy v1 to v2 migration was not blocked without confirmation'
    [[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'unconfirmed legacy migration updated services'
    grep -Fxq 'format=1' "$DEPLOY_STATE_DIR/current" || fail 'blocked migration did not preserve a v1 bootstrap baseline'
    [[ ! -e $DEPLOY_STATE_DIR/previous ]] || fail 'blocked legacy migration created a previous record'

    export ALLOW_DEPLOY_CONFIG_CHANGE=true CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
    : >"$MOCK_FIXTURE/up.log"
    run_release success
    assert_record current "$b"
    assert_record previous "$a"
    grep -Fxq 'format=1' "$DEPLOY_STATE_DIR/previous" || fail 'healthy legacy pair was not initialized with a v1 record'
    if grep -q '^site_web_digest=' "$DEPLOY_STATE_DIR/previous"; then fail 'legacy bootstrap fabricated a site-web digest'; fi
    grep -Fxq 'format=2' "$DEPLOY_STATE_DIR/current" || fail 'first three-image deployment did not write v2 state'
    assert_pair "$b"
    : >"$MOCK_FIXTURE/up.log"
    run_release failure rollback
    grep -Fq 'rollback_format_mismatch' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'rollback across v2 current and v1 previous was not blocked'
    [[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'rollback across v2/v1 records updated services'
done
pass 'legacy tag/digest pairs bootstrap as v1, block unconfirmed v2 migration and rollback, then confirmed migration retains v1 history'

for failure in pull sitepull config up siteup health sitehealth smoke state; do
    new_fixture
    set_target "$a"
    run_release success
    snapshot_records
    set_target "$b"
    ref=$(ref_for "$b" app)
    case "$failure" in
        pull) export MOCK_PULL_FAIL_REF=$ref ;;
        sitepull) export MOCK_PULL_FAIL_REF; MOCK_PULL_FAIL_REF=$(ref_for "$b" site-web) ;;
        config) export MOCK_CONFIG_FAIL_REF=$ref ;;
        up) export MOCK_UP_FAIL_REF=$ref ;;
        siteup) export MOCK_UP_FAIL_REF; MOCK_UP_FAIL_REF=$(ref_for "$b" site-web) ;;
        health) export MOCK_HEALTH_FAIL_REF=$ref ;;
        sitehealth) export MOCK_SITE_HEALTH_FAIL_REF; MOCK_SITE_HEALTH_FAIL_REF=$(ref_for "$b" site-web) ;;
        smoke) export MOCK_SMOKE_FAIL_REF=$ref ;;
        state) export MOCK_FAIL_STATE_WRITE=true ;;
    esac
    : >"$MOCK_FIXTURE/up.log"
    run_release failure
    assert_state_unchanged
    assert_pair "$a"
    [[ ! -e $DEPLOY_STATE_DIR/previous ]] || fail "$failure failure changed previous record"
    case "$failure" in
        pull|sitepull|config) [[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'pre-update failure changed services' ;;
        *) [[ $(wc -l <"$MOCK_FIXTURE/up.log") -eq 2 ]] || fail 'failed update did not reapply the successful pair' ;;
    esac
    if [[ "$failure" == smoke ]]; then
        grep -Fq 'smoke_failure path=/ category=http_status curl_exit=22 http_status=503' \
            "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'HTTP response failure was not classified as HTTP status'
    fi
    pass "$failure failure leaves successful state intact and preserves/restores the running pair"
done

new_fixture
set_target "$a"
run_release success
snapshot_records
set_target "$d"
: >"$MOCK_FIXTURE/up.log"
run_release failure
grep -Fq 'config_change_blocked' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'migration change was not blocked'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'migration change updated services without confirmation'
assert_state_unchanged
assert_pair "$a"
export CI_PIPELINE_SOURCE=web ALLOW_DEPLOY_CONFIG_CHANGE=true CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
export MOCK_UP_FAIL_REF=$APP_IMAGE_REF
run_release failure
grep -Fq 'recovery=disabled_for_config_change' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'config exception attempted incompatible automatic rollback'
assert_state_unchanged
pass 'migration/config change requires an explicit manual exception and disables image recovery'

new_fixture
set_target "$a"
run_release success
snapshot_records
set_target "$b"
export MOCK_REMOTE_HEAD=$c
: >"$MOCK_FIXTURE/up.log"
run_release failure
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'outdated pipeline updated services'
assert_state_unchanged
assert_pair "$a"
pass 'outdated default-branch pipeline is blocked before changing services'

# Digest pulls need not create a local SHA tag. Cleanup still keeps retained
# digests and unrelated repositories while removing only this project's old tags.
unset MOCK_REMOTE_HEAD
run_release success
awk -F '|' -v ref="$registry/app:$b" '$1 != ref' "$MOCK_FIXTURE/images" >"$MOCK_FIXTURE/images.next"
mv "$MOCK_FIXTURE/images.next" "$MOCK_FIXTURE/images"
tag_release "$c"
unrelated="registry.example/other/app:$c"
add_image "$unrelated" "sha256:$(digest_for "$c" app)" 'unrelated'
bash "$project_root/deploy/scripts/cleanup-images.sh" cleanup-state "$registry" "$DEPLOY_STATE_DIR" >"$MOCK_FIXTURE/cleanup.log" 2>&1
grep -Fxq "$registry/app:$c" "$MOCK_FIXTURE/removed" || fail 'digest-only retention prevented cleanup'
[[ -n $(image_row "$unrelated") ]] || fail 'cleanup removed an unrelated repository image'
assert_pair "$b"
pass 'cleanup handles digest-only retained images and leaves unrelated repositories intact'

new_fixture
set_target "$a"
run_release success
snapshot_records
set_target "$b"
export MOCK_SMOKE_FAIL_ALL=true MOCK_SMOKE_FAIL_PATH=/articles MOCK_SMOKE_FAIL_EXIT=28 MOCK_SMOKE_FAIL_STATUS=200
: >"$MOCK_FIXTURE/curl.log"
run_release failure
grep -Fq 'smoke_failure path=/articles category=curl_error curl_exit=28 http_status=200 content_type=text/html;_charset=utf-8 elapsed_seconds=0.123456' \
    "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'HTTP 200 followed by a curl timeout was not classified as a curl error'
grep -Fq 'public smoke failed path=/articles category=curl_error curl_exit=28 http_status=200' "$MOCK_FIXTURE/result.log" || fail 'curl timeout details were not reported in the job log'
grep -Fq 'release verification failed (production_smoke_failed)' "$MOCK_FIXTURE/result.log" || fail 'recovery failure was mislabeled as a health failure'
if grep -Rq 'MOCK_PRIVATE_RESPONSE_SECRET' "$CI_PROJECT_DIR/test-results/deploy" "$MOCK_FIXTURE/result.log"; then fail 'public smoke response body leaked into logs or diagnostics'; fi
[[ $(grep -Fc 'url=https://site.example/articles connect=5 max=15 show_error=true' "$MOCK_FIXTURE/curl.log") -eq 4 ]] || fail 'public smoke retry or recovery did not retain the configured request options'
pass 'public smoke timeout after HTTP 200 is recorded without response bodies and recovery names release verification failure'

# A changed site digest must advance current even when the commit SHA is the
# same; an identical three-image retry must keep the rollback record intact.
new_fixture
set_target "$a"
run_release success
cp "$DEPLOY_STATE_DIR/current" "$MOCK_FIXTURE/current.site-original"
alternate_site_digest=$(printf '%064d' 9)
alternate_site_ref="$registry/site-web@sha256:$alternate_site_digest"
add_digest "$alternate_site_ref"
SITE_WEB_IMAGE_REF=$alternate_site_ref
export SITE_WEB_IMAGE_REF
: >"$MOCK_FIXTURE/up.log"
run_release success
grep -Fxq "release_commit=$a" "$DEPLOY_STATE_DIR/current" || fail 'same-commit site digest update changed the release commit'
grep -Fxq "site_web_digest=$alternate_site_digest" "$DEPLOY_STATE_DIR/current" || fail 'same-commit site digest change did not advance current'
cmp "$MOCK_FIXTURE/current.site-original" "$DEPLOY_STATE_DIR/previous" || fail 'same-commit site digest update did not retain the old complete record'
cp "$DEPLOY_STATE_DIR/previous" "$MOCK_FIXTURE/previous.site-original"
: >"$MOCK_FIXTURE/up.log"
run_release success
cmp "$MOCK_FIXTURE/previous.site-original" "$DEPLOY_STATE_DIR/previous" || fail 'identical three-digest retry overwrote the previous record'
pass 'same-commit site digest changes advance current while identical three-image retries preserve previous'

# The first Astro release requires confirmation; two Astro releases can then
# roll back together, including the site-web image.
new_fixture
set_target "$a"
run_release success
snapshot_records
export CI_PIPELINE_SOURCE=web PUBLIC_SITE_MODE=astro MOCK_PUBLIC_SITE_MODE=astro
set_target "$b"
: >"$MOCK_FIXTURE/up.log"
run_release failure
grep -Fq 'config_change_blocked' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'go-to-astro mode change was not blocked without confirmation'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'unconfirmed go-to-astro mode change updated services'
assert_state_unchanged
assert_pair "$a"
export ALLOW_DEPLOY_CONFIG_CHANGE=true CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
run_release success
grep -Fxq 'public_site_mode=astro' "$DEPLOY_STATE_DIR/current" || fail 'confirmed Astro release did not record Astro mode'
grep -Fq 'url=https://site.example/_astro/site.123abc.css' "$MOCK_FIXTURE/curl.log" || fail 'Astro smoke did not request the CSS linked by the returned HTML'
set_target "$c"
: >"$MOCK_FIXTURE/curl.log"
run_release success
assert_record previous "$b"
grep -Fxq 'public_site_mode=astro' "$DEPLOY_STATE_DIR/previous" || fail 'second Astro release did not retain Astro previous state'
grep -Fq 'url=https://site.example/_astro/site.123abc.css' "$MOCK_FIXTURE/curl.log" || fail 'second Astro release did not request its linked CSS'
run_release success rollback
assert_pair "$b"
assert_record current "$b"
assert_record previous "$c"
grep -Fxq 'public_site_mode=astro' "$DEPLOY_STATE_DIR/current" || fail 'Astro rollback did not preserve mode'
pass 'Astro mode changes require confirmation, smoke follows the HTML CSS link, and same-mode rollback restores all three images'

# A v2 rollback across different public modes is rejected before any up call.
new_fixture
set_target "$a"
run_release success
set_target "$b"
export CI_PIPELINE_SOURCE=web PUBLIC_SITE_MODE=astro MOCK_PUBLIC_SITE_MODE=astro
export ALLOW_DEPLOY_CONFIG_CHANGE=true CONFIG_CHANGE_CONFIRMATION=I_HAVE_BACKUP_AND_CONFIRMED_COMPATIBILITY
run_release success
cp "$DEPLOY_STATE_DIR/current" "$MOCK_FIXTURE/current.before-mode-rollback"
cp "$DEPLOY_STATE_DIR/previous" "$MOCK_FIXTURE/previous.before-mode-rollback"
: >"$MOCK_FIXTURE/up.log"
run_release failure rollback
grep -Fq 'rollback_mode_mismatch' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'v2 rollback across go/astro modes was not blocked'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'cross-mode rollback updated services'
cmp "$MOCK_FIXTURE/current.before-mode-rollback" "$DEPLOY_STATE_DIR/current" || fail 'cross-mode rollback changed current state'
cmp "$MOCK_FIXTURE/previous.before-mode-rollback" "$DEPLOY_STATE_DIR/previous" || fail 'cross-mode rollback changed previous state'
pass 'v2 rollback across go and astro modes is rejected before services change'

# Public responses that contradict the selected mode fail verification in
# both directions and restore the previously recorded image trio.
new_fixture
set_target "$a"
run_release success
snapshot_records
set_target "$b"
export PUBLIC_SITE_MODE=go MOCK_PUBLIC_SITE_MODE=astro
: >"$MOCK_FIXTURE/up.log"
run_release failure
grep -Fq 'smoke_failure path=/ category=mode_marker' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'Astro HTML was accepted in go mode'
assert_state_unchanged
assert_pair "$a"
[[ $(wc -l <"$MOCK_FIXTURE/up.log") -eq 2 ]] || fail 'wrong Astro response in go mode did not restore the current image trio'

new_fixture
set_target "$a"
export PUBLIC_SITE_MODE=astro MOCK_PUBLIC_SITE_MODE=astro
run_release success
snapshot_records
set_target "$b"
export MOCK_PUBLIC_SITE_MODE=go
: >"$MOCK_FIXTURE/up.log"
run_release failure
grep -Fq 'smoke_failure path=/ category=marker' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'Go HTML was accepted in astro mode'
assert_state_unchanged
assert_pair "$a"
[[ $(wc -l <"$MOCK_FIXTURE/up.log") -eq 2 ]] || fail 'wrong Go response in Astro mode did not restore the current image trio'
pass 'public smoke rejects Go/Astro mode mismatches in both directions and restores the recorded trio'

# Malformed mixed-version and incomplete v2 records must stop deploy and
# cleanup before they can change services or delete images.
new_fixture
set_target "$a"
run_release success
sed 's/^format=2$/format=1/' "$DEPLOY_STATE_DIR/current" >"$MOCK_FIXTURE/current.mixed-schema"
mv "$MOCK_FIXTURE/current.mixed-schema" "$DEPLOY_STATE_DIR/current"
: >"$MOCK_FIXTURE/up.log"
: >"$MOCK_FIXTURE/removed"
run_release failure
grep -Fq 'invalid_current_record' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'mixed v1/v2 state was not rejected by deploy'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'deploy changed services with mixed v1/v2 state'
bash "$project_root/deploy/scripts/cleanup-images.sh" cleanup-state "$registry" "$DEPLOY_STATE_DIR" >"$MOCK_FIXTURE/cleanup-mixed.log" 2>&1
[[ ! -s $MOCK_FIXTURE/removed ]] || fail 'cleanup deleted images with mixed v1/v2 state'

new_fixture
set_target "$a"
run_release success
grep -v '^site_web_digest=' "$DEPLOY_STATE_DIR/current" >"$MOCK_FIXTURE/current.missing-site-digest"
mv "$MOCK_FIXTURE/current.missing-site-digest" "$DEPLOY_STATE_DIR/current"
: >"$MOCK_FIXTURE/up.log"
: >"$MOCK_FIXTURE/removed"
run_release failure
grep -Fq 'invalid_current_record' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'v2 state missing site_web_digest was not rejected by deploy'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'deploy changed services with incomplete v2 state'
bash "$project_root/deploy/scripts/cleanup-images.sh" cleanup-state "$registry" "$DEPLOY_STATE_DIR" >"$MOCK_FIXTURE/cleanup-missing.log" 2>&1
[[ ! -s $MOCK_FIXTURE/removed ]] || fail 'cleanup deleted images with incomplete v2 state'
pass 'deploy and cleanup reject mixed or incomplete release records without deleting images'

# An orphan site-web container prevents legacy bootstrap when no state exists.
new_fixture
set_target "$a"
for service in app admin-web site-web; do printf '%s\n' "$(ref_for "$a" "$service")" >"$MOCK_FIXTURE/running-$service"; done
: >"$MOCK_FIXTURE/up.log"
run_release failure
grep -Fq 'incomplete_site_state' "$CI_PROJECT_DIR/test-results/deploy/deploy-$CI_PIPELINE_ID.txt" || fail 'site-web without release state was not rejected'
[[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'orphan site-web state changed services'
[[ ! -e $DEPLOY_STATE_DIR/current ]] || fail 'orphan site-web container fabricated a current record'
pass 'site-web without a release state blocks legacy bootstrap'

printf 'deployment and rollback mock checks passed\n'
