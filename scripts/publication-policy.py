#!/usr/bin/env python3
"""Plan immutable publication names and alias movement; never perform writes.

The publisher must obtain complete fresh remote state under its serialized lock,
validate candidate materials/approvals, and apply the returned plan separately.
This policy does not turn a ref check into permission to publish.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import subprocess

spec = importlib.util.spec_from_file_location('release_ref', Path(__file__).with_name('check-release-ref.py'))
refs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(refs)


def publication_plan(ref, sha, event, state, repository=Path('.')):
    if event != 'push':
        raise ValueError('only main/tag push events are publication inputs')
    identity = refs.release_ref(ref, sha, repository)
    if not isinstance(state, dict) or set(state) != {'releases', 'image_tags', 'preview_source_sha'}:
        raise ValueError('complete release, image and preview state is required')
    releases, images, previous = state['releases'], state['image_tags'], state['preview_source_sha']
    if (not isinstance(releases, list) or len(releases) > 512
            or not isinstance(images, list) or len(images) > 512
            or any(not isinstance(t, str) or not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}', t) for t in images)
            or len(set(images)) != len(images)):
        raise ValueError('invalid or oversized remote state')
    tags, stable = set(), []
    for release in releases:
        if (not isinstance(release, dict) or type(release.get('draft')) is not bool
                or type(release.get('prerelease')) is not bool or not isinstance(release.get('tag_name'), str)):
            raise ValueError('invalid release state')
        tag = release['tag_name']
        if tag in tags:
            raise ValueError('duplicate release tag')
        tags.add(tag)
        if re.fullmatch(r'preview-(?:[0-9a-f]{40}|[0-9a-f]{64})', tag):
            if not release['prerelease']:
                raise ValueError('preview must be a prerelease')
            continue
        version, suffix = refs.tag_parts(tag)
        if not suffix and not release['draft'] and not release['prerelease']:
            stable.append(version)
    if previous is not None and (not isinstance(previous, str)
                                or not re.fullmatch(r'(?:[0-9a-f]{40}|[0-9a-f]{64})', previous)):
        raise ValueError('invalid preview source identity')
    if 'preview' in images and previous is None:
        raise ValueError('existing preview alias requires its verified source identity')
    if 'latest' in images and not stable:
        raise ValueError('existing latest alias has no qualified stable release')
    commit = identity['source_sha']
    preview = identity['channel'] == 'preview'
    release_tag = 'preview-'+commit if preview else identity['version']
    image_tag = 'sha-'+commit if preview else identity['version']
    if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}', image_tag):
        raise ValueError('version exceeds registry tag constraints')
    if release_tag in tags or image_tag in images:
        raise ValueError('immutable version already exists; rerun must not overwrite it')
    aliases = []
    if preview:
        if previous is None:
            aliases = ['preview']
        else:
            # Unknown ancestry is an error; a newer/diverged preview is left intact.
            subprocess.run(['git', 'rev-parse', '--verify', previous+'^{commit}'], cwd=repository,
                           check=True, capture_output=True, timeout=10)
            result = subprocess.run(['git', 'merge-base', '--is-ancestor', previous, commit],
                                    cwd=repository, capture_output=True, timeout=10)
            if result.returncode == 0:
                aliases = ['preview']
            elif result.returncode != 1:
                raise ValueError('cannot establish preview ancestry')
    elif not identity['prerelease']:
        version, _ = refs.tag_parts(identity['version'])
        if not stable or version > max(stable):
            aliases = ['latest']
    return {**identity, 'release_tag': release_tag, 'image_tag': image_tag,
            'aliases': aliases, 'external_writes': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ref', required=True)
    parser.add_argument('--sha', required=True)
    parser.add_argument('--event', required=True)
    parser.add_argument('--remote-state', type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.remote_state.stat().st_size > 2 << 20:
            raise ValueError('remote state exceeds bound')
        result = publication_plan(args.ref, args.sha, args.event,
                                  json.loads(args.remote_state.read_text()))
    except (ValueError, OSError, subprocess.SubprocessError):
        parser.exit(1, 'publication plan rejected: ref, immutable version or complete remote state is invalid\n')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
