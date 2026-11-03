import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('collect_aports', Path(__file__).with_name('collect-aports-sources.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

APKBUILD = b'pkgname=fixture\n# This source is data, never executed.\n'
PATCH = b'--- a/file\n+++ b/file\n'
COMMIT = '1' * 40


def blob(data):
    return hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()


def entry(path, data):
    return {'name': path.rsplit('/', 1)[-1], 'path': path, 'type': 'file',
            'size': len(data), 'sha': blob(data), 'download_url': 'https://untrusted.invalid/do-not-use'}


def receipt():
    return {'recipes': [{'origin': 'fixture', 'repository': 'main', 'commit': COMMIT,
                          'collected': True, 'sha256': hashlib.sha256(APKBUILD).hexdigest()}]}


class AportsSourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.output = self.root / 'output'
        prefix = 'main/fixture'
        api = 'https://api.github.com/repos/alpinelinux/aports/contents/'
        raw = 'https://raw.githubusercontent.com/alpinelinux/aports/' + COMMIT + '/'
        self.responses = {
            api + prefix + '?ref=' + COMMIT: json.dumps([
                entry(prefix + '/APKBUILD', APKBUILD),
                {'name': 'patches', 'path': prefix + '/patches', 'type': 'dir', 'sha': '2' * 40}]).encode(),
            api + prefix + '/patches?ref=' + COMMIT: json.dumps([entry(prefix + '/patches/fix.patch', PATCH)]).encode(),
            raw + prefix + '/APKBUILD': APKBUILD,
            raw + prefix + '/patches/fix.patch': PATCH,
        }

    def collect(self, plan=None):
        with contextlib.redirect_stdout(io.StringIO()), patch.object(module, 'fetch', side_effect=lambda url, limit: self.responses[url]) as fetch:
            result = module.collect(plan or receipt(), self.output)
        return result, fetch

    def test_complete_recipe_and_nested_patch(self):
        result, fetch = self.collect()
        self.assertTrue(result['complete'])
        self.assertEqual(len(result['files']), 2)
        self.assertEqual((self.output / COMMIT / 'main/fixture/patches/fix.patch').read_bytes(), PATCH)
        self.assertTrue(all('untrusted.invalid' not in call.args[0] for call in fetch.call_args_list))
        self.assertFalse((self.root / 'file').exists())

    def test_corrupt_blob_is_not_saved(self):
        key = 'https://raw.githubusercontent.com/alpinelinux/aports/' + COMMIT + '/main/fixture/patches/fix.patch'
        self.responses[key] = b'bad source'
        with self.assertRaises(ValueError):
            self.collect()
        self.assertFalse((self.output / COMMIT / 'main/fixture/patches/fix.patch').exists())
        self.assertFalse(json.loads((self.output / 'recipe-trees.json').read_text())['complete'])

    def test_changed_recorded_apkbuild_is_rejected(self):
        plan = receipt()
        plan['recipes'][0]['sha256'] = '0' * 64
        with self.assertRaisesRegex(ValueError, 'APKBUILD checksum'):
            self.collect(plan)
        self.assertFalse((self.output / COMMIT / 'main/fixture/APKBUILD').exists())

    def test_path_escape_and_unsupported_link_are_not_followed(self):
        url = next(iter(self.responses))
        for metadata in [dict(entry('main/fixture/../escape', b'bad'), name='../escape'),
                         dict(entry('main/fixture/link', b'target'), type='symlink')]:
            with self.subTest(metadata=metadata), tempfile.TemporaryDirectory() as directory:
                self.output = Path(directory) / 'output'
                self.responses[url] = json.dumps([metadata]).encode()
                with self.assertRaises(ValueError):
                    self.collect()
                self.assertFalse((Path(directory) / 'escape').exists())

    def test_invalid_plan_rejected_before_network_or_output(self):
        for change in [{'origin': '../escape'}, {'commit': 'main'}, {'repository': 'evil'}, {'collected': False}]:
            plan = receipt()
            plan['recipes'][0].update(change)
            with self.subTest(change=change), patch.object(module, 'fetch') as fetch:
                with self.assertRaises(ValueError):
                    module.collect(plan, self.output)
                fetch.assert_not_called()
                self.assertFalse(self.output.exists())
        plan = receipt()
        plan['recipes'].append(dict(plan['recipes'][0]))
        with self.assertRaises(ValueError):
            module.collect(plan, self.output)

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        (self.output / 'keep').write_text('existing')
        with patch.object(module, 'fetch') as fetch, self.assertRaises(FileExistsError):
            module.collect(receipt(), self.output)
        fetch.assert_not_called()
        self.assertEqual((self.output / 'keep').read_text(), 'existing')

    def test_response_body_is_bounded(self):
        with patch.object(module.urllib.request, 'build_opener') as opener:
            opener.return_value.open.return_value = io.BytesIO(b'12345')
            with self.assertRaisesRegex(ValueError, 'response exceeds'):
                module.fetch('https://raw.githubusercontent.com/alpinelinux/aports/example', 3)

    def test_oversized_or_truncated_metadata_rejected(self):
        url = next(iter(self.responses))
        for entries in [[dict(entry('main/fixture/APKBUILD', APKBUILD), size=module.MAX_FILE + 1)],
                        [entry('main/fixture/APKBUILD', APKBUILD)] * 1000]:
            with self.subTest(count=len(entries)), tempfile.TemporaryDirectory() as directory:
                self.output = Path(directory) / 'output'
                self.responses[url] = json.dumps(entries).encode()
                with self.assertRaises(ValueError):
                    self.collect()
                self.assertEqual(json.loads((self.output / 'recipe-trees.json').read_text())['files'], [])


if __name__ == '__main__':
    unittest.main()
