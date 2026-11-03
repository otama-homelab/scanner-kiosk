#!/usr/bin/env python3
"""Retain checksum-bound aports source files as data; never extract or execute.

Requires a separately pinned recipe-content ledger. This does not authenticate
that pin or approve licensing, corresponding-source completeness or publication.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import tempfile
import time
import urllib.parse
import urllib.request

MAX_FILE = 2 * 1024**3
MAX_TOTAL = 4 * 1024**3
MIRROR = 'https://distfiles.alpinelinux.org/distfiles/v3.24/'


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, url):
        return None


def source_plan(root, expected):
    ledger = root / 'recipe-trees.json'
    if ledger.is_symlink() or not ledger.is_file():
        raise ValueError('recipe ledger must be a regular file')
    with ledger.open('rb') as stream:
        ledger_bytes = stream.read(2 * 1024**2 + 1)
    if (not re.fullmatch(r'[0-9a-f]{64}', expected) or len(ledger_bytes) > 2 * 1024**2
            or hashlib.sha256(ledger_bytes).hexdigest() != expected):
        raise ValueError('recipe ledger checksum mismatch')
    ledger = json.loads(ledger_bytes)
    if ledger.get('complete') is not True or not 1 <= len(ledger['files']) <= 2048:
        raise ValueError('recipe contents are incomplete')
    contents = {}
    content_bytes = 0
    for row in ledger['files']:
        name = row['path']
        relative = PurePosixPath(name)
        if relative.is_absolute() or '..' in relative.parts or relative.as_posix() != name or '\\' in name or name in contents:
            raise ValueError('invalid recipe path')
        path = root / name
        if (path.is_symlink() or not path.is_file() or not path.resolve().is_relative_to(root.resolve())
                or path.stat().st_size > 8 * 1024**2):
            raise ValueError('invalid recipe file')
        content_bytes += path.stat().st_size
        if content_bytes > 128 * 1024**2:
            raise ValueError('recipe content byte bound exceeded')
        with path.open('rb') as stream:
            data = stream.read(8 * 1024**2 + 1)
        if len(data) != row['bytes'] or hashlib.sha256(data).hexdigest() != row['sha256']:
            raise ValueError('recipe file checksum mismatch')
        contents[name] = data
    archives = {}
    local_files = 0
    seen = set()
    for origin in ledger['origins']:
        if (not re.fullmatch(r'[0-9a-f]{40}', origin['commit'])
                or origin['repository'] not in ['main', 'community']
                or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9+_.-]{0,95}', origin['origin'])):
            raise ValueError('invalid recorded origin')
        prefix = origin['commit'] + '/' + origin['repository'] + '/' + origin['origin']
        if prefix in seen:
            raise ValueError('duplicate origin')
        seen.add(prefix)
        blocks = re.findall(r'^sha512sums="([\s\S]*?)"', contents[prefix + '/APKBUILD'].decode(), re.MULTILINE)
        if len(blocks) != 1:
            raise ValueError('expected one literal sha512sums block')
        for line in blocks[0].strip().splitlines():
            match = re.fullmatch(r'([0-9a-f]{128})  ([A-Za-z0-9_][A-Za-z0-9._+-]{0,191})', line.strip())
            if not match:
                raise ValueError('unsupported checksum entry')
            checksum, filename = match.groups()
            local = prefix + '/' + filename
            if local in contents:
                if hashlib.sha512(contents[local]).hexdigest() != checksum:
                    raise ValueError('local source checksum mismatch')
                local_files += 1
            else:
                if (root / local).exists() or (root / local).is_symlink():
                    raise ValueError('unrecorded local source')
                if filename in archives and archives[filename]['sha512'] != checksum:
                    raise ValueError('conflicting source filename checksums')
                item = archives.setdefault(filename, {'filename': filename, 'sha512': checksum, 'origins': []})
                item['origins'].append(prefix)
    if not archives or len(archives) > 256:
        raise ValueError('invalid source file count')
    return {'recipe_ledger_sha256': expected, 'origin_count': len(seen), 'local_source_checks': local_files,
            'archives': [archives[name] for name in sorted(archives)]}


def download(item, output, limit):
    url = MIRROR + urllib.parse.quote(item['filename'], safe='')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={'User-Agent': 'scanner-kiosk-source-inventory'})
    sha512, sha256, size = hashlib.sha512(), hashlib.sha256(), 0
    deadline = time.monotonic() + 600
    fd, temporary = tempfile.mkstemp(prefix='.source-', dir=output)
    try:
        with os.fdopen(fd, 'wb') as target, opener.open(request, timeout=30) as source:
            while True:
                if time.monotonic() > deadline:
                    raise TimeoutError('source download deadline')
                chunk = source.read(min(1024**2, limit - size + 1))
                if not chunk:
                    break
                size += len(chunk)
                if size > limit:
                    raise ValueError('source download exceeds byte bound')
                sha512.update(chunk)
                sha256.update(chunk)
                target.write(chunk)
        if sha512.hexdigest() != item['sha512']:
            raise ValueError('upstream source checksum mismatch')
        destination = output / item['filename']
        os.link(temporary, destination)
        destination.chmod(0o400)
        return {**item, 'url': url, 'bytes': size, 'sha256': sha256.hexdigest()}
    finally:
        Path(temporary).unlink(missing_ok=True)


def collect(root, expected, output):
    plan = source_plan(root, expected)
    output.mkdir(mode=0o700)
    result = {**plan, 'complete': False, 'scope': __doc__.strip(), 'retained': [], 'errors': [], 'bytes': 0}
    receipt = output / 'source-archives.json'
    def save():
        temporary = output / '.receipt'
        temporary.write_text(json.dumps(result, indent=2) + '\n')
        os.replace(temporary, receipt)
    save()
    for item in plan['archives']:
        try:
            remaining = min(MAX_FILE, MAX_TOTAL - result['bytes'])
            if remaining <= 0:
                raise ValueError('source collection total byte bound')
            retained = download(item, output, remaining)
            result['retained'].append(retained)
            result['bytes'] += retained['bytes']
            print(json.dumps({'file': item['filename'], 'bytes': retained['bytes']}), flush=True)
        except Exception as error:
            result['errors'].append({'file': item['filename'], 'error_class': type(error).__name__,
                                     'http_status': getattr(error, 'code', None)})
            print(json.dumps(result['errors'][-1]), flush=True)
        save()
    result['complete'] = not result['errors'] and len(result['retained']) == len(plan['archives'])
    save()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--recipe-dir', type=Path, required=True)
    parser.add_argument('--recipe-ledger-sha256', required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    result = collect(args.recipe_dir, args.recipe_ledger_sha256, args.output_dir)
    print(json.dumps({'complete': result['complete'], 'retained': len(result['retained']),
                      'expected': len(result['archives']), 'bytes': result['bytes']}))
    if not result['complete']:
        parser.exit(1, 'source archives incomplete; verified files and receipt retained\n')


if __name__ == '__main__':
    main()
