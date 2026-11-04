# Native library replacement and personal repackaging

The delivered executable binds every native file to its embedded manifest.
Editing an installed runtime cache is rejected. There is no flag to bypass that
check. A deliberately modified native tree instead needs its own reviewed
manifest and a newly built standalone executable. This does not require the
original distributor's signing key or a runtime verification exception.

This document describes the technical packaging path. Project licensing,
component-specific permissions, complete corresponding source/relink materials
and recipient delivery are still open release requirements. It does not grant
permission to distribute a modified build or establish LGPL compliance.

## Inputs

Use the exact matching application source, Python 3 and Go 1.27.3 in an isolated
build environment without credentials or external networking. The scanner's end
user does not need these build tools. Supply an already checked static Linux
amd64 `webui` app as `./app-scanner` and its verified native tree as
`./native-verified`, using the build/assembly instructions in
[the native input guide](../../native/README.md).

Build a compatible replacement library from the appropriate source and build
inputs separately. Preserve its required notices and corresponding source;
account for SONAME, symlinks and dependency changes. Packaging checks file
identity, not ABI compatibility or vulnerability/license acceptance. Do not
present modified bytes as the original signed APK contents or reuse original
package metadata as proof of their origin.

## Repackage a separate native tree

Run from the source repository root. All output paths below must be new.

```sh
mkdir -m 700 ./work-repack
cp -a ./native-verified ./work-repack/native
```

Replace the intended library only inside `work-repack/native`, preserving the
required paths and executable/read-only file modes. Leave the original input,
product locks, executable and installed cache intact. Retain your change's
source, transformation record and notices alongside the personal build.

Generate a provisional inventory without editing the product's native lock:

```sh
python3 scripts/pack-standalone.py \
  --app ./app-scanner \
  --native ./work-repack/native \
  --destination ./work-repack/provisional

python3 - <<'PY'
import json
from pathlib import Path
root = Path('work-repack')
original = json.loads(Path('native/runtime-manifest.json').read_text())
provisional = json.loads((root/'provisional/manifest.json').read_text())
files = provisional['files']
files.pop('app/scanner')
original['files'] = files
with (root/'user-native-manifest.json').open('x') as output:
    output.write(json.dumps(original, sort_keys=True, indent=2) + '\n')
PY
```

Review the generated manifest against the original, including each changed
file, mode, link and new dependency. It is an inventory of the supplied tree,
not an approval. After that review, build a separate executable:

```sh
python3 scripts/build-standalone.py \
  --app ./app-scanner \
  --native ./work-repack/native \
  --native-manifest ./work-repack/user-native-manifest.json \
  --output-dir ./work-repack/build

XDG_CACHE_HOME="$PWD/work-repack/cache" \
  ./work-repack/build/scanner-onefile-linux-amd64 version
```

The new executable has its own payload/executable identity and private cache.
The original executable continues to enforce its original inventory. Exercise
the modified component's real operations in isolation before treating it as a
usable replacement. Never install a fixture or modified build over a production
scanner worker while it may own work or a physical lease.

## Local validation and limits

A bounded offline fixture changed only a non-loaded ELF section in the retained
GnuTLS library. All allocated sections remained byte-identical. It regenerated
the native manifest using the commands above and built a standalone executable
without the host module cache, a signing key or a verification bypass.

The original executable rejected its tampered scratch cache. The separate user
build installed the modified library into a fresh cache; the loader selected it,
and an actual scanimage process mapped it while making one authenticated TLS GET
to an isolated loopback fixture returning 503. No scan acquisition or external
request occurred. This validates binary replacement/repackaging mechanics; it is
not a source rebuild, semantic modification, ABI compatibility test, complete
source offer or license approval. The fixture build was discarded and the
product/native locks and accepted development candidate stayed unchanged.
