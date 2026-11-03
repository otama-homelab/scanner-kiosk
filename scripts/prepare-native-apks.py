#!/usr/bin/env python3
"""Verify locked offline APK archives and select native inputs, without installation."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile

spec = importlib.util.spec_from_file_location('assemble_native', Path(__file__).with_name('assemble-native.py'))
assembly = importlib.util.module_from_spec(spec)
spec.loader.exec_module(assembly)


def write_verified(stream, destination, size, digest, mode):
    actual, copied = hashlib.sha256(), 0
    with destination.open('xb') as target:
        while chunk := stream.read(min(1 << 20, size - copied + 1)):
            copied += len(chunk)
            if copied > size:
                raise ValueError('native input exceeds recorded size')
            actual.update(chunk)
            target.write(chunk)
    if copied != size or actual.hexdigest() != digest:
        raise ValueError('native input does not match recorded hash')
    destination.chmod(mode)


def snapshot(root, name, destination, digest, size=None):
    with assembly.source_parent(root, name) as (parent, leaf):
        descriptor = os.open(leaf, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        with os.fdopen(descriptor, 'rb') as stream:
            info = os.fstat(stream.fileno())
            limit = 128 << 20 if size is not None else 64 << 10
            if not stat.S_ISREG(info.st_mode) or info.st_size > limit or (size is not None and size != info.st_size):
                raise ValueError('native input type or size is invalid')
            write_verified(stream, destination, info.st_size, digest, 0o444)


def extract_selected(archive_path, package, selection, output):
    seen, control, declared = set(), None, 0
    with tarfile.open(archive_path, mode='r:gz', ignore_zeros=True) as archive:
        for count, member in enumerate(archive, 1):
            declared += member.size
            if count > 100000 or declared > 512 << 20:
                raise ValueError('APK content exceeds bounds')
            name = member.name.removeprefix('./')
            if name == '.PKGINFO':
                if control is not None or not member.isfile() or member.size > 65536:
                    raise ValueError('invalid APK control metadata')
                control = {}
                wanted = {'pkgname', 'pkgver', 'arch', 'origin', 'commit'}
                for line in archive.extractfile(member).read().decode().splitlines():
                    key, separator, value = line.partition(' = ')
                    if separator and key in wanted:
                        if key in control:
                            raise ValueError('duplicate APK control field')
                        control[key] = value
            if name not in selection:
                continue
            if name in seen:
                raise ValueError('duplicate selected APK member')
            seen.add(name)
            entry, destination = selection[name], output/name
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if entry.get('link'):
                if not member.issym() or member.linkname != entry['link']:
                    raise ValueError('selected APK link differs from manifest')
                destination.symlink_to(member.linkname)
            else:
                if not member.isfile() or member.size != entry['size']:
                    raise ValueError('selected APK file differs from manifest')
                with archive.extractfile(member) as stream:
                    write_verified(stream, destination, entry['size'], entry['sha256'], entry['mode'])
    expected = {field: package[key] for field, key in [
        ('pkgname', 'name'), ('pkgver', 'version'), ('arch', 'archive_architecture'),
        ('origin', 'origin'), ('commit', 'source_commit')]}
    if control != expected or seen != set(selection):
        raise ValueError('APK controls or selected member coverage differ')


def prepare(lock_path, manifest, provenance, archives, keys, apk, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('APK output already exists')
    raw, origin_raw = lock_path.read_bytes(), provenance.read_bytes()
    lock, origin = json.loads(raw), json.loads(origin_raw)
    if lock['runtime_provenance_sha256'] != hashlib.sha256(origin_raw).hexdigest():
        raise ValueError('APK lock does not match provenance')
    files, selected = assembly.plan(manifest, origin)
    packages = lock['packages']
    if (not packages or len(packages) > 256 or len({p['name'] for p in packages}) != len(packages)
            or {p['name'] for p in packages} != set(origin['apk_source_components'])):
        raise ValueError('APK lock coverage is invalid')
    archives, keys = archives.resolve(strict=True), keys.resolve(strict=True)
    if not apk.is_absolute() or any(output.resolve().is_relative_to(root) for root in [archives, keys]):
        raise ValueError('invalid APK tool or output location')
    trusted = lock['trusted_keys_sha256']
    if not trusted or set(p.name for p in keys.iterdir()) != set(trusted):
        raise ValueError('trusted key set differs')
    selections, all_sources, total = {}, set(), 0
    for package in packages:
        name = package['name']
        if (not re.fullmatch('[a-z0-9][a-z0-9+_.-]{0,127}', name)
                or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9._+~]*-r[0-9]+', package['version'])
                or package['filename'] != name+'-'+package['version']+'.apk'
                or type(package['bytes']) is not int or not 0 < package['bytes'] < 128 << 20
                or not re.fullmatch('[0-9a-f]{64}', package['sha256'])):
            raise ValueError('invalid locked APK identity')
        total += package['bytes']
        component = origin['apk_source_components'][name]
        metadata = component['source_metadata']
        if any(package[key] != metadata[field] for key, field in [
                ('version', 'version'), ('origin', 'originPackage'), ('source_commit', 'gitCommitOfApkPort')]):
            raise ValueError('APK origin metadata differs')
        selections[name] = {selected[path][1]: files[path] for path in component['retained_files']}
        if len(selections[name]) != len(component['retained_files']) or all_sources.intersection(selections[name]):
            raise ValueError('APK source paths overlap')
        all_sources.update(selections[name])
    if total > 256 << 20 or any(parent.as_posix() in all_sources for name in all_sources for parent in PurePosixPath(name).parents):
        raise ValueError('APK inputs exceed bounds or have conflicting paths')
    with tempfile.TemporaryDirectory(prefix='scanner-native-apks-') as temporary:
        work = Path(temporary)
        (work/'keys').mkdir(mode=0o700)
        for name, digest in trusted.items():
            if not re.fullmatch(r'[A-Za-z0-9@_.+-]+\.pub', name):
                raise ValueError('invalid trusted key name')
            snapshot(keys, name, work/'keys'/name, digest)
        snapshots = []
        for package in packages:
            destination = work/package['filename']
            snapshot(archives, package['filename'], destination, package['sha256'], package['bytes'])
            snapshots.append(destination)
        # Verify immutable private snapshots before any archive parsing or output.
        subprocess.run([str(apk), '--no-network', '--keys-dir', str(work/'keys'), 'verify',
                        *map(str, snapshots)], stdin=subprocess.DEVNULL, check=True,
                       capture_output=True, timeout=120,
                       env={'PATH': '/usr/bin:/bin', 'HOME': str(work), 'TMPDIR': str(work), 'LC_ALL': 'C'})
        output.mkdir(mode=0o700)
        try:
            for package, archive in zip(packages, snapshots):
                extract_selected(archive, package, selections[package['name']], output)
            for name in all_sources:
                path = output/name
                if path.is_symlink() and (not path.resolve().is_relative_to(output.resolve()) or not path.is_file()):
                    raise ValueError('APK output link is not contained')
        except Exception as error:
            shutil.rmtree(output)
            if isinstance(error, (OSError, tarfile.TarError, UnicodeError)):
                raise ValueError('APK content unavailable or unsafe') from error
            raise
    return {'packages': len(packages), 'entries': len(all_sources),
            'lock_sha256': hashlib.sha256(raw).hexdigest(), 'publication_approved': False}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['lock', 'manifest', 'provenance', 'archives', 'keys', 'apk', 'output']:
        parser.add_argument('--'+name, type=Path, required=True)
    args = parser.parse_args()
    try:
        result = prepare(args.lock, args.manifest, args.provenance, args.archives, args.keys, args.apk, args.output)
    except (ValueError, OSError, subprocess.SubprocessError, KeyError, TypeError):
        parser.exit(1, 'native APK preparation rejected\n')
    print(json.dumps(result))
