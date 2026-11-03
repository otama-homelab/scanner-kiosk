import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('collect_archives', Path(__file__).with_name('collect-aports-archives.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

DATA = b'archive fixture, never extracted'
PATCH = b'local patch fixture'
COMMIT = '1' * 40


class AportsArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.recipes = self.root / 'recipes'
        self.prefix = COMMIT + '/main/fixture'
        folder = self.recipes / self.prefix
        folder.mkdir(parents=True)
        recipe = ('pkgname=fixture\nsha512sums="\n' + hashlib.sha512(DATA).hexdigest()
                  + '  fixture.tar.gz\n' + hashlib.sha512(PATCH).hexdigest() + '  fix.patch\n"\n').encode()
        self.files = {'APKBUILD': recipe, 'fix.patch': PATCH}
        for name, data in self.files.items():
            (folder / name).write_bytes(data)
        self.ledger = {'complete': True, 'origins': [{'origin': 'fixture', 'repository': 'main', 'commit': COMMIT}],
                       'files': [{'path': self.prefix + '/' + name, 'bytes': len(data),
                                  'sha256': hashlib.sha256(data).hexdigest()} for name, data in self.files.items()]}
        self.pin = self.save_ledger()
        self.output = self.root / 'output'
        self.item = {'filename': 'fixture.tar.gz', 'sha512': hashlib.sha512(DATA).hexdigest(), 'origins': [self.prefix]}

    def save_ledger(self):
        data = (json.dumps(self.ledger) + '\n').encode()
        (self.recipes / 'recipe-trees.json').write_bytes(data)
        return hashlib.sha256(data).hexdigest()

    def opener(self, data=DATA):
        mocked = patch.object(module.urllib.request, 'build_opener')
        opener = mocked.start()
        self.addCleanup(mocked.stop)
        opener.return_value.open.return_value = io.BytesIO(data)
        return opener

    def test_plan_binds_recipe_and_local_patch(self):
        plan = module.source_plan(self.recipes, self.pin)
        self.assertEqual(plan['local_source_checks'], 1)
        self.assertEqual(plan['archives'], [self.item])

    def test_leading_underscore_local_source_name(self):
        recipe = self.recipes / self.prefix / 'APKBUILD'
        data = recipe.read_bytes().replace(b'fix.patch', b'__stack_chk_fail_local.c')
        recipe.write_bytes(data)
        (self.recipes / self.prefix / 'fix.patch').rename(self.recipes / self.prefix / '__stack_chk_fail_local.c')
        self.ledger['files'][0].update(bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
        self.ledger['files'][1]['path'] = self.prefix + '/__stack_chk_fail_local.c'
        plan = module.source_plan(self.recipes, self.save_ledger())
        self.assertEqual(plan['local_source_checks'], 1)

    def test_changed_ledger_or_recipe_is_rejected(self):
        with self.assertRaises(ValueError):
            module.source_plan(self.recipes, '0' * 64)
        (self.recipes / self.prefix / 'APKBUILD').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            module.source_plan(self.recipes, self.pin)

    def test_unsupported_checksum_syntax_is_not_executed(self):
        path = self.recipes / self.prefix / 'APKBUILD'
        data = path.read_bytes().replace(b'fixture.tar.gz', b'$(touch marker)')
        path.write_bytes(data)
        self.ledger['files'][0].update(bytes=len(data), sha256=hashlib.sha256(data).hexdigest())
        pin = self.save_ledger()
        with self.assertRaises(ValueError):
            module.source_plan(self.recipes, pin)
        self.assertFalse((self.root / 'marker').exists())

    def test_local_patch_checksum_must_match_recipe(self):
        path = self.recipes / self.prefix / 'fix.patch'
        path.write_bytes(b'changed')
        self.ledger['files'][1].update(bytes=7, sha256=hashlib.sha256(b'changed').hexdigest())
        with self.assertRaisesRegex(ValueError, 'local source checksum'):
            module.source_plan(self.recipes, self.save_ledger())

    def test_streamed_download_is_verified_and_readonly(self):
        self.output.mkdir()
        opener = self.opener()
        result = module.download(self.item, self.output, len(DATA))
        self.assertEqual(result['sha256'], hashlib.sha256(DATA).hexdigest())
        self.assertEqual((self.output / 'fixture.tar.gz').read_bytes(), DATA)
        self.assertEqual((self.output / 'fixture.tar.gz').stat().st_mode & 0o777, 0o400)
        self.assertEqual(list(self.output.glob('.source-*')), [])
        self.assertEqual(opener.return_value.open.call_args.args[0].full_url, module.MIRROR + 'fixture.tar.gz')

    def test_corrupt_or_oversized_download_is_not_published(self):
        self.output.mkdir()
        self.opener(b'wrong source')
        with self.assertRaises(ValueError):
            module.download(self.item, self.output, 100)
        self.assertEqual(list(self.output.iterdir()), [])
        with patch.object(module.urllib.request, 'build_opener') as opener:
            opener.return_value.open.return_value = io.BytesIO(DATA)
            with self.assertRaises(ValueError):
                module.download(self.item, self.output, len(DATA) - 1)
        self.assertEqual(list(self.output.iterdir()), [])

    def test_existing_archive_is_not_overwritten(self):
        self.output.mkdir()
        target = self.output / 'fixture.tar.gz'
        target.write_bytes(b'keep')
        self.opener()
        with self.assertRaises(FileExistsError):
            module.download(self.item, self.output, 100)
        self.assertEqual(target.read_bytes(), b'keep')

    def test_failed_collection_is_explicitly_incomplete(self):
        self.opener(b'wrong')
        with contextlib.redirect_stdout(io.StringIO()):
            result = module.collect(self.recipes, self.pin, self.output)
        self.assertFalse(result['complete'])
        self.assertEqual(result['retained'], [])
        self.assertEqual(len(result['errors']), 1)
        self.assertFalse(json.loads((self.output / 'source-archives.json').read_text())['complete'])

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        (self.output / 'keep').write_text('keep')
        with patch.object(module.urllib.request, 'build_opener') as opener, self.assertRaises(FileExistsError):
            module.collect(self.recipes, self.pin, self.output)
        opener.assert_not_called()
        self.assertEqual((self.output / 'keep').read_text(), 'keep')


if __name__ == '__main__':
    unittest.main()
