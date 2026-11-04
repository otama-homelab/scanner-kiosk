#!/usr/bin/env python3
"""Acquire locked external native sources as data; run in a bounded empty-env sandbox."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import tempfile
import urllib.request

spec = importlib.util.spec_from_file_location('apk_fetch', Path(__file__).with_name('fetch-native-apks.py'))
apk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(apk)


def validate(lock):
    if type(lock.get('format')) is not int or lock['format'] != 1:
        raise ValueError('invalid source lock format')
    files = lock.get('files')
    if not isinstance(files, list) or not 1 <= len(files) <= 2048:
        raise ValueError('invalid source file count')
    seen, total = set(), 0
    for entry in files:
        name = entry.get('path', '')
        if (not isinstance(name, str) or name in seen or '\\' in name
                or any(p in ['', '.', '..'] for p in name.split('/'))
                or PurePosixPath(name).is_absolute()):
            raise ValueError('invalid/duplicate source path')
        if re.fullmatch(r'recipes/[0-9a-f]{40}/(?:main|community)/[A-Za-z0-9+_.-]+/[A-Za-z0-9+_./-]+', name):
            url = 'https://raw.githubusercontent.com/alpinelinux/aports/'+name.removeprefix('recipes/')
            role = 'apk-recipe'
        elif re.fullmatch(r'upstream/apk/[A-Za-z0-9+_.-]+', name):
            url = ('https://codeload.github.com/alexpevzner/sane-airscan/tar.gz/refs/tags/0.99.36'
                   if name.endswith('/sane-airscan-0.99.36.tar.gz') else
                   'https://distfiles.alpinelinux.org/distfiles/v3.24/'+PurePosixPath(name).name)
            role = 'apk-upstream-source'
        else:
            raise ValueError('unknown source path')
        if (entry.get('url') != url or entry.get('role') != role
                or type(entry.get('bytes')) is not int or not 0 < entry['bytes'] <= 1 << 30
                or not isinstance(entry.get('sha256'), str) or not re.fullmatch(r'[0-9a-f]{64}', entry['sha256'])):
            raise ValueError('invalid source identity/URL')
        seen.add(name)
        total += entry['bytes']
    if total > 2 << 30:
        raise ValueError('source inputs exceed total bound')
    return files


def fetch(lock_path, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('source output exists')
    descriptor = os.open(lock_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 2 << 20:
            raise ValueError('invalid source lock file')
        raw = stream.read((2 << 20)+1)
    if len(raw) != info.st_size:
        raise ValueError('source lock changed')
    files = validate(json.loads(raw))
    output.mkdir(mode=0o700)
    receipt = {'complete': False, 'lock_sha256': hashlib.sha256(raw).hexdigest(),
               'files': [], 'source_executed': False, 'publication_approved': False}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), apk.NoRedirect())
    try:
        for entry in files:
            destination = output/entry['path']
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            with tempfile.TemporaryDirectory(prefix='source-', dir=output) as directory:
                temporary = Path(directory)/'input'
                request = urllib.request.Request(entry['url'], headers={'User-Agent': 'scanner-kiosk-source-inputs'})
                with opener.open(request, timeout=60) as response:
                    apk.prepare.write_verified(response, temporary, entry['bytes'], entry['sha256'], 0o444)
                os.link(temporary, destination)
            receipt['files'].append(entry)
        receipt['complete'] = True
    finally:
        (output/'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lock', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    result = fetch(args.lock, args.output)
    print(json.dumps({'complete': result['complete'], 'files': len(result['files']), 'publication_approved': False}))
