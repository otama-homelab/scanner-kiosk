#!/usr/bin/env python3
"""Catalog a checked onefile and native inputs offline, without Docker or publishing.

Run inside a bounded OS sandbox: no network, read-only inputs, private scratch.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tempfile

spec = importlib.util.spec_from_file_location('native_sbom', Path(__file__).with_name('native-sbom.py'))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


def identity(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 1 << 30:
            raise ValueError('expected bounded regular executable')
        digest, size = hashlib.sha256(), 0
        while chunk := stream.read(min(1 << 20, info.st_size - size + 1)):
            size += len(chunk)
            if size > info.st_size:
                raise ValueError('executable grew')
            digest.update(chunk)
        after = os.fstat(stream.fileno())
        if size != info.st_size or (info.st_size, info.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
            raise ValueError('executable changed')
        return {'sha256': digest.hexdigest(), 'bytes': size}


def catalog(binary, build_receipt, packed_manifest, native_manifest, provenance, syft, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('onefile SBOM output already exists')
    build = json.loads(build_receipt.read_bytes())
    origin = json.loads(provenance.read_bytes())
    files, _ = native.assembler.plan(native_manifest, origin)
    packed_raw = packed_manifest.read_bytes()
    packed = json.loads(packed_raw)['files']
    if (set(packed) != set(files) | {'app/scanner'}
            or {k: v for k, v in packed.items() if k != 'app/scanner'} != files):
        raise ValueError('packed and native manifests differ')
    app = packed['app/scanner']
    if (app.get('mode') != 0o555 or type(app.get('size')) is not int
            or not 0 < app['size'] <= 512 << 20
            or not isinstance(app.get('sha256'), str)
            or not re.fullmatch('[0-9a-f]{64}', app['sha256']) or app.get('link')):
        raise ValueError('invalid packed app identity')
    binary_identity = identity(binary)
    if (binary_identity['sha256'] != build['binary_sha256']
            or binary_identity['bytes'] != build['binary_bytes']
            or app['sha256'] != build['app_sha256']
            or build['native_manifest_sha256'] != origin['runtime_manifest_sha256']
            or build['native_entries'] != len(files)):
        raise ValueError('onefile inputs do not match build receipt')
    tool_identity = identity(syft)
    with tempfile.TemporaryDirectory(prefix='scanner-onefile-sbom-', dir=output.parent) as directory:
        work = Path(directory)
        bundle = work/'bundle'
        bundle.mkdir(mode=0o700)
        for name in ['home', 'tmp', 'cache']:
            (work/name).mkdir()
        env = {'PATH': '/usr/bin:/bin', 'HOME': str(work/'home'),
               'TMPDIR': str(work/'tmp'), 'XDG_CACHE_HOME': str(work/'cache'),
               'LC_ALL': 'C.UTF-8', 'GOMAXPROCS': '2', 'SYFT_PARALLELISM': '2',
               'SYFT_CHECK_FOR_APP_UPDATE': 'false'}

        def run(command, name, timeout):
            stdout, stderr = bundle/(name+'.stdout'), bundle/(name+'.stderr')
            with stdout.open('wb') as out, stderr.open('wb') as err:
                subprocess.run(command, cwd=work, env=env, stdout=out, stderr=err,
                               check=True, timeout=timeout)
            return stdout

        version = json.loads(run([str(binary.resolve()), 'version'], 'onefile-version', 60).read_bytes())
        roots = list((work/'cache/scanner-kiosk/native').iterdir())
        if len(roots) != 1 or not roots[0].is_dir() or roots[0].is_symlink():
            raise ValueError('onefile did not produce one private runtime')
        root = roots[0]
        native.verify_native(root, packed)
        native_receipt = native.catalog(native_manifest, provenance, root, bundle/'native')
        tool_version = json.loads(run([str(syft.resolve()), 'version', '--output', 'json'],
                                      'syft-version', 30).read_bytes())
        if tool_version.get('application') != 'syft':
            raise ValueError('expected Syft tool identity')
        config = work/'syft.yaml'
        config.write_text('{}\n')
        run([str(syft.resolve()), '--config', str(config), 'scan', 'dir:'+str(root),
             '--quiet', '--output', 'syft-json='+str(bundle/'onefile.syft.json'),
             '--output', 'cyclonedx-json='+str(bundle/'onefile.cdx.json')], 'catalog', 180)
        inventory = json.loads((bundle/'onefile.syft.json').read_bytes())
        go_count = sum(p['type'] == 'go-module' for p in inventory['artifacts'])
        document = json.loads((bundle/'onefile.cdx.json').read_bytes())
        if not go_count or document.get('bomFormat') != 'CycloneDX' or not document.get('components'):
            raise ValueError('compiled Go inventory missing')
        artifacts = {p.relative_to(bundle).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                     for p in sorted(bundle.rglob('*')) if p.is_file()}
        receipt = {'binary_sha256': binary_identity['sha256'], 'binary_bytes': binary_identity['bytes'],
                   'build_receipt_sha256': hashlib.sha256(build_receipt.read_bytes()).hexdigest(),
                   'packed_manifest_sha256': hashlib.sha256(packed_raw).hexdigest(),
                   'version': version, 'native': native_receipt, 'go_package_count': go_count,
                   'syft': {'sha256': tool_identity['sha256'], 'version': tool_version},
                   'artifacts_sha256': artifacts, 'publication_approved': False,
                   'vulnerability_gate_passed': False, 'license_gate_passed': False,
                   'container_base_included': False,
                   'scope': 'Hash-bound onefile compiled Go inventory and native selected-file supplement. '
                            'Keep a separate immutable image inventory for the container base. '
                            'Overlapping components are not unique dependency totals; no release approval.'}
        (bundle/'receipt.json').write_text(json.dumps(receipt, sort_keys=True, indent=2)+'\n')
        output.mkdir(mode=0o700)
        try:
            shutil.copytree(bundle, output, dirs_exist_ok=True)
        except Exception:
            shutil.rmtree(output)
            raise
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['binary', 'build-receipt', 'packed-manifest', 'native-manifest',
                 'provenance', 'syft', 'output-dir']:
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(catalog(args.binary, args.build_receipt, args.packed_manifest,
                             args.native_manifest, args.provenance, args.syft, args.output_dir)))
