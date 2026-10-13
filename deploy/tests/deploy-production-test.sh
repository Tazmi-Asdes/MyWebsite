#!/usr/bin/env bash
# Exercise deployment orchestration with local fakes; never contact a server.
set -Eeuo pipefail

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
temp_root=$(mktemp -d)
trap 'rm -rf -- "$temp_root"' EXIT
mkdir -p "$temp_root/bin"
export MOCK_REAL_MV
MOCK_REAL_MV=$(command -v mv)

cat >"$temp_root/common.sh" <<'COMMON'
set -eu
registry=registry.example/group/project
digest_for() {
    local commit=$1 service=$2 suffix=1
    [[ $service == admin-web ]] && suffix=2
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
            for service in app admin-web; do
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
                add_digest "$APP_IMAGE_REF"
                add_digest "$ADMIN_WEB_IMAGE_REF" ;;
            up)
                printf '%s|%s\n' "$APP_IMAGE_REF" "$ADMIN_WEB_IMAGE_REF" >>"$MOCK_FIXTURE/up.log"
                printf '%s\n' "$APP_IMAGE_REF" >"$MOCK_FIXTURE/running-app"
                if [[ $APP_IMAGE_REF == "${MOCK_UP_FAIL_REF:-unmatched}" && ! -e $MOCK_FIXTURE/up-failed ]]; then
                    touch "$MOCK_FIXTURE/up-failed"
                    exit 50
                fi
                printf '%s\n' "$ADMIN_WEB_IMAGE_REF" >"$MOCK_FIXTURE/running-admin-web" ;;
            *) exit 51 ;;
        esac ;;
    *) exit 52 ;;
esac
DOCKER

cat >"$temp_root/bin/curl" <<'CURL'
#!/usr/bin/env bash
set -eu
body= output=false
url=${*: -1}
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) body=$2; shift 2 ;;
        -w) output=true; shift 2 ;;
        --connect-timeout|--max-time) shift 2 ;;
        *) shift ;;
    esac
done
ref=$(cat "$MOCK_FIXTURE/running-app")
if [[ $url == http://* && $ref == "${MOCK_HEALTH_FAIL_REF:-unmatched}" ]]; then exit 22; fi
if [[ $url == https://* && $ref == "${MOCK_SMOKE_FAIL_REF:-unmatched}" ]]; then exit 22; fi
if [[ $output == true ]]; then
    case "$url" in
        */assets/styles.css) printf '%s\n' '--bg: black;' >"$body"; printf '200\ttext/css' ;;
        *)
            cat >"$body" <<'HTML'
<a href="/" aria-current="page">首页</a>
<a href="/articles" aria-current="page">文章</a>
<a href="/projects" aria-current="page">项目</a>
<a href="/about" aria-current="page">关于</a>
<div id="app"></div>
HTML
            printf '200\ttext/html; charset=utf-8' ;;
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
    for path in compose.yaml deploy/compose.onepanel.example.yaml deploy/compose.redis.onepanel.yaml deploy/compose.images.yaml; do
        printf 'fixture\n' >"$CI_PROJECT_DIR/$path"
    done
    cp "$project_root/deploy/scripts/cleanup-images.sh" "$CI_PROJECT_DIR/deploy/scripts/cleanup-images.sh"
    printf 'REDIS_ADDR=Redis:6379\nREDIS_PASSWORD_FILE=/run/secrets/redis_password\n' >"$DEPLOY_ROOT/deploy/env.production"
    printf 'test-db-password\n' >"$DEPLOY_ROOT/secrets/db_password"
    printf 'test-redis-password\n' >"$DEPLOY_ROOT/secrets/redis_password"
    : >"$MOCK_FIXTURE/images"
    : >"$MOCK_FIXTURE/docker.log"
    : >"$MOCK_FIXTURE/up.log"
    : >"$MOCK_FIXTURE/removed"
    : >"$MOCK_FIXTURE/running-app"
    : >"$MOCK_FIXTURE/running-admin-web"
    export CI_DEFAULT_BRANCH=main CI_COMMIT_BRANCH=main CI_COMMIT_REF_PROTECTED=true CI_PIPELINE_SOURCE=push
    export CI_REGISTRY_IMAGE=$registry CI_PIPELINE_ID=$case_number
    export COMPOSE_PROJECT_NAME=mywebsite MYSQL_NETWORK_NAME=1panel-network APP_PORT=18080 ADMIN_WEB_PORT=18081 PRODUCTION_URL=https://site.example
    export ALLOW_DEPLOY_CONFIG_CHANGE=false CONFIG_CHANGE_CONFIRMATION=
    unset MOCK_REMOTE_HEAD MOCK_UP_FAIL_REF MOCK_HEALTH_FAIL_REF MOCK_SMOKE_FAIL_REF MOCK_PULL_FAIL_REF MOCK_CONFIG_FAIL_REF MOCK_FAIL_STATE_WRITE
}

tag_release() {
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
    export APP_IMAGE_REF ADMIN_WEB_IMAGE_REF
    APP_IMAGE_REF=$(ref_for "$1" app)
    ADMIN_WEB_IMAGE_REF=$(ref_for "$1" admin-web)
    tag_release "$1"
}

run_release() {
    local expected=$1 mode=${2:-deploy} status=0
    bash "$project_root/deploy/scripts/deploy-production.sh" "$mode" >"$MOCK_FIXTURE/result.log" 2>&1 || status=$?
    if [[ $expected == success && $status -ne 0 || $expected == failure && $status -eq 0 ]]; then
        cat "$MOCK_FIXTURE/result.log" >&2
        fail "expected $expected for $mode, got status=$status"
    fi
    [[ ! -e $CI_PROJECT_DIR/deploy/env.production ]] || fail 'copied production env was left in workspace'
    if grep -Rq 'test-db-password\|test-redis-password' "$CI_PROJECT_DIR/test-results"; then fail 'diagnostics contain secret fixture values'; fi
}

assert_record() { grep -Fxq "release_commit=$2" "$DEPLOY_STATE_DIR/$1" || fail "$1 record must be $2"; }
assert_pair() {
    [[ $(cat "$MOCK_FIXTURE/running-app") == "$(ref_for "$1" app)" ]] || fail 'app running image differs'
    [[ $(cat "$MOCK_FIXTURE/running-admin-web") == "$(ref_for "$1" admin-web)" ]] || fail 'admin running image differs'
}
snapshot_records() { cp "$DEPLOY_STATE_DIR/current" "$MOCK_FIXTURE/current.before"; }
assert_state_unchanged() { cmp "$MOCK_FIXTURE/current.before" "$DEPLOY_STATE_DIR/current" || fail 'failure advanced successful state'; }

new_fixture
set_target "$a"
run_release success
assert_record current "$a"
[[ ! -e $DEPLOY_STATE_DIR/previous ]] || fail 'first deployment invented a previous release'
assert_pair "$a"
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
[[ -n $(image_row "$registry/app:$b") && -n $(image_row "$registry/app:$c") ]] || fail 'cleanup removed a retained release'
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
    tag_release "$a"
    for service in app admin-web; do
        if [[ $initial_ref_type == tag ]]; then ref="$registry/$service:$a"; else ref=$(ref_for "$a" "$service"); fi
        printf '%s\n' "$ref" >"$MOCK_FIXTURE/running-$service"
    done
    run_release success
    assert_record current "$b"
    assert_record previous "$a"
    assert_pair "$b"
done
pass 'existing healthy SHA-tag and digest deployments can initialize release state'

for failure in pull config up health smoke state; do
    new_fixture
    set_target "$a"
    run_release success
    snapshot_records
    set_target "$b"
    ref=$(ref_for "$b" app)
    case "$failure" in
        pull) export MOCK_PULL_FAIL_REF=$ref ;;
        config) export MOCK_CONFIG_FAIL_REF=$ref ;;
        up) export MOCK_UP_FAIL_REF=$ref ;;
        health) export MOCK_HEALTH_FAIL_REF=$ref ;;
        smoke) export MOCK_SMOKE_FAIL_REF=$ref ;;
        state) export MOCK_FAIL_STATE_WRITE=true ;;
    esac
    : >"$MOCK_FIXTURE/up.log"
    run_release failure
    assert_state_unchanged
    assert_pair "$a"
    [[ ! -e $DEPLOY_STATE_DIR/previous ]] || fail "$failure failure changed previous record"
    case "$failure" in
        pull|config) [[ ! -s $MOCK_FIXTURE/up.log ]] || fail 'pre-update failure changed services' ;;
        *) [[ $(wc -l <"$MOCK_FIXTURE/up.log") -eq 2 ]] || fail 'failed update did not reapply the successful pair' ;;
    esac
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

printf 'deployment and rollback mock checks passed\n'
