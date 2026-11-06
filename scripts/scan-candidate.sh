#!/bin/bash
# Acquisition has network; report generation sees only the acquired DB and candidate data.
set -Eeuo pipefail
if [ "$#" -ne 2 ]; then echo 'usage: scan-candidate.sh CANDIDATE_DIRECTORY NEW_OUTPUT_DIRECTORY' >&2; exit 2; fi
root=$(cd "$(dirname "$0")/.." && pwd -P)
candidate=$(realpath "$1")
out=$(realpath -m "$2")
test ! -e "$out" && test ! -L "$out"
case "$root$candidate$out" in *','*|*$'\n'*) exit 2;; esac
mkdir -m 700 "$out"
prefix=scanner-security-$(python3 -c 'import uuid;print(uuid.uuid4().hex)')
volume=$prefix-db
image=$prefix:tools
cleanup() {
    status=$?
    trap - EXIT
    set +e
    if [ ! -d "$out/reports" ]; then timeout 90 docker cp "$prefix-keeper:/work/reports" "$out/reports" >/dev/null 2>&1; fi
    timeout 60 docker rm --force "$prefix-acquire" "$prefix-scan" "$prefix-keeper" >/dev/null 2>&1
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
timeout 30 docker run --detach --name "$prefix-keeper" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 32m --memory-swap 32m --cpus 0.25 --pids-limit 8 --mount "type=volume,src=$volume,dst=/work,readonly" --entrypoint /bin/sleep "$image" 5400 > "$out/keeper.log"
for phase in acquire scan; do
    network=none; duration=3200
    if [ "$phase" = acquire ]; then network=bridge; duration=900; fi
    timeout "$duration" docker run --rm --name "$prefix-$phase" --network "$network" --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 6g --memory-swap 6g --cpus 2 --pids-limit 64 --ulimit nofile=256:256 --ulimit fsize=4294967296:4294967296 --tmpfs /tmp:rw,nosuid,nodev,size=256m,mode=1777 \
        --mount "type=volume,src=$volume,dst=/work" --mount "type=bind,src=$root,dst=/source,readonly" \
        --mount "type=bind,src=$candidate,dst=/candidate,readonly" --workdir /work --entrypoint /usr/bin/env "$image" \
        -i PATH=/usr/local/bin:/usr/bin:/bin PYTHONDONTWRITEBYTECODE=1 python3 /source/scripts/scan-candidate.py "$phase" > "$out/$phase.log" 2>&1
done
timeout 90 docker cp "$prefix-keeper:/work/reports" "$out/reports"
