import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('fetch_tesseract', Path(__file__).with_name('fetch-tesseract-source.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class TesseractSourceFetchTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.data = b'fixed upstream archive'
        self.lock = json.loads((Path(__file__).resolve().parents[1]/'native/tesseract-source.lock.json').read_text())
        self.lock.update(bytes=len(self.data), sha256=hashlib.sha256(self.data).hexdigest())
        self.path, self.output = self.root/'lock.json', self.root/'source.tar.gz'
        self.path.write_text(json.dumps(self.lock))

    def fetch(self, data=None):
        with patch.object(module.urllib.request, 'build_opener') as opener:
            opener.return_value.open.return_value = io.BytesIO(self.data if data is None else data)
            result = module.fetch(self.path, self.output)
            self.assertEqual(opener.return_value.open.call_args.args[0].full_url, self.lock['url'])
            return result

    def test_exact_source_is_verified(self):
        result = self.fetch()
        self.assertEqual(self.output.read_bytes(), self.data)
        self.assertEqual(result['sha256'], self.lock['sha256'])

    def test_mismatch_leaves_no_source(self):
        for data in [b'short', self.data+b'extra', b'x'*len(self.data)]:
            with self.subTest(data=data), self.assertRaises(ValueError):
                self.fetch(data)
            self.assertFalse(self.output.exists())

    def test_untrusted_url_is_rejected_before_network(self):
        for url in ['http://codeload.github.com/unsafe', 'https://127.0.0.1/source',
                    self.lock['url']+'?token=unsafe']:
            lock = self.lock|{'url':url}
            self.path.write_text(json.dumps(lock))
            with patch.object(module.urllib.request, 'build_opener') as opener:
                with self.assertRaises(ValueError):
                    module.fetch(self.path, self.output)
                opener.assert_not_called()

    def test_existing_output_is_not_overwritten(self):
        self.output.write_bytes(b'keep')
        with self.assertRaises(FileExistsError):
            self.fetch()
        self.assertEqual(self.output.read_bytes(), b'keep')


if __name__ == '__main__':
    unittest.main()
