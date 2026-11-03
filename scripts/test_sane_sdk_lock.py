import json
from pathlib import Path
import unittest

from test_native_sdk_lock import sums

ROOT = Path(__file__).resolve().parents[1] / 'native'


class SaneSDKLockTests(unittest.TestCase):
    def setUp(self):
        self.lock = json.loads((ROOT / 'sane-sdk.lock.json').read_text())

    def test_complete_archive_set_and_original_additions(self):
        packages = self.lock['packages']
        self.assertEqual(len(packages), 46)
        self.assertEqual(len({p['name'] for p in packages}), 46)
        self.assertEqual(sum(p['base_input'] for p in packages), 17)
        self.assertEqual(sums(ROOT / 'sane-sdk.SHA256SUMS'),
                         {p['filename']: p['sha256'] for p in packages})
        for p in packages:
            self.assertEqual(p['filename'], p['name'] + '-' + p['version'] + '.apk')
            self.assertRegex(p['filename'], r'^[a-z0-9][a-z0-9+_.~-]+\.apk$')
            self.assertRegex(p['sha256'], r'^[0-9a-f]{64}$')
            self.assertIn(p['repository'], ['main', 'community'])
            self.assertGreater(p['bytes'], 0)
        versions = {p['name']: p['version'] for p in packages}
        self.assertEqual(versions['sane-dev'], '1.4.0-r4')
        self.assertEqual(versions['sane-backend-test'], '1.4.0-r4')

    def test_repository_and_key_identity_shared_with_tesseract(self):
        other = json.loads((ROOT / 'tesseract-sdk.lock.json').read_text())
        for key in ['repositories', 'index_sha256', 'trusted_keys_sha256']:
            self.assertEqual(self.lock[key], other[key])

    def test_offline_recipe_verifies_inputs_and_signatures(self):
        recipe = (ROOT / 'Dockerfile.sane-sdk').read_text()
        self.assertEqual(self.lock['platform'], 'linux/amd64')
        self.assertRegex(self.lock['base_image'], r'^golang:1\.27\.3-alpine3\.24@sha256:[0-9a-f]{64}$')
        self.assertIn('FROM ' + self.lock['base_image'], recipe)
        for required in ['RUN --network=none',
                         'sha256sum -c /inputs/sane-sdk.SHA256SUMS',
                         'sha256sum -c /inputs/alpine-sdk-keys.SHA256SUMS',
                         'apk --no-network verify /inputs/apks/*.apk',
                         'apk --no-network add /inputs/apks/*.apk']:
            self.assertIn(required, recipe)
        self.assertNotIn('--allow-untrusted', recipe)
        self.assertNotIn('curl ', recipe)
        self.assertNotIn('wget ', recipe)


if __name__ == '__main__':
    unittest.main()
