#!/usr/bin/env python3
"""Collect notice bytes from integrity-verified locked public npm archives.

Does not install packages, execute scripts, mutate node_modules or approve licenses.
Includes optional foreign-platform dependencies; this is not a build-stage SBOM.
"""
import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import gzip
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import tarfile
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
PREFIXES = ('LICENSE', 'LICENCE', 'COPYING', 'NOTICE', 'COPYRIGHT', 'AUTHORS', 'PATENTS')
MAX_ARCHIVE = 32 * 1024 * 1024
MAX_EXPANDED = 128 * 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise ValueError('registry redirect refused')


def collect(item, output):
    path, package = item
    name = package.get('name') or path.rsplit('node_modules/', 1)[-1]
    url = urllib.parse.urlsplit(package['resolved'])
    if (url.scheme != 'https' or url.netloc != 'registry.npmjs.org'
            or url.query or url.fragment or not url.path.endswith('.tgz')):
        raise ValueError('non-public registry archive refused')
    integrity = package['integrity']
    if not integrity.startswith('sha512-'):
        raise ValueError('expected single SHA512 integrity')
    expected = base64.b64decode(integrity[7:], validate=True)
    if len(expected) != 64:
        raise ValueError('invalid SHA512 integrity')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(package['resolved'], timeout=30) as response:
        if response.status != 200 or response.headers.get('Content-Encoding'):
            raise ValueError('unexpected registry response')
        archive = response.read(MAX_ARCHIVE + 1)
    if len(archive) > MAX_ARCHIVE or hashlib.sha512(archive).digest() != expected:
        raise ValueError('archive size or integrity mismatch')
    with gzip.GzipFile(fileobj=io.BytesIO(archive)) as compressed:
        expanded = compressed.read(MAX_EXPANDED + 1)
    if len(expanded) > MAX_EXPANDED:
        raise ValueError('expanded archive exceeds bound')
    key = hashlib.sha256(path.encode()).hexdigest()
    entries, members, seen, manifest, total = [], [], set(), None, 0
    with tarfile.open(fileobj=io.BytesIO(expanded), mode='r:') as tar:
        for index, member in enumerate(tar):
            if index >= 20000:
                raise ValueError('too many archive entries')
            relative = PurePosixPath(member.name)
            if relative.is_absolute() or '..' in relative.parts:
                raise ValueError('unsafe archive path')
            members.append({'path': member.name, 'size': member.size,
                            'regular_file': member.isfile()})
            is_manifest = len(relative.parts) == 2 and relative.name == 'package.json'
            basename = relative.name.upper()
            is_notice = (basename.startswith(PREFIXES)
                         or basename.startswith(('THIRD-PARTY-NOTICE', 'THIRD_PARTY_NOTICE',
                                                 'THIRDPARTYNOTICE', 'THIRD-PARTY-LICENSE',
                                                 'THIRD_PARTY_LICENSE')))
            is_readme = basename.startswith('README')
            selected = is_manifest or is_notice or is_readme
            if not selected:
                continue
            if (not member.isfile() or member.name in seen
                    or member.size > 4 * 1024 * 1024):
                raise ValueError('invalid selected archive member')
            seen.add(member.name)
            stream = tar.extractfile(member)
            with stream:
                data = stream.read(4 * 1024 * 1024 + 1)
            total += len(data)
            if len(data) != member.size or total > 16 * 1024 * 1024:
                raise ValueError('notice size exceeds bound')
            if is_manifest:
                if manifest is not None:
                    raise ValueError('multiple root package manifests')
                manifest = json.loads(data)
                if manifest.get('name') != name or manifest.get('version') != package['version']:
                    raise ValueError('archive package identity mismatch')
            # Never extract archive paths. Output filenames are content independent.
            target = output / 'notices' / key / str(len(entries))
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            entries.append({'archive_path': member.name,
                            'bundled_path': str(target.relative_to(output)),
                            'kind': 'manifest' if is_manifest else 'notice' if is_notice else 'readme',
                            'sha256': hashlib.sha256(data).hexdigest()})
    if manifest is None:
        raise ValueError('missing package manifest')
    return {'lock_path': path, 'name': name, 'version': package['version'],
            'integrity': integrity, 'resolved': package['resolved'],
            'archive_sha256': hashlib.sha256(archive).hexdigest(),
            'declared_license': manifest.get('license'),
            'archive_members': members,
            'notice_files': [e for e in entries if e['kind'] == 'notice'],
            'readme_files': [e for e in entries if e['kind'] == 'readme'],
            'files': entries}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    output = args.output_dir.resolve()
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    lock_path = ROOT / 'web/package-lock.json'
    before = lock_path.read_bytes()
    receipt = {'passed': False, 'started_at': datetime.now(timezone.utc).isoformat(),
               'scope': __doc__, 'lock_sha256': hashlib.sha256(before).hexdigest(),
               'script_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    try:
        lock = json.loads(before)
        items = sorted((k, v) for k, v in lock['packages'].items() if k)
        with ThreadPoolExecutor(max_workers=4) as executor:
            packages = list(executor.map(lambda item: collect(item, output), items))
        if lock_path.read_bytes() != before:
            raise ValueError('source lockfile changed')
        index = output / 'index.json'
        index.write_text(json.dumps({'scope': __doc__, 'packages': packages}, indent=2) + '\n')
        receipt.update(passed=True, packages=len(packages),
                       packages_without_notice_files=[p['lock_path'] for p in packages if not p['notice_files']],
                       notice_files=sum(len(p['notice_files']) for p in packages),
                       index_sha256=hashlib.sha256(index.read_bytes()).hexdigest())
    except Exception as error:
        receipt['error'] = type(error).__name__ + ': ' + str(error)
    finally:
        receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
        (output / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))
    return 0 if receipt['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
