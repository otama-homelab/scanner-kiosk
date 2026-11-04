#!/usr/bin/env python3
"""Fetch fixed APK URLs as data, then verify their hashes and trusted signatures.

Run in a bounded, credential-free build sandbox. Never installs packages,
extracts target code, publishes artifacts or establishes license approval.
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
import urllib.request

spec = importlib.util.spec_from_file_location('prepare_native', Path(__file__).with_name('prepare-native-apks.py'))
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)
REPOSITORIES = tuple('https://dl-cdn.alpinelinux.org/alpine/v3.24/'+name
                     for name in ['main', 'community'])


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, url):
        return None


def validate(lock):
    if not isinstance(lock, dict):
        raise ValueError('APK lock must be an object')
    versions = [lock[key] for key in ['format', 'format_version'] if key in lock]
    repositories, packages, keys = (lock.get(key) for key in
                                   ['repositories', 'packages', 'trusted_keys_sha256'])
    if (not versions or any(type(value) is not int or value != 1 for value in versions)
            or lock.get('platform') != 'linux/amd64'
            or not isinstance(repositories, list) or not repositories
            or any(repo not in REPOSITORIES for repo in repositories)
            or len(repositories) != len(set(repositories))
            or not isinstance(packages, list) or not 1 <= len(packages) <= 256
            or not isinstance(keys, dict) or not 1 <= len(keys) <= 16):
        raise ValueError('invalid native APK lock')
    for name, digest in keys.items():
        if (not re.fullmatch(r'[A-Za-z0-9@_.+-]+\.pub', name)
                or not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)):
            raise ValueError('invalid trusted key identity')
    names, total = set(), 0
    for package in packages:
        if not isinstance(package, dict):
            raise ValueError('invalid package record')
        name, version, digest = (package.get(key) for key in ['name', 'version', 'sha256'])
        if (not isinstance(name, str) or not re.fullmatch('[a-z0-9][a-z0-9+_.-]{0,127}', name)
                or name in names or not isinstance(version, str)
                or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._+~]*-r[0-9]+', version)
                or package.get('filename') != name+'-'+version+'.apk'
                or package.get('repository') not in ['main', 'community']
                or REPOSITORIES[['main', 'community'].index(package['repository'])] not in repositories
                or package.get('repository_architecture', 'x86_64') != 'x86_64'
                or type(package.get('bytes')) is not int or not 0 < package['bytes'] < 128 << 20
                or not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)):
            raise ValueError('invalid locked APK identity')
        names.add(name)
        total += package['bytes']
    if total > 512 << 20:
        raise ValueError('locked APK inputs exceed total bound')
    return packages


def fetch(lock_path, keys, apk, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('APK output already exists')
    descriptor = os.open(lock_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 2 << 20:
            raise ValueError('APK lock must be a bounded regular file')
        raw = stream.read((2 << 20)+1)
        if len(raw) != info.st_size:
            raise ValueError('APK lock changed during reading')
    lock = json.loads(raw)
    packages = validate(lock)
    keys = keys.resolve(strict=True)
    if (not apk.is_absolute() or set(path.name for path in keys.iterdir()) != set(lock['trusted_keys_sha256'])
            or output.resolve().is_relative_to(keys)):
        raise ValueError('invalid APK tool, trusted keys or output')
    with tempfile.TemporaryDirectory(prefix='scanner-apk-fetch-') as temporary:
        work = Path(temporary)
        for name in ['keys', 'verified']:
            (work/name).mkdir(mode=0o700)
        for name, digest in lock['trusted_keys_sha256'].items():
            prepare.snapshot(keys, name, work/'keys'/name, digest)
        env = {'PATH': '/usr/bin:/bin', 'HOME': str(work), 'TMPDIR': str(work), 'LC_ALL': 'C'}
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        for package in packages:
            url = ('https://dl-cdn.alpinelinux.org/alpine/v3.24/'+package['repository']
                   +'/x86_64/'+package['filename'])
            request = urllib.request.Request(url, headers={'User-Agent': 'scanner-kiosk-build-inputs'})
            with opener.open(request, timeout=30) as response:
                prepare.write_verified(response, work/'verified'/package['filename'],
                                       package['bytes'], package['sha256'], 0o444)
        subprocess.run([str(apk), '--no-network', '--keys-dir', str(work/'keys'), 'verify',
                        *[str(work/'verified'/package['filename']) for package in packages]],
                       stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                       env=env, check=True, timeout=120)
        output.mkdir(mode=0o700)
        try:
            for package in packages:
                path = output/package['filename']
                shutil.copyfile(work/'verified'/package['filename'], path)
                path.chmod(0o444)
        except Exception:
            shutil.rmtree(output)
            raise
    return {'packages': len(packages), 'bytes': sum(p['bytes'] for p in packages),
            'lock_sha256': hashlib.sha256(raw).hexdigest(), 'signatures_verified': True,
            'publication_approved': False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['lock', 'keys', 'apk', 'output']:
        parser.add_argument('--'+name, type=Path, required=True)
    args = parser.parse_args()
    try:
        result = fetch(args.lock, args.keys, args.apk, args.output)
    except (ValueError, OSError, subprocess.SubprocessError, KeyError, TypeError):
        parser.exit(1, 'locked APK retrieval rejected\n')
    print(json.dumps(result))
