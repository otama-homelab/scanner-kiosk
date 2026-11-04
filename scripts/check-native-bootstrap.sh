#!/bin/sh
# Fetch is networked; archive parsing uses a separate offline, read-only stage.
set -eu
if [ "$#" -ne 2 ]; then
    echo 'usage: check-native-bootstrap.sh REPOSITORY_DIRECTORY BOOTSTRAP_IMAGE' >&2
    exit 2
fi
repo_root=$(cd "$1" && pwd -P)
test -d "$repo_root/native"
image=$2
task_volume=$(docker volume create --driver local --opt type=tmpfs --opt device=tmpfs \
    --opt o=size=128m,uid=10001,gid=10001,mode=0700)
fetch_container="scanner-fetch-$task_volume"
prepare_container="scanner-prepare-$task_volume"
holder_container="scanner-inputs-$task_volume"
cleanup() {
    run_status=$?
    trap - EXIT
    set +e
    docker rm --force "$fetch_container" "$prepare_container" "$holder_container" >/dev/null 2>&1
    docker volume rm "$task_volume"
    cleanup_status=$?
    if [ "$run_status" -eq 0 ]; then run_status=$cleanup_status; fi
    exit "$run_status"
}
trap cleanup EXIT
# A tmpfs volume is unmounted when its last container exits; retain it across stages.
docker run --detach --name "$holder_container" --network none --read-only \
    --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
    --memory 32m --memory-swap 32m --cpus 0.25 --pids-limit 8 \
    --mount "type=volume,src=$task_volume,dst=/cache" \
    --entrypoint /bin/sleep "$image" 660 >/dev/null
run_stage() {
    stage_name=$1
    stage_network=$2
    cache_readonly=$3
    shift 3
    timeout 300 docker run --rm --name "$stage_name" --network "$stage_network" \
        --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges \
        --memory 512m --memory-swap 512m --cpus 2 --pids-limit 64 \
        --ulimit nofile=64:64 --ulimit fsize=67108864:67108864 \
        --tmpfs /work:rw,noexec,nosuid,size=256m,mode=1777 \
        --mount "type=bind,src=$repo_root,dst=/src,readonly" \
        --mount "type=volume,src=$task_volume,dst=/cache,readonly=$cache_readonly" \
        --entrypoint /usr/bin/env "$image" \
        -i PATH=/usr/local/bin:/usr/bin:/bin PYTHONDONTWRITEBYTECODE=1 TMPDIR=/work \
        python3 "$@"
}
run_stage "$fetch_container" bridge false /src/scripts/fetch-native-apks.py \
    --lock /src/native/runtime-apks.lock.json --keys /build-keys \
    --apk /usr/local/bin/apk --output /cache/apks
run_stage "$prepare_container" none true /src/scripts/prepare-native-apks.py \
    --lock /src/native/runtime-apks.lock.json --manifest /src/native/runtime-manifest.json \
    --provenance /src/native/runtime-provenance.json --archives /cache/apks \
    --keys /build-keys --apk /usr/local/bin/apk --output /work/selected
