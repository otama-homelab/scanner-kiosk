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
tesseract_smoke=$prefix-tesseract-smoke
tools=$prefix:tools
image=$prefix:runtime
tesseract_image=$prefix:tesseract
cleanup() {
    status=$?
    trap - EXIT
    set +e
    timeout 60 docker rm --force "$acquirer" "$builder" "$smoke" "$tesseract_smoke" "$keeper" "$prefix-inventory" >/dev/null 2>&1
    timeout 60 docker volume rm "$volume" >> "$out/cleanup.log" 2>&1
    clean_status=$?
    timeout 60 docker image rm --force "$tools" >> "$out/cleanup.log" 2>&1
    image_status=$?
    if [ "$status" -eq 0 ] && { [ "$clean_status" -ne 0 ] || [ "$image_status" -ne 0 ]; }; then status=1; fi
    if [ "$status" -ne 0 ]; then timeout 60 docker image rm --force "$image" "$tesseract_image" >> "$out/cleanup.log" 2>&1; fi
    python3 - "$out" "$revision" "$status" "$image" "$tesseract_image" <<'PY'
import sys,json,pathlib
out=pathlib.Path(sys.argv[1]);result={'passed':sys.argv[3]=='0','source_revision':sys.argv[2],
 'image_tag':sys.argv[4] if sys.argv[3]=='0' else None,
 'tesseract_image_tag':sys.argv[5] if sys.argv[3]=='0' else None,'publication_approved':False}
for name in ['application','image','image-version','image-container','image-binary','image-payload',
             'tesseract-image','tesseract-image-version','tesseract-image-container','tesseract-image-binary','tesseract-image-payload']:
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
mkdir -m 700 "$out/packaged" "$out/native-sbom"
timeout 30 docker volume create --driver local --opt type=tmpfs --opt device=tmpfs --opt o=size=4g,uid=10001,gid=10001,mode=0700 "$volume"
timeout 30 docker run --detach --name "$keeper" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 32m --memory-swap 32m --cpus 0.25 --pids-limit 8 --mount "type=volume,src=$volume,dst=/work,readonly" --entrypoint /bin/sleep "$tools" 5400
stage() {
    phase=$1; network=$2; memory=$3; seconds=$4
    inputs=()
    if [ "$phase" = build ]; then inputs+=(--mount "type=bind,src=$out/native-input.tar,dst=/native.tar,readonly"); fi
    if [ "$phase" = inventory ]; then
        inputs+=(--mount "type=bind,src=$out/base-image,dst=/base-image,readonly"
                 --mount "type=bind,src=$out/runtime-images/runtime-image-inputs.json,dst=/runtime-inputs.json,readonly")
        for role in core tesseract; do
            inputs+=(--mount "type=bind,src=$out/runtime-images/$role/native-manifest.json,dst=/$role-native-manifest.json,readonly")
        done
        for runtime in image tesseract-image; do
            for suffix in .tar .json -rootfs.tar -container.json; do
                inputs+=(--mount "type=bind,src=$out/$runtime$suffix,dst=/$runtime$suffix,readonly")
            done
        done
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
for name in application app-buildinfo launcher-buildinfo service-buildinfo service-version; do
    timeout 30 docker cp "$keeper:/work/$name.json" "$out/$name.json"
done
timeout 120 docker cp "$keeper:/work/runtime-images" "$out/runtime-images"
for role in core tesseract; do
    timeout 90 docker cp "$keeper:/work/$role-native-sbom" "$out/$role-native-sbom"
done
python3 - "$out" "$revision" "$native" <<'PY'
import sys,pathlib,json,hashlib,stat
out=pathlib.Path(sys.argv[1]);package=out/'packaged'
def regular(path,limit):
 s=path.lstat();assert stat.S_ISREG(s.st_mode) and 0<s.st_size<=limit;return path
meta=json.loads(regular(out/'application.json',2<<20).read_text());build=json.loads(regular(package/'build.json',2<<20).read_text())
assert meta['passed'] and meta['source_revision']==sys.argv[2]
with open(sys.argv[3],'rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==meta['native_archive_sha256']
with regular(package/'scanner-onefile-linux-amd64',128<<20).open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==build['binary_sha256']
inputs_path=regular(out/'runtime-images/runtime-image-inputs.json',2<<20)
assert hashlib.sha256(inputs_path.read_bytes()).hexdigest()==meta['runtime_inputs_sha256']
inputs=json.loads(inputs_path.read_bytes());assert inputs['source_revision']==sys.argv[2] and set(inputs['roles'])=={'core','tesseract'}
assert inputs['roles']['core']['binary']['sha256']==build['app_sha256']
for role in inputs['roles']:
 assert regular(package/'LICENSE',65536).read_bytes()==(out/'runtime-images'/role/'LICENSE').read_bytes()
PY
for role in core tesseract; do
    if [ "$role" = core ]; then
        runtime=image; tag=$image; container=$smoke; binary=scanner
    else
        runtime=tesseract-image; tag=$tesseract_image; container=$tesseract_smoke; binary=scanner-tesseract-api
    fi
    timeout 300 docker build --platform linux/amd64 --network none --tag "$tag" "$out/runtime-images/$role" > "$out/$runtime-build.log" 2>&1
    timeout 30 docker image inspect "$tag" > "$out/$runtime.json"
    python3 - "$out/$runtime.json" "$binary" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))[0]['Config'];assert c['Entrypoint']==['/usr/local/bin/'+sys.argv[2]] and c['Cmd']==['serve'] and c['User']=='10001:10001'
PY
    timeout 30 docker create --name "$container" --network none --read-only --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges --memory 768m --memory-swap 768m --cpus 1 --pids-limit 64 --tmpfs /tmp:rw,exec,nosuid,nodev,size=512m,mode=0700,uid=10001,gid=10001 "$tag" version
    timeout 90 docker cp "$container:/usr/local/bin/$binary" "$out/$runtime-executable"
    python3 - "$out" "$runtime" "$role" <<'PY2'
import json,pathlib,sys,hashlib,stat
out=pathlib.Path(sys.argv[1]);prefix=sys.argv[2];p=out/(prefix+'-executable');s=p.lstat();assert stat.S_ISREG(s.st_mode) and 0<s.st_size<=128<<20
with p.open('rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
assert digest==json.loads((out/'runtime-images/runtime-image-inputs.json').read_text())['roles'][sys.argv[3]]['binary']['sha256']
(out/(prefix+'-binary.json')).write_text(json.dumps({'sha256':digest})+'\n');p.unlink()
PY2
    timeout 90 docker start --attach "$container" > "$out/$runtime-version.json"
    timeout 30 docker inspect "$container" > "$out/$runtime-container.json"
    python3 - "$out" "$runtime" "$role" <<'PY2'
import json,pathlib,sys
out=pathlib.Path(sys.argv[1]);prefix=sys.argv[2];version=json.loads((out/'application.json').read_text())['version']
if sys.argv[3]=='tesseract':version={**version,'native_runtime_sha256':json.loads((out/'runtime-images/runtime-image-inputs.json').read_text())['roles']['tesseract']['native_manifest_sha256']}
assert json.loads((out/(prefix+'-version.json')).read_text())==version
s=json.loads((out/(prefix+'-container.json')).read_text())[0]['State'];assert not s['Running'] and not s['OOMKilled'] and s['ExitCode']==0
PY2
    timeout 120 docker image save --output "$out/$runtime.tar" "$tag"
    timeout 120 docker export --output "$out/$runtime-rootfs.tar" "$container"
    chmod 444 "$out/$runtime.tar" "$out/$runtime.json" "$out/$runtime-rootfs.tar" "$out/$runtime-container.json"
done
install -d -m 755 "$out/base-image"
while IFS= read -r path; do
    install -d -m 755 "$out/base-image/$(dirname "$path")"
    timeout 30 docker cp "$smoke:/${path#image/}" "$out/base-image/$path"
    chmod 444 "$out/base-image/$path"
done < <(python3 scripts/base-source.py paths --lock native/base-source.lock.json)
stage inventory none 2g 600
for runtime in image tesseract-image; do
    timeout 90 docker cp "$keeper:/work/$runtime-sbom" "$out/$runtime-sbom"
    timeout 30 docker cp "$keeper:/work/$runtime-payload.json" "$out/$runtime-payload.json"
    rm -- "$out/$runtime-rootfs.tar"
done
timeout 30 docker cp "$keeper:/work/base-sources.tar" "$out/base-sources.tar"
timeout 30 docker cp "$keeper:/work/base-inventory.json" "$out/base-inventory.json"
python3 - "$out" <<'PY2'
import json,pathlib,sys,hashlib,stat
out=pathlib.Path(sys.argv[1]);checks={}
for name in ['image.tar','tesseract-image.tar','packaged/scanner-onefile-linux-amd64','packaged/LICENSE']:
 p=out/name;s=p.lstat();assert stat.S_ISREG(s.st_mode) and 0<s.st_size<=512<<20
 with p.open('rb') as f:checks[name]=hashlib.file_digest(f,'sha256').hexdigest()
(out/'candidate-checksums.json').write_text(json.dumps(checks,indent=2)+'\n')
PY2
