#!/usr/bin/env python3
"""Pack an already verified app and relocated Linux amd64 runtime for go:embed.

This does not fetch packages or establish their provenance; retain input receipts.
"""
import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import os
import tarfile


def pack(app, native, destination):
    native = native.resolve(strict=True)
    assert native.is_dir() and app.is_file() and not app.is_symlink()
    sources = {'app/scanner': app}
    for source in sorted(native.rglob('*')):
        if source.is_symlink() or source.is_file():
            name = source.relative_to(native).as_posix()
            assert name != 'tools' and not name.startswith(('tools/', 'app/'))
            sources[name] = source
        else:
            assert source.is_dir(), source
    for required in ('lib/ld-musl-x86_64.so.1', 'usr/bin/scanimage',
                     'usr/bin/tesseract', 'usr/libexec/scanner/sane-parameters',
                     'usr/share/tessdata/eng.traineddata', 'usr/share/tessdata/jpn.traineddata',
                     'usr/share/tessdata/osd.traineddata',
                     'usr/share/scanner-kiosk/ca-certificates.crt'):
        assert required in sources, required
    files = {}
    compressed = io.BytesIO()
    with gzip.GzipFile(fileobj=compressed, mode='wb', filename='', mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as archive:
            for name, source in sorted(sources.items()):
                header = tarfile.TarInfo(name)
                if source.is_symlink():
                    link = os.readlink(source)
                    assert not PurePosixPath(link).is_absolute()
                    assert source.resolve(strict=True).is_relative_to(native)
                    header.type = tarfile.SYMTYPE
                    header.linkname = link
                    files[name] = {'size': 0, 'mode': 0, 'sha256': '', 'link': link}
                    archive.addfile(header)
                else:
                    data = source.read_bytes()
                    mode = 0o555 if name == 'app/scanner' or source.stat().st_mode & 0o111 else 0o444
                    header.mode, header.size = mode, len(data)
                    files[name] = {'size': len(data), 'mode': mode,
                                   'sha256': hashlib.sha256(data).hexdigest()}
                    archive.addfile(header, io.BytesIO(data))
    payload = compressed.getvalue()
    manifest = {'payload_sha256': hashlib.sha256(payload).hexdigest(), 'files': files}
    destination.mkdir(parents=True, exist_ok=True)
    (destination / 'payload.tar.gz').write_bytes(payload)
    (destination / 'manifest.json').write_text(json.dumps(manifest, sort_keys=True, separators=(',', ':')) + '\n')
    print(json.dumps({'payload_sha256': manifest['payload_sha256'], 'bytes': len(payload), 'entries': len(files)}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, required=True)
    parser.add_argument('--native', type=Path, required=True)
    parser.add_argument('--destination', type=Path, default=Path('cmd/scanner-standalone'))
    args = parser.parse_args()
    pack(args.app, args.native, args.destination)
