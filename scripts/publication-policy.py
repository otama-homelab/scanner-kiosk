#!/usr/bin/env python3
"""Plan immutable publication names and alias movement; never perform writes.

The publisher must obtain complete fresh remote state under its serialized lock,
validate candidate materials/approvals, and apply the returned plan separately.
This policy does not turn a ref check into permission to publish.
"""
import argparse
import hashlib
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
                or type(release.get('prerelease')) is not bool or not isinstance(release.get('tag_name'), str)
                or ('publication_complete' in release and type(release['publication_complete']) is not bool)):
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
        if not suffix and not release['draft'] and not release['prerelease'] and release.get('publication_complete', True):
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


def prepare_publication(candidate, approval_path, approval_sha256, ref, sha, event, state, repository=Path('.')):
    """Offline preflight; the approval hash must come from trusted operator state.

    Run credential-free in the same bounded/read-only sandbox as staging. This
    authenticates an explicit approval record, not its human review process.
    The writer still owns fresh service-state checks and the serialized lock.
    """
    stage_spec = importlib.util.spec_from_file_location('stage_candidate', Path(__file__).with_name('stage-candidate.py'))
    staging = importlib.util.module_from_spec(stage_spec)
    stage_spec.loader.exec_module(staging)
    manifest = staging.verify_staged(candidate)
    require = staging.require
    require(isinstance(approval_sha256, str) and re.fullmatch(r'[0-9a-f]{64}', approval_sha256), 'invalid trusted approval hash')
    approval_path = Path(approval_path)
    approval = staging.read_json(approval_path)
    staging.check_file(approval_path.parent, {'path': approval_path.name,
        'bytes': approval_path.stat().st_size, 'sha256': approval_sha256})
    manifest_hash = hashlib.sha256((Path(candidate)/'manifest.json').read_bytes()).hexdigest()
    gates = {'required_checks', 'security', 'license', 'g5', 'recipient_access', 'c1'}
    require(set(approval) == {'format', 'manifest_sha256', 'source_sha', 'repository', 'visibility', 'gates'}
            and type(approval['format']) is int and approval['format'] == 1
            and approval['manifest_sha256'] == manifest_hash and approval['source_sha'] == manifest['source_sha']
            and approval['repository'] == 'otama-homelab/scanner-kiosk'
            and approval['visibility'] in ['private', 'public'], 'approval scope/manifest mismatch')
    require(isinstance(approval['gates'], dict) and set(approval['gates']) == gates, 'missing acceptance gate')
    for gate in approval['gates'].values():
        require(isinstance(gate, dict) and set(gate) == {'passed', 'evidence_sha256'}
                and gate['passed'] is True and isinstance(gate['evidence_sha256'], str)
                and re.fullmatch(r'[0-9a-f]{64}', gate['evidence_sha256']), 'unpassed gate or missing evidence identity')
    require(manifest['scans_completed'] is True, 'incomplete advisory scans')
    plan = publication_plan(ref, sha, event, state, repository)
    require(manifest['source_sha'] == plan['source_sha']
            and manifest['version']['revision'] == plan['source_sha']
            and manifest['version']['version'] == plan['version'], 'candidate is not the exact main/tag build')
    return {**plan, 'manifest_sha256': manifest_hash, 'approval_sha256': approval_sha256,
            'repository': approval['repository'], 'visibility': approval['visibility'],
            'image_id': manifest['image_id'], 'advisory_threshold_passed': manifest['advisory_threshold_passed'],
            'uploads': sorted(r['file'] for r in manifest['files'].values())+
                       ['manifest.json', 'SHA256SUMS', 'receipt.json']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ref', required=True)
    parser.add_argument('--sha', required=True)
    parser.add_argument('--event', required=True)
    parser.add_argument('--remote-state', type=Path, required=True)
    parser.add_argument('--candidate', type=Path)
    parser.add_argument('--approval', type=Path)
    parser.add_argument('--approval-sha256')
    parser.add_argument('--artifact-id',type=int,help='Immutable staged artifact in the same push run')
    args = parser.parse_args()
    if any([args.candidate, args.approval, args.approval_sha256]) and not all([args.candidate, args.approval, args.approval_sha256]):
        parser.error('candidate preflight requires --candidate, --approval and --approval-sha256 together')
    if args.artifact_id is not None and (not args.candidate or not 0<args.artifact_id<1<<63):
        parser.error('artifact ID requires complete candidate preflight and a positive bounded ID')
    try:
        if args.remote_state.stat().st_size > 2 << 20:
            raise ValueError('remote state exceeds bound')
        state = json.loads(args.remote_state.read_text())
        result = (prepare_publication(args.candidate, args.approval, args.approval_sha256,
                                     args.ref, args.sha, args.event, state)
                  if args.candidate else publication_plan(args.ref, args.sha, args.event, state))
    except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError):
        parser.exit(1, 'publication plan rejected: ref, materials, approvals or remote state is invalid\n')
    if args.artifact_id is not None:
        result['artifact_id']=args.artifact_id
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
