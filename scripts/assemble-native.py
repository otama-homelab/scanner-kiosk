#!/usr/bin/env python3
"""Assemble an offline native tree from pinned, separately supplied inputs.

Hash identity is enforced here; input acquisition, signatures, source builds and
distribution approval remain the caller's responsibility. No input code runs.
"""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import re
import shutil
import stat
import tarfile
import tempfile


def safe_name(name):
    if (not isinstance(name, str) or not name or '\x00' in name
            or '\\' in name or PurePosixPath(name).is_absolute()
            or PurePosixPath(name).as_posix() != name
            or any(p in {'.', '..'} for p in name.split('/'))):
        raise ValueError('invalid native input path')
    return name


def plan(manifest, provenance):
    raw = manifest.read_bytes()
    if hashlib.sha256(raw).hexdigest() != provenance['runtime_manifest_sha256']:
        raise ValueError('provenance does not match manifest')
    files = json.loads(raw)['files']
    if not files or len(files) > 4096:
        raise ValueError('invalid native inventory')
    total = 0
    for name, entry in files.items():
        safe_name(name)
        if entry.get('link'):
            link = entry['link']
            if not isinstance(link, str) or '\x00' in link or PurePosixPath(link).is_absolute():
                raise ValueError('invalid native link')
            target = posixpath.normpath(posixpath.join(posixpath.dirname(name), link))
            if target not in files or any(entry.get(k) != v for k, v in [('size', 0), ('mode', 0), ('sha256', '')]):
                raise ValueError('native link leaves inventory')
        else:
            if (type(entry['size']) is not int or entry['size'] < 0
                    or entry['mode'] not in [0o444, 0o555]
                    or not re.fullmatch('[0-9a-f]{64}', entry['sha256'])):
                raise ValueError('invalid native file identity')
            total += entry['size']
    if total > 512 << 20:
        raise ValueError('native inventory exceeds size limit')
    for name in files:
        visited = set()
        while files[name].get('link'):
            if name in visited:
                raise ValueError('native link cycle')
            visited.add(name)
            name = posixpath.normpath(posixpath.join(posixpath.dirname(name), files[name]['link']))
    selected = {}

    def add(name, role, source):
        if name not in files or name in selected:
            raise ValueError('incomplete or duplicate native origin')
        selected[name] = (role, safe_name(source))

    for component in provenance['apk_source_components'].values():
        relocated = component.get('relocated_from', {})
        if not set(relocated).issubset(component['retained_files']):
            raise ValueError('unknown relocated entry')
        for name in component['retained_files']:
            add(name, 'apk', relocated.get(name, name))
    for component in provenance['upstream_source_components'] + provenance['firstparty_components']:
        for name, entry in component['files'].items():
            if entry != files.get(name):
                raise ValueError('source build identity differs from manifest')
            add(name, 'built', name)
    for name in provenance['notice_files']:
        add(name, 'notices', name)
    if set(selected) != set(files):
        raise ValueError('native origin coverage is incomplete')
    return files, selected


@contextmanager
def source_parent(root, name):
    # Descriptor-relative traversal refuses symlink parents, including swap races.
    descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in PurePosixPath(name).parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        yield descriptor, PurePosixPath(name).name
    finally:
        os.close(descriptor)


def unpack_builds(archives, files, selected, root):
    expected = {name: files[name] for name, (role, _) in selected.items() if role == 'built'}
    seen, links, count, total = set(), {}, 0, 0
    if not 1 <= len(archives) <= 16:
        raise ValueError('invalid build archive count')
    try:
        for archive in archives:
            descriptor = os.open(archive, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(descriptor, 'rb') as source:
                info = os.fstat(source.fileno())
                total += info.st_size
                if not stat.S_ISREG(info.st_mode) or total > 512 << 20:
                    raise ValueError('build archives must be bounded regular uncompressed tar files')
                with tarfile.open(fileobj=source, mode='r:') as stream:
                    for member in stream:
                        count += 1
                        if count > 4096:
                            raise ValueError('too many build archive members')
                        name = member.name.removeprefix('./').rstrip('/')
                        if name in ['', '.'] and member.isdir():
                            continue
                        safe_name(name)
                        if member.isdir():
                            continue
                        if not name.startswith('native/'):
                            if not member.isfile():
                                raise ValueError('unsafe build archive metadata')
                            continue
                        name = name.removeprefix('native/')
                        if name not in expected or name in seen:
                            raise ValueError('unregistered or duplicate build artifact')
                        seen.add(name)
                        entry = expected[name]
                        if entry.get('link'):
                            if not member.issym() or member.linkname != entry['link']:
                                raise ValueError('build artifact link changed')
                            links[name] = entry['link']
                            continue
                        if not member.isfile() or member.size != entry['size']:
                            raise ValueError('build artifact type or size changed')
                        destination = root/name
                        destination.parent.mkdir(parents=True, exist_ok=True)
                        digest = hashlib.sha256()
                        with stream.extractfile(member) as data, destination.open('xb') as target:
                            while chunk := data.read(1 << 20):
                                digest.update(chunk)
                                target.write(chunk)
                        if digest.hexdigest() != entry['sha256']:
                            raise ValueError('build artifact hash changed')
                        destination.chmod(entry['mode'])
        if seen != set(expected):
            raise ValueError('build artifact coverage is incomplete')
        # Create verified links last so archive paths can never traverse them.
        for name, target in links.items():
            (root/name).parent.mkdir(parents=True, exist_ok=True)
            (root/name).symlink_to(target)
    except (OSError, tarfile.TarError) as error:
        raise ValueError('build archive unavailable or unsafe') from error


def assemble(manifest, provenance, apk_root, built_root, notices_root, output,
             *, built_archives=None):
    if output.exists() or output.is_symlink():
        raise FileExistsError('native output already exists')
    origin = json.loads(provenance.read_bytes())
    files, selected = plan(manifest, origin)
    if built_archives is not None:
        if built_root is not None:
            raise ValueError('choose a built root or build archives')
        with tempfile.TemporaryDirectory(prefix='scanner-native-builds-') as temporary:
            root = Path(temporary)
            unpack_builds(built_archives, files, selected, root)
            return assemble(manifest, provenance, apk_root, root, notices_root, output)
    roots = {'apk': apk_root.resolve(strict=True), 'built': built_root.resolve(strict=True),
             'notices': notices_root.resolve(strict=True)}
    if any(output.resolve().is_relative_to(root) for root in roots.values()):
        raise ValueError('output must be separate from inputs')
    output.mkdir(mode=0o700)
    try:
        for name, (role, source) in sorted(selected.items()):
            entry, destination = files[name], output/name
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            with source_parent(roots[role], source) as (parent, leaf):
                if entry.get('link'):
                    if os.readlink(leaf, dir_fd=parent) != entry['link']:
                        raise ValueError('native symlink changed')
                    destination.symlink_to(entry['link'])
                else:
                    descriptor = os.open(leaf, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
                    with os.fdopen(descriptor, 'rb') as stream:
                        info = os.fstat(stream.fileno())
                        if not stat.S_ISREG(info.st_mode) or info.st_size != entry['size']:
                            raise ValueError('native input type or size changed')
                        digest, copied = hashlib.sha256(), 0
                        with destination.open('xb') as target:
                            while chunk := stream.read(min(1 << 20, entry['size'] - copied + 1)):
                                copied += len(chunk)
                                if copied > entry['size']:
                                    raise ValueError('native input grew')
                                digest.update(chunk)
                                target.write(chunk)
                        if copied != entry['size'] or digest.hexdigest() != entry['sha256']:
                            raise ValueError('native input hash changed')
                    destination.chmod(entry['mode'])
    except Exception as error:
        shutil.rmtree(output)
        if isinstance(error, OSError):
            raise ValueError('native input unavailable or unsafe') from error
        raise
    return {'entries': len(files), 'runtime_manifest_sha256': origin['runtime_manifest_sha256'],
            'publication_approved': False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['manifest', 'provenance', 'apk-root', 'notices-root', 'output']:
        parser.add_argument('--' + name, type=Path, required=True)
    built = parser.add_mutually_exclusive_group(required=True)
    built.add_argument('--built-root', type=Path)
    built.add_argument('--built-archive', type=Path, action='append')
    args = parser.parse_args()
    print(json.dumps(assemble(args.manifest, args.provenance, args.apk_root,
                              args.built_root, args.notices_root, args.output,
                              built_archives=args.built_archive)))
