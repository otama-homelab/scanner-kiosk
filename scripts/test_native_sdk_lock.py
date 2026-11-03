import base64
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1] / 'native'


def sums(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (\S+)', line)
        if not match or match[2] in result:
            raise ValueError('invalid or duplicate checksum entry')
        result[match[2]] = match[1]
    return result


class NativeSDKLockTests(unittest.TestCase):
    def setUp(self):
        self.lock = json.loads((ROOT / 'tesseract-sdk.lock.json').read_text())

    def test_package_archive_identity_and_checksum_list(self):
        packages = self.lock['packages']
        self.assertEqual(len(packages), 174)
        self.assertEqual(len({p['name'] for p in packages}), 174)
        for p in packages:
            with self.subTest(package=p['name']):
                self.assertRegex(p['name'], r'^[a-z0-9][a-z0-9+_.-]{0,127}$')
                self.assertRegex(p['version'], r'^[A-Za-z0-9][A-Za-z0-9._+~]*-r[0-9]+$')
                self.assertEqual(p['filename'], p['name'] + '-' + p['version'] + '.apk')
                self.assertIn(p['archive_architecture'], ['x86_64', 'noarch'])
                self.assertEqual(p['repository_architecture'], 'x86_64')
                self.assertIn(p['repository'], ['main', 'community'])
                self.assertGreater(p['bytes'], 0)
                self.assertLess(p['bytes'], 128 * 1024**2)
                self.assertRegex(p['sha256'], r'^[0-9a-f]{64}$')
                self.assertTrue(p['apk_control_checksum'].startswith('Q1'))
                self.assertEqual(len(base64.b64decode(p['apk_control_checksum'][2:], validate=True)), 20)
        self.assertLess(sum(p['bytes'] for p in packages), 512 * 1024**2)
        self.assertEqual(sums(ROOT / 'tesseract-sdk.SHA256SUMS'), {p['filename']: p['sha256'] for p in packages})

    def test_trusted_key_list_matches_lock(self):
        keys = self.lock['trusted_keys_sha256']
        self.assertEqual(len(keys), 3)
        for name in keys:
            self.assertEqual(Path(name).name, name)
            self.assertTrue(name.endswith('.pub'))
        self.assertEqual(sums(ROOT / 'alpine-sdk-keys.SHA256SUMS'),
                         {'/etc/apk/keys/' + name: sha for name, sha in keys.items()})

    def test_sdk_recipe_requires_offline_pinned_inputs(self):
        recipe = (ROOT / 'Dockerfile.tesseract-sdk').read_text()
        self.assertIn('FROM ' + self.lock['base_image'], recipe)
        self.assertIn('RUN --network=none', recipe)
        self.assertIn('sha256sum -c /inputs/tesseract-sdk.SHA256SUMS', recipe)
        self.assertIn('sha256sum -c /inputs/alpine-sdk-keys.SHA256SUMS', recipe)
        self.assertIn('apk --no-network add /inputs/apks/*.apk', recipe)
        self.assertNotIn('--allow-untrusted', recipe)
        self.assertNotIn('curl ', recipe)
        self.assertNotIn('wget ', recipe)

    def test_repository_snapshots_are_pinned(self):
        self.assertEqual(self.lock['platform'], 'linux/amd64')
        self.assertRegex(self.lock['base_image'], r'^alpine:3\.24\.3@sha256:[0-9a-f]{64}$')
        self.assertEqual(set(self.lock['index_sha256']), {'main', 'community'})
        for sha in self.lock['index_sha256'].values():
            self.assertRegex(sha, r'^[0-9a-f]{64}$')
        self.assertEqual(self.lock['repositories'], [
            'https://dl-cdn.alpinelinux.org/alpine/v3.24/main',
            'https://dl-cdn.alpinelinux.org/alpine/v3.24/community'])


if __name__ == '__main__':
    unittest.main()
