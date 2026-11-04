#!/usr/bin/env python3
"""Package locked external source bytes offline, without extracting or executing them.

This is the APK source subset, not a complete corresponding-source approval.
Run with read-only inputs and a bounded writable output directory.
"""
import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tarfile
import tempfile

spec = importlib.util.spec_from_file_location('fetch_sources', Path(__file__).with_name('fetch-source-inputs.py'))
fetch = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fetch)


def pack(lock, inputs, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('source archive already exists')
    descriptor = os.open(lock, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 2 << 20:
            raise ValueError('invalid source lock')
        raw = stream.read((2 << 20)+1)
    if len(raw) != info.st_size:
        raise ValueError('source lock changed')
    entries = fetch.validate(json.loads(raw))
    with tempfile.TemporaryDirectory(prefix='source-pack-', dir=output.parent) as directory:
        temporary = Path(directory)
        archive_path = temporary/'sources.tar'
        with tarfile.open(archive_path, 'w', format=tarfile.USTAR_FORMAT) as archive:
            def add(name, size, stream):
                member = tarfile.TarInfo(name)
                member.size, member.mode = size, 0o444
                archive.addfile(member, stream)
            add('source-inputs.lock.json', len(raw), io.BytesIO(raw))
            for entry in sorted(entries, key=lambda row: row['path']):
                with fetch.apk.prepare.assembly.source_parent(inputs, entry['path']) as (parent, leaf):
                    descriptor = os.open(leaf, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
                    with os.fdopen(descriptor, 'rb') as stream:
                        info = os.fstat(stream.fileno())
                        if not stat.S_ISREG(info.st_mode) or info.st_size != entry['bytes']:
                            raise ValueError('source type or size mismatch')
                        snapshot = temporary/'verified'
                        fetch.apk.prepare.write_verified(stream, snapshot, entry['bytes'], entry['sha256'], 0o444)
                with snapshot.open('rb') as stream:
                    add(entry['path'], entry['bytes'], stream)
                snapshot.unlink()
        archive_path.chmod(0o444)
        os.link(archive_path, output)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lock', type=Path, required=True)
    parser.add_argument('--inputs', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    pack(args.lock, args.inputs, args.output)
