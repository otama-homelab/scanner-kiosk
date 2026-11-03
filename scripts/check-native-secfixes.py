#!/usr/bin/env python3
"""Compare fixed native APK origins with supplied official Alpine secfixes data.

Offline evidence only: secfixes is not a complete database of unfixed CVEs.
"""
import argparse
from functools import lru_cache
import hashlib
import json
from pathlib import Path
import subprocess


@lru_cache(None)
def compare(apk, current, fixed):
    result = subprocess.run([apk, '--no-network', 'version', '-t', current, fixed],
                            env={'PATH': '/usr/bin:/bin', 'HOME': '/tmp'},
                            capture_output=True, text=True, check=True, timeout=5)
    relation = result.stdout.strip()
    if relation not in ['<', '=', '>']:
        raise ValueError('invalid apk version comparison')
    return relation


def evaluate(apk, current, secfixes):
    comparisons, addressed, newer = [], set(), {}
    for fixed, ids in secfixes.items():
        if not isinstance(fixed, str) or not fixed or not isinstance(ids, list) or not all(isinstance(i, str) and i for i in ids):
            raise ValueError('invalid secfixes record')
        relation = compare(apk, current, fixed)
        comparisons.append({'fixed': fixed, 'relation': relation, 'advisories': ids})
        if relation in ['=', '>']:
            addressed.update(ids)
        else:
            for advisory in ids:
                newer.setdefault(advisory, []).append(fixed)
    return {'comparisons': comparisons, 'addressed_advisories': sorted(addressed),
            'newer_fix_records': {key: value for key, value in newer.items() if key not in addressed}}


def inspect(apk, manifest_path, provenance_path, databases):
    manifest_raw, provenance_raw = manifest_path.read_bytes(), provenance_path.read_bytes()
    manifest, provenance = json.loads(manifest_raw), json.loads(provenance_raw)
    if provenance['runtime_manifest_sha256'] != hashlib.sha256(manifest_raw).hexdigest():
        raise ValueError('provenance does not match native manifest')
    indexes, hashes = {}, {}
    for repo, path in databases.items():
        raw = path.read_bytes()
        data = json.loads(raw)
        if data.get('distroversion') != 'v3.24' or data.get('reponame') != repo:
            raise ValueError('unexpected secdb release or repository')
        packages = {}
        for record in data['packages']:
            package = record['pkg']
            if package['name'] in packages:
                raise ValueError('duplicate secdb origin')
            packages[package['name']] = package
        indexes[repo] = packages
        hashes[repo] = hashlib.sha256(raw).hexdigest()
    matches, unlisted = [], []
    for name, component in provenance['apk_source_components'].items():
        origin = component['source_metadata']['originPackage']
        for path in component['retained_files']:
            if path not in manifest['files']:
                raise ValueError('component references an absent native file')
        records = [(repo, packages[origin]) for repo, packages in indexes.items() if origin in packages]
        if not records:
            unlisted.append({'package': name, 'origin': origin, 'version': component['version']})
        for repo, record in records:
            matches.append({'package': name, 'origin': origin, 'version': component['version'],
                            'repository': repo, **evaluate(apk, component['version'], record['secfixes'])})
    pending = sum(len(m['newer_fix_records']) for m in matches)
    return {'comparison_completed': True, 'release_gate_passed': False,
            'scope': 'Alpine v3.24 fixed-version records only; not unfixed CVE coverage, Go, distroless OS, custom Tesseract or native TLS acceptance',
            'native_manifest_sha256': hashlib.sha256(manifest_raw).hexdigest(),
            'provenance_sha256': hashlib.sha256(provenance_raw).hexdigest(),
            'secdb_sha256': hashes, 'results': matches, 'not_listed_components': unlisted,
            'newer_fix_record_count': pending,
            'upstream_source_components': provenance['upstream_source_components'],
            'limitations': ['Missing secfixes entries do not prove safety',
                            'Source-built Tesseract is not compared as an invented APK version',
                            'No finding is suppressed based on exploitability or severity']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apk', required=True)
    parser.add_argument('--native-manifest', type=Path, required=True)
    parser.add_argument('--provenance', type=Path, required=True)
    parser.add_argument('--main-secdb', type=Path, required=True)
    parser.add_argument('--community-secdb', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    report = inspect(args.apk, args.native_manifest, args.provenance,
                     {'main': args.main_secdb, 'community': args.community_secdb})
    # Exclusive creation preserves any existing report, including a failed gate.
    with args.output.open('x') as stream:
        json.dump(report, stream, indent=2)
        stream.write('\n')
    print(json.dumps({'comparison_completed': True, 'release_gate_passed': False,
                      'newer_fix_record_count': report['newer_fix_record_count'],
                      'not_listed_components': len(report['not_listed_components'])}))
    return 2 if report['newer_fix_record_count'] else 0


if __name__ == '__main__':
    raise SystemExit(main())
