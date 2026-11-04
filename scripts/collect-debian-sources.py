#!/usr/bin/env python3
"""Retain exact Debian source files from a separately authenticated, hash-pinned manifest.

Does not extract/build source, authenticate the supplied manifest, publish an offer,
or approve licenses. All Debian binary/source identities must match the supplied SBOM.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


def snapshot_url(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != 'https' or parsed.netloc != 'snapshot.debian.org' or parsed.query or parsed.fragment:
        raise ValueError('only Snapshot Debian HTTPS URLs are accepted')


class SnapshotRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, url):
        snapshot_url(url)
        return super().redirect_request(request, fp, code, msg, headers, url)


def plan_files(manifest, sbom):
    if manifest.get('complete_exact_source_coverage') is not True:
        raise ValueError('manifest source coverage is incomplete')
    expected, observed, files, groups = {}, {}, {}, set()
    for source in manifest['sources']:
        identity = (source['source_package'], source['source_version'])
        if identity in groups or not source['files'] or not source['binary_packages']:
            raise ValueError('duplicate or empty source group')
        groups.add(identity)
        for package in source['binary_packages']:
            name = package['name'].split(':', 1)[0]
            if name in expected:
                raise ValueError('duplicate manifest binary package')
            expected[name] = (package['version'], *identity)
        for item in source['files']:
            name, size, digest = item['filename'], item['size'], item['sha256']
            if name in {'receipt.json', 'source-manifest.json', 'runtime.syft.json'} or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9.+~_-]*', name):
                raise ValueError('unsafe source filename')
            if type(size) is not int or not 0 < size <= 512 * 1024 * 1024 or not re.fullmatch(r'[0-9a-f]{64}', digest):
                raise ValueError('invalid source size or SHA256')
            snapshot_url(item['url'])
            if not re.fullmatch(r'/archive/(debian|debian-security)/[0-9]{8}T[0-9]{6}Z/pool/[A-Za-z0-9/_.+~%-]+', urllib.parse.urlsplit(item['url']).path):
                raise ValueError('source URL is not a pinned Snapshot pool file')
            if urllib.parse.unquote(item['url'].rsplit('/', 1)[-1]) != name:
                raise ValueError('source URL filename mismatch')
            if name in files and files[name] != item:
                raise ValueError('conflicting source filenames')
            files[name] = item
    for package in sbom['artifacts']:
        if package['type'] != 'deb':
            continue
        name = package['name'].split(':', 1)[0]
        if name in observed:
            raise ValueError('duplicate SBOM binary package')
        metadata = package['metadata']
        observed[name] = (package['version'], metadata.get('source') or name,
                          metadata.get('sourceVersion') or package['version'])
    if not expected or observed != expected or len(expected) != manifest['binary_packages'] or len(groups) != manifest['source_groups']:
        raise ValueError('manifest and SBOM binary/source inventories differ')
    if len(files) > 4096 or sum(f['size'] for f in files.values()) > 2 * 1024**3:
        raise ValueError('source bundle exceeds collection bound')
    return sorted(files.values(), key=lambda item: item['filename'])


def verify_file(path, item):
    # Refuse symlinks without following them, including between check and open.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size != item['size'] or hashlib.file_digest(stream, 'sha256').hexdigest() != item['sha256']:
            raise ValueError('existing source file differs from manifest: ' + path.name)


def collect_file(item, directory, fetch):
    for attempt in range(3):
        try:
            return collect_file_once(item, directory, fetch)
        except urllib.error.HTTPError as error:
            if error.code not in {408, 429, 500, 502, 503, 504} or attempt == 2:
                raise
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            if attempt == 2:
                raise
        time.sleep(2 ** (attempt + 1))


def collect_file_once(item, directory, fetch):
    target = directory / item['filename']
    if target.is_symlink():
        raise ValueError('source destination is a symlink')
    if target.exists():
        verify_file(target, item)
        return 'reused'
    temporary = None
    try:
        with fetch(item['url'], timeout=60) as response:
            if response.status != 200 or response.headers.get('Content-Encoding'):
                raise ValueError('unexpected source response')
            with tempfile.NamedTemporaryFile(dir=directory, prefix='.source-', delete=False) as output:
                temporary = Path(output.name)
                digest, total = hashlib.sha256(), 0
                while True:
                    chunk = response.read(min(1024 * 1024, item['size'] - total + 1))
                    if not chunk:
                        break
                    total += len(chunk)
                    if total > item['size']:
                        raise ValueError('source response exceeds manifest size')
                    digest.update(chunk)
                    output.write(chunk)
                if total != item['size'] or digest.hexdigest() != item['sha256']:
                    raise ValueError('source response size or SHA256 mismatch')
                output.flush()
                os.fsync(output.fileno())
        # Publish without overwriting a concurrently created destination.
        os.link(temporary, target)
        return 'downloaded'
    finally:
        if temporary is not None:
            temporary.unlink()


def atomic_write(directory, name, data):
    with tempfile.NamedTemporaryFile(dir=directory, prefix='.receipt-', delete=False) as stream:
        temporary = Path(stream.name)
        try:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
            os.replace(temporary, directory / name)
        finally:
            temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--manifest-sha256', required=True, help='reviewed hash of separately authenticated manifest')
    parser.add_argument('--sbom', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    manifest_bytes, sbom_bytes = args.manifest.read_bytes(), args.sbom.read_bytes()
    if hashlib.sha256(manifest_bytes).hexdigest() != args.manifest_sha256:
        raise ValueError('manifest SHA256 does not match reviewed input')
    manifest, sbom = json.loads(manifest_bytes), json.loads(sbom_bytes)
    files = plan_files(manifest, sbom)
    directory = args.output_dir
    if directory.is_symlink():
        raise ValueError('bundle directory is a symlink')
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    if not directory.is_dir() or directory.stat().st_mode & 0o077:
        raise ValueError('bundle directory must be private')
    receipt = {'passed': False, 'scope': __doc__, 'started_at': datetime.now(timezone.utc).isoformat(),
               'manifest_sha256': args.manifest_sha256, 'sbom_sha256': hashlib.sha256(sbom_bytes).hexdigest(),
               'script_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               'image_id': sbom.get('source', {}).get('metadata', {}).get('imageID'),
               'binary_packages': manifest['binary_packages'], 'source_groups': manifest['source_groups'],
               'files': [], 'release_license_gate_passed': False}
    try:
        for name, data in [('source-manifest.json', manifest_bytes), ('runtime.syft.json', sbom_bytes)]:
            target = directory / name
            if target.is_symlink() or (target.exists() and target.read_bytes() != data):
                raise ValueError('bundle belongs to different inputs')
            atomic_write(directory, name, data)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), SnapshotRedirect())
        with ThreadPoolExecutor(max_workers=4) as pool:
            pending = {pool.submit(collect_file, item, directory, opener.open): item for item in files}
            try:
                for future in as_completed(pending):
                    item, status = pending[future], future.result()
                    receipt['files'].append(dict(item, status=status))
                    print(json.dumps({'file': item['filename'], 'status': status, 'completed': len(receipt['files']), 'total': len(files)}), flush=True)
            except BaseException:
                for future in pending:
                    future.cancel()
                raise
        for item in files:
            verify_file(directory / item['filename'], item)
        receipt.update(passed=True, source_bytes=sum(f['size'] for f in files), source_files=len(files))
    except Exception as error:
        receipt['error'] = type(error).__name__ + ': ' + str(error)
    finally:
        receipt['finished_at'] = datetime.now(timezone.utc).isoformat()
        atomic_write(directory, 'receipt.json', (json.dumps(receipt, indent=2) + '\n').encode())
    return 0 if receipt['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
