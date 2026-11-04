#!/usr/bin/env python3
"""Retrieve the fixed upstream Tesseract source as data in a bounded sandbox."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import urllib.request

spec = importlib.util.spec_from_file_location('apk_fetch', Path(__file__).with_name('fetch-native-apks.py'))
apk_fetch = importlib.util.module_from_spec(spec)
spec.loader.exec_module(apk_fetch)


def fetch(lock_path, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('source output exists')
    descriptor = os.open(lock_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 65536:
            raise ValueError('source lock must be a bounded regular file')
        raw = stream.read(65537)
    if len(raw) != info.st_size:
        raise ValueError('source lock changed during reading')
    lock = json.loads(raw)
    if not isinstance(lock, dict):
        raise ValueError('source lock must be an object')
    commit, digest, size = (lock.get(key) for key in ['commit', 'sha256', 'bytes'])
    if (not isinstance(commit, str) or not re.fullmatch('[0-9a-f]{40}', commit)
            or lock.get('url') != 'https://codeload.github.com/tesseract-ocr/tesseract/tar.gz/'+commit
            or not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)
            or type(size) is not int or not 0 < size <= 32 << 20):
        raise ValueError('invalid fixed upstream source identity')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), apk_fetch.NoRedirect())
    request = urllib.request.Request(lock['url'], headers={'User-Agent':'scanner-kiosk-build-inputs'})
    with tempfile.TemporaryDirectory(prefix='tesseract-source-', dir=output.parent) as temporary:
        source = Path(temporary)/'source.tar.gz'
        with opener.open(request, timeout=60) as response:
            apk_fetch.prepare.write_verified(response, source, size, digest, 0o444)
        os.link(source, output)
    return {'commit':commit, 'bytes':size, 'sha256':digest, 'publication_approved':False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lock', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(fetch(args.lock, args.output)))
