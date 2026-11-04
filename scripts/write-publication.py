#!/usr/bin/env python3
"""Transfer an approved candidate; default is service-read-only dry run.

Call only under the single GitHub publisher concurrency group. Preflight and
archive parsing happen separately without credentials. This process only hashes
approved files and uses installed gh/Docker clients; it builds/runs no candidate.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

staging = load('stage_candidate', 'stage-candidate.py')
policy = load('publication_policy', 'publication-policy.py')
require = staging.require
REPOSITORY = 'otama-homelab/scanner-kiosk'
REGISTRY = 'ghcr.io/'+REPOSITORY
MARKER = '<!-- scanner-kiosk:'


def command(args, env, input_data=None, timeout=60, allow_failure=False):
    result = subprocess.run(args, env=env, input=input_data, capture_output=True, timeout=timeout)
    if allow_failure:
        return result
    if result.returncode:
        raise RuntimeError(args[0]+' command failed with exit '+str(result.returncode))
    return result.stdout


def decode(raw):
    require(len(raw) <= 8 << 20, 'service response exceeds bound')
    return json.loads(raw, object_pairs_hook=staging.unique_object)


def api(path, env, body=None, missing=False, paginate=False):
    args = ['gh', 'api', '--hostname', 'github.com']
    if paginate:
        args += ['--paginate', '--slurp']
    args += [path]
    if body is not None:
        args += ['--method', 'POST' if path.endswith('/releases') else 'PATCH', '--input', '-']
    result = command(args, env, input_data=json.dumps(body).encode() if body is not None else None, allow_failure=True)
    if result.returncode:
        if missing and b'HTTP 404' in result.stderr:
            return None
        raise RuntimeError('GitHub API request failed')
    return decode(result.stdout)


def registry_image(tag, env):
    reference = REGISTRY+':'+tag
    result = command(['docker', 'manifest', 'inspect', '--verbose', reference], env, allow_failure=True)
    if result.returncode:
        error = result.stderr.decode(errors='replace').strip()
        if error.startswith('no such manifest: '+reference) or error.startswith('manifest unknown'):
            return None
        raise RuntimeError('registry inspection failed; absence was not established')
    document = decode(result.stdout)
    if isinstance(document, list):
        require(len(document) == 1, 'only single-platform images are supported')
        document = document[0]
    descriptor = document['Descriptor']
    manifest = document.get('SchemaV2Manifest') or document.get('OCIManifest')
    require(isinstance(manifest, dict), 'missing image manifest')
    digest, image_id = descriptor['digest'], manifest['config']['digest']
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', digest)
            and re.fullmatch(r'sha256:[0-9a-f]{64}', image_id), 'invalid registry identity')
    return {'digest': digest, 'image_id': image_id}


def pages_items(pages, key=None):
    require(isinstance(pages, list) and len(pages) <= 8, 'incomplete/oversized paginated response')
    result = []
    for page in pages:
        items = page[key] if key else page
        require(isinstance(items, list), 'invalid service page')
        result.extend(items)
    require(len(result) <= 512, 'service item bound exceeded')
    return result


def remote_source(ref, source, env):
    main = api('repos/'+REPOSITORY+'/git/ref/heads/main', env)['object']['sha']
    require(re.fullmatch(r'[0-9a-f]{40}', main), 'invalid remote main identity')
    comparison = api('repos/'+REPOSITORY+'/compare/'+source+'...'+main, env)
    require(comparison['status'] in ['ahead', 'identical']
            and comparison['merge_base_commit']['sha'] == source, 'source no longer belongs to remote main')
    if ref.startswith('refs/tags/'):
        obj = api('repos/'+REPOSITORY+'/git/ref/tags/'+ref.removeprefix('refs/tags/'), env)['object']
        for _ in range(8):
            if obj['type'] == 'commit':
                break
            require(obj['type'] == 'tag' and re.fullmatch(r'[0-9a-f]{40}', obj['sha']), 'invalid remote tag')
            obj = api('repos/'+REPOSITORY+'/git/tags/'+obj['sha'], env)['object']
        require(obj['type'] == 'commit' and obj['sha'] == source, 'remote tag moved')


def publication_metadata(release):
    body = release.get('body') or ''
    if MARKER not in body:
        return None
    require(body.count(MARKER) == 1, 'ambiguous publication metadata')
    raw, separator, _ = body.split(MARKER, 1)[1].partition('-->')
    require(separator and len(raw) <= 65536, 'invalid publication metadata')
    metadata = json.loads(raw, object_pairs_hook=staging.unique_object)
    require(isinstance(metadata, dict), 'invalid publication metadata object')
    return metadata


def fresh_state(plan, env):
    releases = pages_items(api('repos/'+REPOSITORY+'/releases?per_page=100', env, paginate=True))
    qualified = []
    for release in releases:
        metadata = publication_metadata(release)
        if metadata is not None:
            complete = metadata.get('format') == 1 and all(
                isinstance(metadata.get(key), str) and re.fullmatch(pattern, metadata[key])
                for key, pattern in [('source_sha', r'[0-9a-f]{40}'), ('manifest_sha256', r'[0-9a-f]{64}'),
                                     ('image_id', r'sha256:[0-9a-f]{64}'), ('image_digest', r'sha256:[0-9a-f]{64}')])
            release = {**release, 'publication_complete': bool(complete)}
        qualified.append(release)
    releases = qualified
    images = {tag: registry_image(tag, env) for tag in [plan['image_tag'], 'preview', 'latest']}
    previous = None
    for alias in ['preview', 'latest']:
        if images[alias] is None:
            continue
        owners = []
        for release in releases:
            metadata = publication_metadata(release)
            if (not release['draft'] and metadata and metadata.get('image_digest') == images[alias]['digest']
                    and metadata.get('image_id') == images[alias]['image_id']):
                owners.append((release, metadata))
        require(len(owners) == 1, 'existing alias has unknown/ambiguous publication owner')
        release, metadata = owners[0]
        source = metadata['source_sha']
        require(re.fullmatch(r'[0-9a-f]{40}', source), 'invalid alias source')
        if alias == 'preview':
            require(release['tag_name'] == 'preview-'+source and release['prerelease'], 'invalid preview owner')
            previous = source
        else:
            _, suffix = policy.refs.tag_parts(release['tag_name'])
            require(not suffix and not release['prerelease'], 'invalid latest owner')
    return {'releases': releases, 'image_tags': [tag for tag, image in images.items() if image is not None],
            'preview_source_sha': previous}


def body_text(plan, image_digest=None):
    metadata = {'format': 1, 'source_sha': plan['source_sha'], 'manifest_sha256': plan['manifest_sha256'],
                'image_id': plan['image_id'], 'image_digest': image_digest}
    return ('Scanner '+plan['version']+'\n\nSource: '+plan['source_sha']+'\nManifest: '+plan['manifest_sha256']+
            '\n\n'+MARKER+json.dumps(metadata, sort_keys=True)+' -->')


def require_current_checks(source, env):
    checks = pages_items(api('repos/'+REPOSITORY+'/commits/'+source+'/check-runs?per_page=100', env, paginate=True), 'check_runs')
    checks = [c for c in checks if c['name'] == 'required-checks' and c['app']['slug'] == 'github-actions'
              and c['head_sha'] == source]
    require(checks and all(type(c['id']) is int for c in checks), 'required-checks is missing or invalid')
    latest = max(checks, key=lambda c: c['id'])
    require(latest['status'] == 'completed' and latest['conclusion'] == 'success', 'current required-checks has not passed')


def transfer_inputs(candidate, plan_path, plan_sha256):
    require(candidate.is_dir() and not candidate.is_symlink(), 'candidate root must be a real directory')
    require(re.fullmatch(r'[0-9a-f]{64}', plan_sha256), 'invalid trusted plan hash')
    plan = staging.read_json(plan_path)
    staging.check_file(plan_path.parent, {'path': plan_path.name, 'bytes': plan_path.stat().st_size, 'sha256': plan_sha256})
    require(plan['repository'] == REPOSITORY and plan['visibility'] in ['private', 'public']
            and plan['external_writes'] is False and re.fullmatch(r'[0-9a-f]{64}', plan['approval_sha256']), 'invalid approved plan')
    manifest = staging.read_json(candidate/'manifest.json')
    staging.check_file(candidate, {'path': 'manifest.json', 'bytes': (candidate/'manifest.json').stat().st_size,
                                    'sha256': plan['manifest_sha256']})
    records = {}
    for role, record in manifest['files'].items():
        require(role in staging.NAMES and record['file'] == staging.NAMES[role], 'invalid upload filename')
        records[role] = {'path': record['file'], 'bytes': record['bytes'], 'sha256': record['sha256']}
    staging.validate_records(records)
    for record in records.values():
        staging.check_file(candidate, record)
    expected = sorted(record['path'] for record in records.values())+['manifest.json', 'SHA256SUMS', 'receipt.json']
    require(plan['uploads'] == expected and manifest['source_sha'] == plan['source_sha']
            and manifest['image_id'] == plan['image_id']
            and manifest['version']['revision'] == plan['source_sha']
            and manifest['version']['version'] == plan['version'], 'candidate/plan identity mismatch')
    checksums = staging.checksum_text(plan['manifest_sha256'], records).encode()
    staging.check_file(candidate, {'path': 'SHA256SUMS', 'bytes': len(checksums), 'sha256': hashlib.sha256(checksums).hexdigest()})
    receipt = staging.read_json(candidate/'receipt.json')
    require(receipt == {'passed': True, 'manifest_sha256': plan['manifest_sha256'], 'candidate_id': manifest['candidate_id'],
                       'publication_approved': False, 'external_writes': False}, 'incomplete staging receipt')
    return plan, {name: {'size': (candidate/name).stat().st_size,
                        'digest': 'sha256:'+hashlib.sha256((candidate/name).read_bytes()).hexdigest()}
                  for name in ['manifest.json', 'SHA256SUMS', 'receipt.json']} | {
                      record['path']: {'size': record['bytes'], 'digest': 'sha256:'+record['sha256']} for record in records.values()}


def publish(candidate, plan_path, plan_sha256, ref, sha, event, repository=Path('.'), apply=False):
    candidate, plan_path = Path(candidate).absolute(), Path(plan_path)
    plan, assets = transfer_inputs(candidate, plan_path, plan_sha256)
    require(event == 'push', 'only push events may publish')
    if apply:
        require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('SCANNER_PUBLISH_ENABLED') == 'true'
                and os.environ.get('GITHUB_REPOSITORY') == REPOSITORY and os.environ.get('GITHUB_EVENT_NAME') == event
                and os.environ.get('GITHUB_REF') == ref and os.environ.get('GITHUB_SHA') == sha, 'enabled push workflow required')
    actor, token = os.environ.get('GITHUB_ACTOR', ''), os.environ.get('GH_TOKEN', '')
    require(token and re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9-]{0,38}(?:\[bot\])?', actor), 'publisher credentials missing')
    with tempfile.TemporaryDirectory(prefix='scanner-publisher-') as directory:
        env = {'PATH': os.environ['PATH'], 'HOME': directory, 'GH_TOKEN': token,
               'GH_CONFIG_DIR': directory+'/gh', 'DOCKER_CONFIG': directory+'/docker'}
        command(['docker', 'login', 'ghcr.io', '--username', actor, '--password-stdin'], env, input_data=token.encode())
        require(api('repos/'+REPOSITORY, env)['visibility'] == plan['visibility'], 'repository visibility differs from approval')
        package_path = 'orgs/otama-homelab/packages/container/scanner-kiosk'
        package = api(package_path, env, missing=True)
        require((package is None and plan['visibility'] == 'private')
                or (package is not None and package['visibility'] == plan['visibility']), 'package visibility differs from approval')
        remote_source(ref, plan['source_sha'], env)
        require_current_checks(plan['source_sha'], env)
        current = policy.publication_plan(ref, sha, event, fresh_state(plan, env), repository)
        require(all(current[k] == plan[k] for k in ['source_sha', 'version', 'release_tag', 'image_tag', 'prerelease']), 'source/plan changed')
        result = {'published': False, 'source_sha': plan['source_sha'], 'manifest_sha256': plan['manifest_sha256'],
                  'release_tag': current['release_tag'], 'image_tag': current['image_tag'], 'aliases': current['aliases']}
        if not apply:
            return result
        release = api('repos/'+REPOSITORY+'/releases', env, body={'tag_name': current['release_tag'],
            'target_commitish': current['source_sha'], 'name': current['version'], 'draft': True,
            'prerelease': current['prerelease'], 'make_latest': 'false', 'body': body_text(plan)})
        require(type(release['id']) is int, 'invalid reserved release')
        release_path = 'repos/'+REPOSITORY+'/releases/'+str(release['id'])
        command(['gh', 'release', 'upload', current['release_tag'], *[str(candidate/name) for name in plan['uploads']],
                 '--repo', REPOSITORY], env, timeout=900)
        uploaded = pages_items(api(release_path+'/assets', env, paginate=True))
        actual = {a['name']: {'size': a['size'], 'digest': a['digest']} for a in uploaded}
        require(len(actual) == len(uploaded) and actual == assets, 'uploaded asset digest/size mismatch')
        remote_source(ref, plan['source_sha'], env)
        require_current_checks(plan['source_sha'], env)
        # Source assets must be accessible before a public registry image can appear.
        api(release_path, env, body={'draft': False, 'make_latest': 'false'})
        command(['docker', 'load', '--input', str(candidate/staging.NAMES['image'])], env, timeout=120)
        loaded = decode(command(['docker', 'image', 'inspect', plan['image_id']], env))
        require(len(loaded) == 1 and loaded[0]['Id'] == plan['image_id'], 'loaded image differs from candidate')
        image_ref = REGISTRY+':'+current['image_tag']
        command(['docker', 'tag', plan['image_id'], image_ref], env)
        require(registry_image(current['image_tag'], env) is None, 'immutable image appeared during upload')
        command(['docker', 'push', image_ref], env, timeout=600)
        image = registry_image(current['image_tag'], env)
        require(image is not None and image['image_id'] == plan['image_id'], 'pushed image identity mismatch')
        require(api(package_path, env)['visibility'] == plan['visibility'], 'pushed package visibility mismatch')
        remote_source(ref, plan['source_sha'], env)
        require_current_checks(plan['source_sha'], env)
        api(release_path, env, body={'body': body_text(plan, image['digest']), 'make_latest': 'false'})
        for alias in current['aliases']:
            command(['docker', 'tag', plan['image_id'], REGISTRY+':'+alias], env)
            command(['docker', 'push', REGISTRY+':'+alias], env, timeout=600)
            require(registry_image(alias, env) == image, 'alias digest differs from immutable image')
        if 'latest' in current['aliases']:
            api(release_path, env, body={'make_latest': 'true'})
        return {**result, 'published': True, 'image_digest': image['digest']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', type=Path, required=True)
    parser.add_argument('--plan', type=Path, required=True)
    parser.add_argument('--plan-sha256', required=True)
    parser.add_argument('--ref', required=True)
    parser.add_argument('--sha', required=True)
    parser.add_argument('--event', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    try:
        result = publish(args.candidate, args.plan, args.plan_sha256, args.ref, args.sha, args.event, apply=args.apply)
    except (ValueError, OSError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError):
        parser.exit(1, 'publication stopped; inspect any reserved draft/version before retrying; never overwrite it\n')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
