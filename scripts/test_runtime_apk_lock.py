import base64
import hashlib
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1] / 'native'


class RuntimeAPKLockTests(unittest.TestCase):
    def setUp(self):
        self.lock = json.loads((ROOT/'runtime-apks.lock.json').read_text())

    def test_every_apk_origin_has_exactly_one_matching_archive(self):
        raw = (ROOT/'runtime-provenance.json').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), self.lock['runtime_provenance_sha256'])
        origins = json.loads(raw)['apk_source_components']
        packages = self.lock['packages']
        self.assertEqual(len(packages), len(origins))
        self.assertEqual({p['name'] for p in packages}, set(origins))
        for package in packages:
            metadata = origins[package['name']]['source_metadata']
            self.assertEqual(package['version'], metadata['version'])
            self.assertEqual(package['origin'], metadata['originPackage'])
            self.assertEqual(package['source_commit'], metadata['gitCommitOfApkPort'])

    def test_archive_identity_and_checksum_list(self):
        packages = self.lock['packages']
        checksums = {}
        for line in (ROOT/'runtime-apks.SHA256SUMS').read_text().splitlines():
            digest, name = line.split('  ')
            self.assertNotIn(name, checksums)
            checksums[name] = digest
        self.assertEqual(checksums, {p['filename']: p['sha256'] for p in packages})
        for package in packages:
            self.assertRegex(package['name'], r'^[a-z0-9][a-z0-9+_.-]{0,127}$')
            self.assertRegex(package['version'], r'^[A-Za-z0-9][A-Za-z0-9._+~]*-r[0-9]+$')
            self.assertEqual(package['filename'], package['name']+'-'+package['version']+'.apk')
            self.assertRegex(package['sha256'], r'^[0-9a-f]{64}$')
            self.assertRegex(package['source_commit'], r'^[0-9a-f]{40}$')
            self.assertIn(package['repository'], ['main', 'community'])
            self.assertIn(package['archive_architecture'], ['x86_64', 'noarch'])
            self.assertEqual(package['repository_architecture'], 'x86_64')
            self.assertGreater(package['bytes'], 0)
            self.assertLess(package['bytes'], 128*1024**2)
            self.assertTrue(package['apk_control_checksum'].startswith('Q1'))
            self.assertEqual(len(base64.b64decode(package['apk_control_checksum'][2:], validate=True)), 20)
        self.assertLess(sum(p['bytes'] for p in packages), 256*1024**2)

    def test_only_official_repositories_and_existing_trusted_keys(self):
        self.assertEqual(self.lock['repositories'], [
            'https://dl-cdn.alpinelinux.org/alpine/v3.24/main',
            'https://dl-cdn.alpinelinux.org/alpine/v3.24/community'])
        sdk = json.loads((ROOT/'tesseract-sdk.lock.json').read_text())
        self.assertEqual(self.lock['trusted_keys_sha256'], sdk['trusted_keys_sha256'])


if __name__ == '__main__':
    unittest.main()
