#!/usr/bin/env python3
"""Validate the exact checkout and its preview/release channel; never publish."""
import argparse
import json
from pathlib import Path
import re
import subprocess


def tag_parts(tag):
    if not isinstance(tag, str):
        raise ValueError('release tag must be text')
    match = re.fullmatch(r'v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?', tag)
    if not match:
        raise ValueError('release requires a vMAJOR.MINOR.PATCH tag (optional prerelease)')
    suffix = match.group(4)
    if suffix and any(part.isdigit() and len(part) > 1 and part.startswith('0') for part in suffix.split('.')):
        raise ValueError('numeric prerelease identifiers cannot have leading zeroes')
    return tuple(int(match.group(i)) for i in [1, 2, 3]), suffix


def release_ref(ref, sha, repository=Path('.')):
    if not re.fullmatch(r'(?:[0-9a-f]{40}|[0-9a-f]{64})', sha):
        raise ValueError('invalid source SHA')

    def git(*args):
        return subprocess.run(['git', *args], cwd=repository, check=True,
                              capture_output=True, text=True, timeout=10).stdout.strip()

    commit = git('rev-parse', '--verify', sha + '^{commit}')
    if git('rev-parse', '--verify', 'HEAD') != commit:
        raise ValueError('checkout does not match event SHA')
    if ref == 'refs/heads/main':
        git('merge-base', '--is-ancestor', commit, 'refs/remotes/origin/main')
        return {'channel': 'preview', 'version': 'preview-' + commit[:12],
                'source_sha': commit, 'prerelease': True}
    tag = ref.removeprefix('refs/tags/')
    if not ref.startswith('refs/tags/'):
        raise ValueError('release requires a vMAJOR.MINOR.PATCH tag (optional prerelease)')
    _, suffix = tag_parts(tag)
    if git('rev-parse', '--verify', ref + '^{commit}') != commit:
        raise ValueError('release tag does not match event SHA')
    git('merge-base', '--is-ancestor', commit, 'refs/remotes/origin/main')
    return {'channel': 'release', 'version': tag, 'source_sha': commit,
            'prerelease': bool(suffix)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ref', required=True)
    parser.add_argument('--sha', required=True)
    args = parser.parse_args()
    try:
        result = release_ref(args.ref, args.sha)
    except (ValueError, subprocess.SubprocessError):
        parser.exit(1, 'release ref rejected: checkout, version or trunk ancestry is invalid\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
