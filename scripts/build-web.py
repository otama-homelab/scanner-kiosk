#!/usr/bin/env python3
"""Build static Astro assets and embed them in the Go executable."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def identity_flags(ref, sha, repository):
    if ref is None and sha is None:
        return []
    if ref is None or sha is None:
        raise ValueError('--ref and --sha must be supplied together')
    identity = json.loads(subprocess.run(
        [sys.executable, str(ROOT / 'scripts/check-release-ref.py'), '--ref', ref, '--sha', sha],
        cwd=repository, check=True, capture_output=True, text=True, timeout=30).stdout)
    return ['-trimpath', '-buildvcs=false',
            '-ldflags=-s -w -X scanner-kiosk/internal/buildinfo.Version=' + identity['version']
            + ' -X scanner-kiosk/internal/buildinfo.Revision=' + identity['source_sha']]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True)
    parser.add_argument('--ref')
    parser.add_argument('--sha')
    args = parser.parse_args(argv)
    try:
        flags = identity_flags(args.ref, args.sha, ROOT)
    except (ValueError, subprocess.SubprocessError):
        parser.error('invalid build identity: provide a verified trunk or release ref and its SHA')
    subprocess.run(['npm', 'ci', '--ignore-scripts', '--no-audit', '--no-fund'], cwd=ROOT / 'web', check=True)
    subprocess.run(['npm', 'run', 'build'], cwd=ROOT / 'web', check=True)
    for asset in (ROOT / 'web/dist').rglob('*'):
        relative = asset.relative_to(ROOT / 'web/dist').as_posix()
        auxiliary_asset = (relative.startswith('pdfjs/') and asset.suffix in ['.bcmap', '.pfb', '.ttf', '.wasm']) or relative in {'pdfjs/NOTICE.txt', 'ui/NOTICE.txt'}
        if asset.is_symlink():
            raise RuntimeError('symlink in built assets')
        if asset.is_file() and (any(part.startswith('.') for part in asset.relative_to(ROOT / 'web/dist').parts)
                                or (asset.suffix not in ['.js', '.mjs', '.css', '.svg']
                                    and relative != 'index.html' and not auxiliary_asset)):
            raise RuntimeError('unreviewed built asset type')
    destination = ROOT / 'internal/webui/dist'
    if destination.is_symlink():
        raise RuntimeError('refusing symlink asset destination')
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(ROOT / 'web/dist', destination)
    subprocess.run(['go', 'build', '-tags', 'webui', *flags, '-o',
                    str(Path(args.output).resolve()), './cmd/scanner'],
                   cwd=ROOT, env=dict(os.environ, CGO_ENABLED='0'), check=True)


if __name__ == '__main__':
    main()
