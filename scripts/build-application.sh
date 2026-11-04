#!/bin/bash
# Build inputs are installed tools plus a verified native tar; no home cache/config is mounted.
set -Eeuo pipefail
if [ "$#" -ne 2 ] && [ "$#" -ne 4 ]; then
    echo 'usage: build-application.sh NATIVE_TAR NEW_OUTPUT_DIRECTORY [REF SHA]' >&2
    exit 2
fi
root=$(cd "$(dirname "$0")/.." && pwd -P)
test -f "$1" && test ! -L "$1"
native=$(realpath "$1")
out=$(realpath -m "$2")
test ! -e "$out" && test ! -L "$out"
ref=${3:-}; sha=${4:-}
cd "$root"
git diff --exit-code HEAD --
git ls-files --error-unmatch scripts/build-application.sh scripts/build-application.py >/dev/null
if [ -n "$ref" ]; then python3 scripts/check-release-ref.py --ref "$ref" --sha "$sha"; fi
revision=$(git rev-parse HEAD)
goroot=$(env -u GOROOT GOTOOLCHAIN=local GOENV=off go env GOROOT)
test "$(GOTOOLCHAIN=local "$goroot/bin/go" version)" = 'go version go1.27.3 linux/amd64'
node=$(node -p process.execPath)
case "$root$native$out$goroot$node" in *','*|*$'\n'*) echo 'Docker paths cannot contain comma or newline' >&2; exit 2;; esac
mkdir -m 700 "$out"
prefix=scanner-app-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
volume=$prefix-work
keeper=$prefix-keeper
acquirer=$prefix-acquire
builder=$prefix-build
smoke=$prefix-smoke
tools=$prefix:tools
image=$prefix:runtime
cleanup() {
    status=$?
    trap - EXIT
    set +e
    timeout 60 docker rm --force "$acquirer" "$builder" "$smoke" "$keeper" "$prefix-inventory" >/dev/null 2>&1
    timeout 60 docker volume rm "$volume" >> "$out/cleanup.log" 2>&1
    clean_status=$?
    timeout 60 docker image rm --force "$tools" >> "$out/cleanup.log" 2>&1
    image_status=$?
    if [ "$status" -eq 0 ] && { [ "$clean_status" -ne 0 ] || [ "$image_status" -ne 0 ]; }; then status=1; fi
    if [ "$status" -ne 0 ]; then timeout 60 docker image rm --force "$image" >> "$out/cleanup.log" 2>&1; fi
    python3 - "$out" "$revision" "$status" "$image" <<'PY'
import sys,json,pathlib
out=pathlib.Path(sys.argv[1]);result={'passed':sys.argv[3]=='0','source_revision':sys.argv[2],
 'image_tag':sys.argv[4] if sys.argv[3]=='0' else None,'publication_approved':False}
for name in ['application','image','image-version','image-container','image-binary']:
 p=out/(name+'.json')
 if p.is_file():
  try:result[name]=json.loads(p.read_text())
  except (ValueError,OSError):result['passed']=False;result.setdefault('unreadable_metadata',[]).append(name)
(out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
raise SystemExit(0 if result['passed'] or sys.argv[3]!='0' else 1)
PY
    record_status=$?
    if [ "$status" -eq 0 ]; then status=$record_status; fi
    exit "$status"
}
trap cleanup EXIT
# The trace contains only public tool/source paths and explicitly empty child environments.
exec 2> >(tee "$out/commands.log" >&2)
set -x
refs=(HEAD refs/remotes/origin/main)
if [[ "$ref" == refs/tags/* ]]; then refs+=("$ref"); fi
timeout 60 git bundle create "$out/source.bundle" "${refs[@]}"
git show "$revision:scripts/build-application.py" > "$out/stage.py"
cp --no-dereference "$native" "$out/native-input.tar"
chmod 444 "$out/source.bundle" "$out/stage.py" "$out/native-input.tar"
timeout 300 docker build --platform linux/amd64 --network none --file native/Dockerfile.apk-fetch --tag "$tools" native
mkdir -m 700 "$out/packaged" "$out/native-sbom" "$out/image-context"
timeout 30 docker volume create --driver local --opt type=tmpfs --opt device=tmpfs --opt o=size=4g,uid=10001,gid=10001,mode=0700 "$volume"
timeout 30 docker run --detach --name "$keeper" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 32m --memory-swap 32m --cpus 0.25 --pids-limit 8 --mount "type=volume,src=$volume,dst=/work,readonly" --entrypoint /bin/sleep "$tools" 3000
stage() {
    phase=$1; network=$2; memory=$3; seconds=$4
    inputs=()
    if [ "$phase" = build ]; then inputs+=(--mount "type=bind,src=$out/native-input.tar,dst=/native.tar,readonly"); fi
    if [ "$phase" = inventory ]; then
        inputs+=(--mount "type=bind,src=$out/image.tar,dst=/image.tar,readonly" --mount "type=bind,src=$out/image.json,dst=/image.json,readonly")
    fi
    args=(--revision "$revision")
    if [ -n "$ref" ]; then args+=(--ref "$ref" --sha "$sha"); fi
    timeout "$seconds" docker run --rm --name "$prefix-$phase" --network "$network" --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory "$memory" --memory-swap "$memory" --cpus 2 --pids-limit 128 --ulimit nofile=1024:1024 --ulimit fsize=536870912:536870912 \
        --mount "type=volume,src=$volume,dst=/work" \
        --mount type=bind,src=/usr,dst=/usr,readonly --mount type=bind,src=/usr/bin,dst=/bin,readonly \
        --mount type=bind,src=/usr/lib,dst=/lib,readonly --mount type=bind,src=/usr/lib64,dst=/lib64,readonly \
        --mount "type=bind,src=$goroot,dst=/go,readonly" --mount "type=bind,src=$node,dst=/tools/node,readonly" \
        \
        --mount "type=bind,src=$out/source.bundle,dst=/source.bundle,readonly" \
        "${inputs[@]}" --mount "type=bind,src=$out/stage.py,dst=/stage.py,readonly" \
        --workdir /work --entrypoint /usr/bin/env "$tools" -i PATH=/usr/bin:/bin PYTHONDONTWRITEBYTECODE=1 /usr/bin/python3 /stage.py "$phase" "${args[@]}" > "$out/$phase.log" 2>&1
}
stage acquire bridge 1536m 600
stage build none 6g 1200
for name in scanner-onefile-linux-amd64 manifest.json LICENSE SHA256SUMS build.json; do
    timeout 90 docker cp "$keeper:/work/package/$name" "$out/packaged/$name"
done
for name in native-origin.cdx.json native-origin-inventory.json receipt.json; do
    timeout 30 docker cp "$keeper:/work/native-sbom/$name" "$out/native-sbom/$name"
done
timeout 90 docker cp "$keeper:/work/onefile-sbom" "$out/onefile-sbom"
timeout 90 docker cp "$keeper:/work/application-sources.tar" "$out/application-sources.tar"
for name in application app-buildinfo launcher-buildinfo; do
    timeout 30 docker cp "$keeper:/work/$name.json" "$out/$name.json"
done
for name in Dockerfile .dockerignore LICENSE; do git show "$revision:$name" > "$out/image-context/$name"; done
python3 - "$out" "$revision" "$native" <<'PY'
import sys,pathlib,json,hashlib,stat,shutil
out=pathlib.Path(sys.argv[1]);package=out/'packaged'
def regular(path,limit):
 s=path.lstat();assert stat.S_ISREG(s.st_mode) and 0<s.st_size<=limit;return path
meta=json.loads(regular(out/'application.json',2<<20).read_text());build=json.loads(regular(package/'build.json',2<<20).read_text())
assert meta['passed'] and meta['source_revision']==sys.argv[2]
with open(sys.argv[3],'rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==meta['native_archive_sha256']
with regular(package/'scanner-onefile-linux-amd64',128<<20).open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==build['binary_sha256']
assert regular(package/'LICENSE',65536).read_bytes()==(out/'image-context/LICENSE').read_bytes()
shutil.copyfile(package/'scanner-onefile-linux-amd64',out/'image-context/scanner')
PY
timeout 300 docker build --platform linux/amd64 --network none --tag "$image" "$out/image-context" > "$out/image-build.log" 2>&1
timeout 30 docker image inspect "$image" > "$out/image.json"
python3 - "$out/image.json" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))[0]['Config'];assert c['Entrypoint']==['/usr/local/bin/scanner'] and c['Cmd']==['serve'] and c['User']=='10001:10001'
PY
timeout 30 docker create --name "$smoke" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 768m --memory-swap 768m --cpus 1 --pids-limit 64 --tmpfs /tmp:rw,exec,nosuid,nodev,size=512m,mode=0700,uid=10001,gid=10001 "$image" version
timeout 90 docker cp "$smoke:/usr/local/bin/scanner" "$out/image-scanner"
python3 - "$out" <<'PY2'
import json,pathlib,sys,hashlib,stat
out=pathlib.Path(sys.argv[1]);p=out/'image-scanner';s=p.lstat();assert stat.S_ISREG(s.st_mode) and s.st_size<=128<<20
with p.open('rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
assert digest==json.loads((out/'packaged/build.json').read_text())['binary_sha256']
(out/'image-binary.json').write_text(json.dumps({'sha256':digest})+'\n');p.unlink()
PY2
timeout 90 docker start --attach "$smoke" > "$out/image-version.json"
timeout 30 docker inspect "$smoke" > "$out/image-container.json"
python3 - "$out" <<'PY2'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]);assert json.loads((out/'image-version.json').read_text())==json.loads((out/'application.json').read_text())['version']
s=json.loads((out/'image-container.json').read_text())[0]['State'];assert not s['Running'] and not s['OOMKilled'] and s['ExitCode']==0
PY2
timeout 120 docker image save --output "$out/image.tar" "$image"
chmod 444 "$out/image.tar" "$out/image.json"
stage inventory none 2g 300
timeout 90 docker cp "$keeper:/work/image-sbom" "$out/image-sbom"
python3 - "$out" <<'PY2'
import json,pathlib,sys,hashlib,stat
out=pathlib.Path(sys.argv[1]);checks={}
for name in ['image.tar','packaged/scanner-onefile-linux-amd64','packaged/LICENSE']:
 p=out/name;s=p.lstat();assert stat.S_ISREG(s.st_mode) and 0<s.st_size<=512<<20
 with p.open('rb') as f:checks[name]=hashlib.file_digest(f,'sha256').hexdigest()
(out/'candidate-checksums.json').write_text(json.dumps(checks,indent=2)+'\n')
PY2
