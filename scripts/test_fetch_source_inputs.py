import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


class FetchSourceInputsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.output = self.root/'sources'
        self.lock = self.root/'lock.json'
        self.entry = {'path': 'upstream/apk/fixture.tar.gz', 'role': 'apk-upstream-source',
                      'url': 'https://distfiles.alpinelinux.org/distfiles/v3.24/fixture.tar.gz',
                      'bytes': 4, 'sha256': hashlib.sha256(b'data').hexdigest()}

    def fetch(self):
        self.lock.write_text(json.dumps({'format': 1, 'files': [self.entry]}))
        spec = importlib.util.spec_from_file_location('fetch_sources', Path(__file__).with_name('fetch-source-inputs.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with patch.object(module.urllib.request, 'build_opener') as factory:
            factory.return_value.open.return_value = io.BytesIO(b'data')
            result = module.fetch(self.lock, self.output)
            self.requested = factory.return_value.open.call_count
            return result

    def test_downloads_only_verified_bytes_and_finishes_receipt(self):
        result = self.fetch()
        self.assertTrue(result['complete'])
        self.assertEqual((self.output/self.entry['path']).read_bytes(), b'data')
        self.assertFalse(result['publication_approved'])

    def test_wrong_hash_leaves_no_final_source_and_failed_receipt(self):
        self.entry['sha256'] = '0'*64
        with self.assertRaises(ValueError):
            self.fetch()
        self.assertFalse((self.output/self.entry['path']).exists())
        self.assertFalse(json.loads((self.output/'receipt.json').read_text())['complete'])

    def test_path_or_url_escape_is_rejected_before_download(self):
        for key, value in [('path', '../escape'), ('url', 'https://example.test/source.tar')]:
            original = self.entry[key]
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.entry[key] = value
                self.fetch()
            self.entry[key] = original
            self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
