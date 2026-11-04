#!/usr/bin/env python3
"""Inventory verified native selected-file origins; never fetch or approve releases."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat

spec = importlib.util.spec_from_file_location('assemble_native', Path(__file__).with_name('assemble-native.py'))
assembler = importlib.util.module_from_spec(spec)
spec.loader.exec_module(assembler)


def verify_native(root, files):
    try:
        for name, entry in sorted(files.items()):
            with assembler.source_parent(root, name) as (parent, leaf):
                if entry.get('link'):
                    if os.readlink(leaf, dir_fd=parent) != entry['link']:
                        raise ValueError('native link changed')
                    continue
                descriptor = os.open(leaf, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
                with os.fdopen(descriptor, 'rb') as stream:
                    before = os.fstat(stream.fileno())
                    if (not stat.S_ISREG(before.st_mode) or before.st_size != entry['size']
                            or stat.S_IMODE(before.st_mode) != entry['mode']):
                        raise ValueError('native file type, size or mode changed')
                    digest, size = hashlib.sha256(), 0
                    while chunk := stream.read(min(1 << 20, entry['size'] - size + 1)):
                        size += len(chunk)
                        if size > entry['size']:
                            raise ValueError('native file grew')
                        digest.update(chunk)
                    after = os.fstat(stream.fileno())
                    if (size != entry['size'] or digest.hexdigest() != entry['sha256']
                            or before.st_mtime_ns != after.st_mtime_ns
                            or before.st_size != after.st_size):
                        raise ValueError('native file content changed')
    except OSError as error:
        raise ValueError('native input unavailable or unsafe') from error


def catalog(manifest, provenance, native_root, output):
    if output.exists() or output.is_symlink():
        raise FileExistsError('SBOM output already exists')
    root = native_root.resolve(strict=True)
    if output.resolve().is_relative_to(root):
        raise ValueError('SBOM output must be separate from native inputs')
    raw_origin = provenance.read_bytes()
    origin = json.loads(raw_origin)
    files, _ = assembler.plan(manifest, origin)
    verify_native(root, files)
    components = []

    def add(name, version, role, names, purl=None, metadata=None):
        ref = 'native-' + hashlib.sha256((role + ':' + name).encode()).hexdigest()
        properties = {'scanner:origin': role,
                      'scanner:retained-files': {n: files[n] for n in sorted(names)}}
        if metadata:
            properties['scanner:origin-metadata'] = metadata
        component = {'bom-ref': ref, 'type': 'library', 'name': name,
                     'properties': [{'name': k, 'value': v if isinstance(v, str)
                                     else json.dumps(v, sort_keys=True, separators=(',', ':'))}
                                    for k, v in properties.items()]}
        if version:
            component['version'] = version
        if purl:
            component['purl'] = purl
        components.append(component)

    for name, item in sorted(origin['apk_source_components'].items()):
        add(name, item['version'], 'selected-apk-files', item['retained_files'],
            item['purl'], {k: v for k, v in item.items() if k != 'retained_files'})
    for role, items in [('source-built', origin['upstream_source_components']),
                        ('first-party', origin['firstparty_components'])]:
        for item in sorted(items, key=lambda item: item['name']):
            add(item['name'], item.get('version'), role, item['files'],
                metadata={k: v for k, v in item.items() if k != 'files'})
    refs = [c['bom-ref'] for c in components]
    if len(refs) != len(set(refs)):
        raise ValueError('duplicate native component identity')
    notes = {n: files[n] for n in origin['notice_files']}
    scope = ('Native selected-file origins only; not an installed APK database or ELF dependency graph. '
             'Combine with separate image and compiled Go inventories. No vulnerability/license approval.')
    receipt = {'native_manifest_sha256': origin['runtime_manifest_sha256'],
               'provenance_sha256': hashlib.sha256(raw_origin).hexdigest(),
               'native_entries': len(files), 'components': len(components),
               'code_and_data_files': len(files) - len(notes), 'notice_files': len(notes),
               'publication_approved': False, 'vulnerability_gate_passed': False,
               'license_gate_passed': False, 'scope': scope}
    bom = {'$schema': 'http://cyclonedx.org/schema/bom-1.6.schema.json',
           'bomFormat': 'CycloneDX', 'specVersion': '1.6', 'version': 1,
           'metadata': {'component': {'bom-ref': 'native-runtime', 'type': 'application',
                                     'name': 'scanner-kiosk-native-origin-supplement'},
                        'properties': [{'name': 'scanner:' + k, 'value': str(v)}
                                       for k, v in receipt.items()]},
           'components': components,
           'dependencies': [{'ref': 'native-runtime', 'dependsOn': refs}]}
    output.mkdir(mode=0o700)
    try:
        for name, value in [('native-origin.cdx.json', bom),
                            ('native-origin-inventory.json', {'components': components,
                                                             'notice_files': notes, 'receipt': receipt}),
                            ('receipt.json', receipt)]:
            (output/name).write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')
    except Exception:
        shutil.rmtree(output)
        raise
    return receipt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['native-manifest', 'provenance', 'native-root', 'output-dir']:
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(catalog(args.native_manifest, args.provenance, args.native_root, args.output_dir)))
