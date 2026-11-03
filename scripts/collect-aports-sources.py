#!/usr/bin/env python3
"""Retain aports recipe trees at recorded APK commits, without executing them.

Not a complete upstream source offer: source archives referenced by APKBUILD,
SDK closure, licenses and reproducible native builds need separate verification.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import urllib.parse
import urllib.request

MAX_FILE = 8 * 1024 * 1024
MAX_TOTAL = 128 * 1024 * 1024
MAX_FILES = 2048


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, url):
        return None


def fetch(url, limit):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, headers={'User-Agent': 'scanner-kiosk-source-inventory',
                                                 'Accept': 'application/vnd.github+json'})
    with opener.open(request, timeout=20) as response:
        data = response.read(limit + 1)
    if len(data) > limit:
        raise ValueError('source response exceeds bound')
    return data


def plans(receipt):
    records = receipt.get('recipes')
    if not isinstance(records, list) or not 1 <= len(records) <= 64:
        raise ValueError('expected 1..64 recorded origins')
    seen = set()
    for record in records:
        origin, commit, repository = record.get('origin', ''), record.get('commit', ''), record.get('repository', '')
        key = (repository, origin, commit)
        if (record.get('collected') is not True or repository not in ['main', 'community']
                or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9+_.-]{0,95}', origin)
                or not re.fullmatch(r'[0-9a-f]{40}', commit)
                or not re.fullmatch(r'[0-9a-f]{64}', record.get('sha256', '')) or key in seen):
            raise ValueError('invalid or duplicate recorded origin')
        seen.add(key)
    return records


def collect(receipt, output):
    records = plans(receipt)
    output.mkdir(mode=0o700)
    result = {'complete': False, 'scope': __doc__.strip(), 'origins': [], 'files': [], 'bytes': 0}
    try:
        directories = 0
        for record in records:
            prefix = record['repository'] + '/' + record['origin']
            pending = [(prefix, 0)]
            found_recipe = False
            origin_files = 0
            while pending:
                folder, depth = pending.pop()
                directories += 1
                if depth > 8 or directories > 256:
                    raise ValueError('source tree depth/directory bound exceeded')
                url = ('https://api.github.com/repos/alpinelinux/aports/contents/'
                       + urllib.parse.quote(folder, safe='/') + '?ref=' + record['commit'])
                entries = json.loads(fetch(url, 2 * 1024 * 1024))
                if not isinstance(entries, list) or len(entries) >= 1000:
                    raise ValueError('invalid or potentially truncated directory listing')
                names = set()
                for entry in entries:
                    name = entry.get('name', '')
                    if (not isinstance(name, str) or name in ['', '.', '..']
                            or '/' in name or '\\' in name or '\x00' in name or name in names
                            or entry.get('path') != folder + '/' + name
                            or not re.fullmatch(r'[0-9a-f]{40}', entry.get('sha', ''))):
                        raise ValueError('invalid source entry path or object identity')
                    names.add(name)
                    if entry.get('type') == 'dir':
                        pending.append((entry['path'], depth + 1))
                        continue
                    if entry.get('type') != 'file':
                        raise ValueError('unsupported source entry type; not followed')
                    size = entry.get('size')
                    if (type(size) is not int or not 0 <= size <= MAX_FILE
                            or result['bytes'] + size > MAX_TOTAL or len(result['files']) >= MAX_FILES):
                        raise ValueError('source size/file bound exceeded')
                    url = ('https://raw.githubusercontent.com/alpinelinux/aports/' + record['commit']
                           + '/' + urllib.parse.quote(entry['path'], safe='/'))
                    data = fetch(url, MAX_FILE)
                    blob = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
                    sha = hashlib.sha256(data).hexdigest()
                    if len(data) != size or blob != entry['sha']:
                        raise ValueError('source Git blob mismatch')
                    if entry['path'] == prefix + '/APKBUILD':
                        if sha != record['sha256']:
                            raise ValueError('recorded APKBUILD checksum mismatch')
                        found_recipe = True
                    relative = record['commit'] + '/' + entry['path']
                    destination = output / relative
                    destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                    with destination.open('xb') as stream:
                        stream.write(data)
                    destination.chmod(0o600)
                    result['files'].append({'path': relative, 'bytes': size, 'sha256': sha, 'git_blob_sha1': blob})
                    result['bytes'] += size
                    origin_files += 1
            if not found_recipe:
                raise ValueError('recorded origin lacks APKBUILD')
            result['origins'].append({'origin': record['origin'], 'repository': record['repository'],
                                      'commit': record['commit'], 'files': origin_files})
            print(json.dumps({'origin': record['origin'], 'files': origin_files}), flush=True)
        result['complete'] = True
    except Exception as error:
        result['error_class'] = type(error).__name__
        raise
    finally:
        (output / 'recipe-trees.json').write_text(json.dumps(result, indent=2) + '\n')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--recipe-receipt', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    result = collect(json.loads(args.recipe_receipt.read_text()), args.output_dir)
    print(json.dumps({'complete': result['complete'], 'origins': len(result['origins']),
                      'files': len(result['files']), 'bytes': result['bytes']}))


if __name__ == '__main__':
    main()
