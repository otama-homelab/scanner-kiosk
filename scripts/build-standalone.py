#!/usr/bin/env python3
"""Package a prebuilt embedded app with an exactly pinned native manifest.

Inputs must already have provenance/license/security approval for publication.
This command builds locally; it never downloads, tags, uploads or deploys.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('pack_standalone', Path(__file__).with_name('pack-standalone.py'))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


def validate_app(info):
    settings = {item['Key']: item['Value'] for item in info.get('Settings', [])}
    if (info.get('Path') != 'scanner-kiosk/cmd/scanner'
            or settings.get('GOOS') != 'linux' or settings.get('GOARCH') != 'amd64'
            or settings.get('CGO_ENABLED') != '0'
            or 'webui' not in settings.get('-tags', '').split(',')):
        raise ValueError('app must be the static Linux amd64 scanner built with webui')


def build(app, native, expected_manifest, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('output directory must not exist')
    if not app.is_file() or app.is_symlink():
        raise ValueError('app must be a regular file, not a symlink')
    try:
        descriptor = os.open(ROOT / 'LICENSE', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError:
        raise ValueError('project license must be a bounded regular file') from None
    with os.fdopen(descriptor, 'rb') as license_file:
        before = os.fstat(license_file.fileno())
        if not stat.S_ISREG(before.st_mode) or not 0 < before.st_size <= 65536:
            raise ValueError('project license must be a bounded regular file')
        license_bytes = license_file.read(65537)
        after = os.fstat(license_file.fileno())
        if (len(license_bytes) != before.st_size
                or (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
                != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns)):
            raise ValueError('project license changed during reading')
    license_sha256 = hashlib.sha256(license_bytes).hexdigest()
    expected_bytes = expected_manifest.read_bytes()
    expected = json.loads(expected_bytes)['files']
    if not isinstance(expected, dict) or not expected:
        raise ValueError('native manifest must contain files')
    expected = {key: value for key, value in expected.items() if key != 'app/scanner'}
    env = {key: os.environ[key] for key in
           ['PATH', 'HOME', 'TMPDIR', 'GOROOT', 'GOCACHE', 'GOMODCACHE', 'GOMAXPROCS'] if key in os.environ}
    env.update(CGO_ENABLED='0', GOOS='linux', GOARCH='amd64', GOTOOLCHAIN='local',
               GOWORK='off', GOENV='off', GOFLAGS='', GOPROXY='off', GOSUMDB='off')
    info = json.loads(subprocess.run(['go', 'version', '-m', '-json', str(app.resolve())],
                                    env=env, check=True, capture_output=True, text=True, timeout=30).stdout)
    validate_app(info)
    with tempfile.TemporaryDirectory(prefix='scanner-onefile-build-') as temporary:
        work = Path(temporary)
        launcher = work / 'cmd/scanner-standalone'
        launcher.mkdir(parents=True)
        for source in (ROOT / 'cmd/scanner-standalone').glob('*.go'):
            shutil.copyfile(source, launcher / source.name)
        for name in ['go.mod', 'go.sum']:
            shutil.copyfile(ROOT / name, work / name)
        packager.pack(app, native, launcher)
        manifest = json.loads((launcher / 'manifest.json').read_text())
        actual = {key: value for key, value in manifest['files'].items() if key != 'app/scanner'}
        if actual != expected:
            raise ValueError('native files differ from the pinned manifest')
        binary = work / 'scanner-onefile-linux-amd64'
        subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-buildvcs=false',
                        '-tags', 'bundled', '-ldflags=-s -w', '-o', str(binary),
                        './cmd/scanner-standalone'], cwd=work, env=env, check=True, timeout=180)
        sha = hashlib.sha256(binary.read_bytes()).hexdigest()
        receipt = {'binary_sha256': sha, 'binary_bytes': binary.stat().st_size,
                   'app_sha256': manifest['files']['app/scanner']['sha256'],
                   'native_manifest_sha256': hashlib.sha256(expected_bytes).hexdigest(),
                   'native_entries': len(actual), 'app_go_version': info['GoVersion'],
                   'project_license_sha256': license_sha256, 'publication_approved': False}
        output.mkdir(mode=0o700)
        try:
            shutil.copyfile(binary, output / binary.name)
            (output / binary.name).chmod(0o555)
            shutil.copyfile(launcher / 'manifest.json', output / 'manifest.json')
            (output / 'LICENSE').write_bytes(license_bytes)
            (output / 'LICENSE').chmod(0o444)
            (output / 'SHA256SUMS').write_text(sha + '  ' + binary.name + '\n'
                                              + license_sha256 + '  LICENSE\n')
            (output / 'build.json').write_text(json.dumps(receipt, indent=2) + '\n')
        except Exception:
            shutil.rmtree(output)
            raise
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, required=True)
    parser.add_argument('--native', type=Path, required=True)
    parser.add_argument('--native-manifest', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(build(args.app, args.native, args.native_manifest, args.output_dir)))


if __name__ == '__main__':
    main()
