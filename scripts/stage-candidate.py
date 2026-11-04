#!/usr/bin/env python3
"""Stage hash-bound candidate materials offline; never approve or publish them.

Run with read-only inputs in a bounded OS sandbox. Existing build/inventory and
source-review receipts are cross-checked; this does not repeat source builds,
archive semantic review, exploitability review or physical acceptance.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tarfile

NAMES = {
    'binary': 'scanner-onefile-linux-amd64', 'image': 'scanner-image.tar',
    'source': 'scanner-source.tar', 'native-source': 'scanner-native-sources.tar',
    'base-source': 'scanner-base-sources.tar', 'license': 'LICENSE',
    'notices': 'THIRD-PARTY-NOTICES.md', 'deployment': 'scanner-deployment.tar',
    'changelog': 'CHANGELOG.md', 'security-reports': 'security-reports.tar',
    'native-manifest': 'native-manifest.json', 'native-provenance': 'native-provenance.json',
    'build': 'build.json', 'packed-manifest': 'packed-manifest.json',
    'application': 'application.json', 'image-config': 'image.json',
    'onefile-sbom': 'onefile.syft.json', 'native-sbom': 'native-origin.cdx.json',
    'image-sbom': 'image.syft.json', 'onefile-inventory': 'onefile-inventory.json',
    'image-inventory': 'image-inventory.json', 'security': 'security-review.json',
    'source-bindings': 'source-bindings.json', 'native-source-index': 'native-source-index.json',
    'base-inventory': 'base-inventory.json',
    'deployment-inventory': 'deployment-inventory.json',
    'application-inputs': 'scanner-application-sources.tar',
    'frontend-inputs': 'scanner-frontend-source-inputs.tar',
}
OPTIONAL = {'application-inputs', 'frontend-inputs'}
MAX_ASSET, MAX_TOTAL = 1 << 30, 2 << 30


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def read_json(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= 8 << 20, 'expected bounded JSON file')
    return json.loads(path.read_bytes(), object_pairs_hook=unique_object)


def copy_checked(root, record, destination):
    relative = record['path']
    require(isinstance(relative, str) and relative and '\\' not in relative
            and not PurePosixPath(relative).is_absolute()
            and all(p not in ['', '.', '..'] for p in relative.split('/')), 'invalid input path')
    path = root
    for part in PurePosixPath(relative).parts[:-1]:
        path /= part
        require(stat.S_ISDIR(path.lstat().st_mode), 'input parent must be a real directory')
    path /= PurePosixPath(relative).name
    require(stat.S_ISREG(path.lstat().st_mode), 'input must be a regular file')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as source, destination.open('xb') as output:
        info = os.fstat(source.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_size == record['bytes'], 'input size/type mismatch')
        digest, size = hashlib.sha256(), 0
        while chunk := source.read(1 << 20):
            size += len(chunk)
            require(size <= record['bytes'], 'input grew during staging')
            digest.update(chunk)
            output.write(chunk)
        after = os.fstat(source.fileno())
        require(size == record['bytes'] and digest.hexdigest() == record['sha256'], 'input hash mismatch')
        require((info.st_size, info.st_mtime_ns) == (after.st_size, after.st_mtime_ns), 'input changed during staging')
    destination.chmod(0o444)


def verify_raw_reports(path, scans):
    expected = {}
    for scope, scan in scans.items():
        artifacts = scan['artifacts_sha256']
        require(isinstance(artifacts, dict) and 'grype.json' in artifacts, 'missing raw advisory report')
        for name, digest in artifacts.items():
            require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', name)
                    and isinstance(digest, str) and re.fullmatch(r'[0-9a-f]{64}', digest), 'invalid raw report record')
            expected[scope+'/'+name] = digest
    require(len(expected) <= 256, 'too many report members')
    seen, total = set(), 0
    with tarfile.open(path, 'r:') as archive:
        for member in archive:
            require(member.name in expected and member.name not in seen and member.isfile()
                    and not member.issparse() and member.size <= 16 << 20, 'unexpected raw report member')
            seen.add(member.name)
            total += member.size
            require(total <= 64 << 20, 'raw reports exceed size bound')
            with archive.extractfile(member) as stream:
                require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected[member.name], 'raw report hash mismatch')
    require(seen == set(expected), 'missing raw report member')


def bind_materials(root, records, source_sha):
    def data(role):
        return read_json(root/NAMES[role])
    def sha(role):
        return records[role]['sha256']
    build, app, packed = data('build'), data('application'), data('packed-manifest')
    native, provenance = data('native-manifest'), data('native-provenance')
    inventory, image_inventory = data('onefile-inventory'), data('image-inventory')
    image, sources, base = data('image-config'), data('source-bindings'), data('base-inventory')
    source_index, security = data('native-source-index'), data('security')
    require(app['passed'] is True and app['source_revision'] == source_sha, 'application source mismatch')
    version = app['version']
    require(version['os'] == 'linux' and version['architecture'] == 'amd64', 'unsupported target')
    require(build['binary_sha256'] == sha('binary') and build['binary_bytes'] == records['binary']['bytes'], 'binary build mismatch')
    require(build['project_license_sha256'] == sha('license'), 'project license mismatch')
    require(build['native_manifest_sha256'] == sha('native-manifest')
            and build['native_entries'] == len(native['files']), 'native build mismatch')
    require(provenance['runtime_manifest_sha256'] == sha('native-manifest'), 'native provenance mismatch')
    require(packed['files']['app/scanner']['sha256'] == build['app_sha256']
            and {k: v for k, v in packed['files'].items() if k != 'app/scanner'} == native['files'], 'packed payload mismatch')
    require(inventory['binary_sha256'] == sha('binary') and inventory['binary_bytes'] == records['binary']['bytes']
            and inventory['version'] == version and inventory['build_receipt_sha256'] == sha('build')
            and inventory['packed_manifest_sha256'] == sha('packed-manifest'), 'onefile inventory mismatch')
    require(inventory['native']['native_manifest_sha256'] == sha('native-manifest')
            and inventory['native']['provenance_sha256'] == sha('native-provenance'), 'native inventory mismatch')
    require(inventory['artifacts_sha256']['onefile.syft.json'] == sha('onefile-sbom')
            and inventory['artifacts_sha256']['native/native-origin.cdx.json'] == sha('native-sbom'), 'onefile SBOM mismatch')
    require(len(image) == 1 and re.fullmatch(r'sha256:[0-9a-f]{64}', image[0]['Id']), 'invalid image identity')
    image_id = image[0]['Id']
    config = image[0]['Config']
    require(config['Entrypoint'] == ['/usr/local/bin/scanner'] and config['Cmd'] == ['serve']
            and config['User'] == '10001:10001', 'image runtime contract mismatch')
    require(image_inventory['passed'] is True and image_inventory['all_layer_diff_ids_verified'] is True
            and image_inventory['image_id'] == image_id and image_inventory['image_archive_sha256'] == sha('image'), 'image archive/inventory mismatch')
    require(sources['application_source_commit'] == source_sha and sources['candidate_sha256'] == sha('binary')
            and sources['api_sha256'] == build['app_sha256'] and sources['image_id'] == image_id
            and sources['version'] == version and sources['assets'] == app['assets']
            and sources['native_manifest_sha256'] == sha('native-manifest')
            and sources['native_provenance_sha256'] == sha('native-provenance'), 'source bindings mismatch')
    for scope, role in [('native', 'native-source'), ('base', 'base-source'),
                        ('application', 'application-inputs'), ('frontend', 'frontend-inputs')]:
        if role in records:
            record = sources['archives'][scope]
            require(record['sha256'] == sha(role) and record['bytes'] == records[role]['bytes'], 'source archive mismatch: '+scope)
    require(source_index['application_source_commit'] == source_sha
            and source_index['candidate_sha256'] == sha('binary')
            and source_index['native_manifest_sha256'] == sha('native-manifest'), 'native source identity mismatch')
    project = [r for r in source_index['files'] if r['archive'] == 'build-controls/scanner-source.tar']
    require(len(project) == 1 and project[0]['sha256'] == sha('source')
            and project[0]['bytes'] == records['source']['bytes'], 'project source archive mismatch')
    require(base['passed'] is True and base['image_id'] == image_id
            and base['archive_sha256'] == sha('base-source'), 'base source mismatch')
    deployment = data('deployment-inventory')
    require(deployment['passed'] is True and deployment['source_archive_sha256'] == sha('source')
            and deployment['archive_sha256'] == sha('deployment'), 'deployment source mismatch')
    scans = security['scans']
    require(set(scans) == {'image', 'onefile', 'native', 'native-cpe'}, 'missing advisory scan record')
    for key, role in [('image', 'image-sbom'), ('onefile', 'onefile-sbom'),
                      ('native', 'native-sbom'), ('native-cpe', 'native-sbom')]:
        scan = scans[key]
        require(scan['input_sha256'] == sha(role) and type(scan['scan_completed']) is bool,
                'advisory scan input/status mismatch')
    verify_raw_reports(root/NAMES['security-reports'], scans)
    image_sbom = data('image-sbom')
    require(image_sbom['source']['type'] == 'image' and image_sbom['source']['metadata']['imageID'] == image_id,
            'image SBOM identity mismatch')
    require(data('onefile-sbom')['artifacts'] and image_sbom['artifacts']
            and data('native-sbom')['bomFormat'] == 'CycloneDX' and data('native-sbom')['components'], 'empty/invalid SBOM')
    return {'source_sha': source_sha, 'version': version, 'image_id': image_id,
            'binary_sha256': sha('binary'), 'api_sha256': build['app_sha256'],
            'native_manifest_sha256': sha('native-manifest'), 'assets': app['assets'],
            'scans_completed': all(s['scan_completed'] for s in scans.values()),
            'advisory_threshold_passed': all(s['scan_completed'] and type(s.get('exit_code')) is int
                                           and s['exit_code'] == 0 for s in scans.values())}


def stage(inputs, inventory_path, output):
    inputs, output = Path(inputs), Path(output)
    require(stat.S_ISDIR(inputs.lstat().st_mode), 'input root must be a real directory')
    if output.exists() or output.is_symlink():
        raise FileExistsError('output already exists')
    spec = read_json(Path(inventory_path))
    require(spec['format'] == 1 and isinstance(spec['source_sha'], str)
            and re.fullmatch(r'(?:[0-9a-f]{40}|[0-9a-f]{64})', spec['source_sha']), 'invalid source identity')
    records = spec['files']
    require(isinstance(records, dict) and set(NAMES)-OPTIONAL <= set(records) <= set(NAMES), 'missing or unknown material role')
    total = 0
    for role, record in records.items():
        require(isinstance(record, dict) and set(record) == {'path', 'bytes', 'sha256'}
                and type(record['bytes']) is int and 0 < record['bytes'] <= MAX_ASSET
                and isinstance(record['sha256'], str) and re.fullmatch(r'[0-9a-f]{64}', record['sha256']), 'invalid material record: '+role)
        total += record['bytes']
    require(total <= MAX_TOTAL, 'candidate exceeds material size bound')
    # mkdir reserves the output name atomically; a failed stage never gets a receipt.
    output.mkdir(mode=0o700)
    try:
        for role, record in records.items():
            copy_checked(inputs, record, output/NAMES[role])
        try:
            identity = bind_materials(output, records, spec['source_sha'])
        except (KeyError, TypeError, IndexError, AttributeError, tarfile.TarError) as error:
            raise ValueError('malformed material metadata') from error
        manifest = {'format': 1, 'candidate_id': 'sha256:'+identity['binary_sha256'], **identity,
                    'publication_approved': False,
                    'files': {role: {'file': NAMES[role], 'bytes': r['bytes'], 'sha256': r['sha256']}
                              for role, r in sorted(records.items())}}
        raw = (json.dumps(manifest, indent=2, sort_keys=True)+'\n').encode()
        (output/'manifest.json').write_bytes(raw)
        digest = hashlib.sha256(raw).hexdigest()
        checksums = [digest+'  manifest.json']+[r['sha256']+'  '+NAMES[role] for role, r in sorted(records.items())]
        (output/'SHA256SUMS').write_text('\n'.join(checksums)+'\n')
        (output/'manifest.json').chmod(0o444)
        (output/'SHA256SUMS').chmod(0o444)
        receipt = {'passed': True, 'manifest_sha256': digest, 'candidate_id': manifest['candidate_id'],
                   'publication_approved': False, 'external_writes': False}
        (output/'receipt.json').write_text(json.dumps(receipt, indent=2)+'\n')
        (output/'receipt.json').chmod(0o444)
        return receipt
    except BaseException:
        shutil.rmtree(output)
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--inputs', type=Path, required=True)
    parser.add_argument('--inventory', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        result = stage(args.inputs, args.inventory, args.output)
    except (ValueError, OSError, KeyError, TypeError):
        parser.exit(1, 'candidate stage rejected: missing, changed or inconsistent materials\n')
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()
