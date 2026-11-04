#!/bin/bash
set -Eeuo pipefail
if [ "$#" -ne 2 ]; then echo 'usage: prepare-candidate.sh EXACT_RUN_INPUTS NEW_OUTPUT_DIRECTORY' >&2; exit 2; fi
root=$(cd "$(dirname "$0")/.." && pwd -P)
inputs=$(realpath "$1")
out=$(realpath -m "$2")
test ! -e "$out" && test ! -L "$out"
case "$root$inputs$out" in *','*|*$'\n'*) exit 2;; esac
mkdir -m 700 "$out"
prefix=scanner-materials-$(python3 -c 'import uuid;print(uuid.uuid4().hex)')
image=$prefix:tools
volume=$prefix-work
cleanup() {
    status=$?
    trap - EXIT
    set +e
    timeout 60 docker rm --force "$prefix-build" "$prefix-keeper" >/dev/null 2>&1
    timeout 60 docker volume rm "$volume" >> "$out/cleanup.log" 2>&1
    clean_status=$?
    timeout 60 docker image rm --force "$image" >> "$out/cleanup.log" 2>&1
    image_status=$?
    if [ "$status" -eq 0 ] && { [ "$clean_status" -ne 0 ] || [ "$image_status" -ne 0 ]; }; then status=1; fi
    exit "$status"
}
trap cleanup EXIT
timeout 300 docker build --platform linux/amd64 --network none --file "$root/native/Dockerfile.apk-fetch" --tag "$image" "$root/native" > "$out/bootstrap.log" 2>&1
timeout 30 docker volume create --driver local --opt type=tmpfs --opt device=tmpfs --opt o=size=4g,uid=10001,gid=10001,mode=0700 "$volume" > "$out/volume.log"
timeout 30 docker run --detach --name "$prefix-keeper" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 32m --memory-swap 32m --cpus 0.25 --pids-limit 8 --mount "type=volume,src=$volume,dst=/work,readonly" --entrypoint /bin/sleep "$image" 1200 > "$out/keeper.log"
timeout 600 docker run --rm --name "$prefix-build" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 6g --memory-swap 6g --cpus 2 --pids-limit 64 --ulimit nofile=256:256 --ulimit fsize=1073741824:1073741824 \
    --mount "type=volume,src=$volume,dst=/work" --mount "type=bind,src=$root,dst=/source,readonly" \
    --mount "type=bind,src=$inputs,dst=/inputs,readonly" --workdir /work --entrypoint /usr/bin/env "$image" \
    -i PATH=/usr/local/bin:/usr/bin:/bin PYTHONDONTWRITEBYTECODE=1 python3 /source/scripts/prepare-candidate.py --inputs /inputs --output /work/bundle > "$out/prepare.log" 2>&1
timeout 180 docker cp "$prefix-keeper:/work/bundle/candidate" "$out/candidate"
