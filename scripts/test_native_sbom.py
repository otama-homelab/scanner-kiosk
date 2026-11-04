import importlib.util
import json
import os
from pathlib import Path
import unittest
import test_assemble_native as fixtures


def load():
    spec = importlib.util.spec_from_file_location('native_sbom', Path(__file__).with_name('native-sbom.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class NativeSBOMTest(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.AssembleNativeTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        f = self.fixture
        f.provenance['apk_source_components']['fixture'].update(
            version='1.0-r0', purl='pkg:apk/alpine/fixture@1.0-r0',
            source_metadata={'originPackage': 'fixture'})
        f.provenance['firstparty_components'][0]['name'] = 'fixture-helper'
        f.assemble()
        self.output = f.work/'sbom'

    def catalog(self, output=None):
        f = self.fixture
        return load().catalog(f.manifest, f.work/'provenance.json', f.output,
                              output or self.output)

    def test_complete_deterministic_origins_and_separate_notices(self):
        receipt = self.catalog()
        self.assertEqual(receipt['native_entries'], 5)
        self.assertEqual(receipt['components'], 2)
        self.assertEqual(receipt['code_and_data_files'], 4)
        self.assertEqual(receipt['notice_files'], 1)
        self.assertFalse(receipt['publication_approved'])
        self.assertFalse(receipt['vulnerability_gate_passed'])
        first = {p.name: p.read_bytes() for p in self.output.iterdir()}
        self.catalog(self.fixture.work/'second')
        self.assertEqual(first, {p.name: p.read_bytes() for p in (self.fixture.work/'second').iterdir()})
        bom = json.loads(first['native-origin.cdx.json'])
        self.assertEqual(bom['bomFormat'], 'CycloneDX')
        self.assertEqual(len(bom['components']), 2)
        self.assertTrue(all('licenses' not in c for c in bom['components']))
        self.assertTrue(all('version' not in c for c in bom['components'] if c['name'] == 'fixture-helper'))
        index = json.loads(first['native-origin-inventory.json'])
        self.assertEqual(set(index['notice_files']), {'share/LICENSE'})
        self.assertEqual(len(index['components']), 2)

    def test_source_managed_notices_keep_their_notice_inventory_role(self):
        f = self.fixture
        f.provenance['notice_sources'] = {'share/LICENSE': 'component/LICENSE'}
        (f.work/'provenance.json').write_text(json.dumps(f.provenance))
        receipt = self.catalog()
        self.assertEqual(receipt['code_and_data_files'], 4)
        self.assertEqual(receipt['notice_files'], 1)
        index = json.loads((self.output/'native-origin-inventory.json').read_text())
        self.assertEqual(set(index['notice_files']), {'share/LICENSE'})

    def test_changed_file_refused_without_partial_output(self):
        (self.fixture.output/'bin/helper').chmod(0o644)
        (self.fixture.output/'bin/helper').write_bytes(b'broken')
        (self.fixture.output/'bin/helper').chmod(0o555)
        with self.assertRaises(ValueError):
            self.catalog()
        self.assertFalse(self.output.exists())

    def test_changed_file_mode_refused(self):
        (self.fixture.output/'bin/helper').chmod(0o444)
        with self.assertRaises(ValueError):
            self.catalog()
        self.assertFalse(self.output.exists())

    def test_symlink_parent_and_leaf_refused(self):
        root = self.fixture.output
        (root/'share').rename(root/'saved')
        (root/'share').symlink_to('saved', target_is_directory=True)
        with self.assertRaises(ValueError):
            self.catalog()
        (root/'share').unlink()
        (root/'saved').rename(root/'share')
        target = root/'share/LICENSE'
        target.unlink()
        target.symlink_to('../bin/helper')
        with self.assertRaises(ValueError):
            self.catalog()
        self.assertFalse(self.output.exists())

    def test_special_file_and_changed_link_refused(self):
        root = self.fixture.output
        target = root/'share/LICENSE'
        target.unlink()
        os.mkfifo(target)
        with self.assertRaises(ValueError):
            self.catalog()
        target.unlink()
        target.write_bytes(b'license')
        target.chmod(0o444)
        (root/'lib/runtime.so').unlink()
        (root/'lib/runtime.so').symlink_to('../bin/helper')
        with self.assertRaises(ValueError):
            self.catalog()
        self.assertFalse(self.output.exists())

    def test_stale_or_incomplete_origins_refused(self):
        f = self.fixture
        for stale in [True, False]:
            origin = json.loads((f.work/'provenance.json').read_text())
            if stale:
                origin['runtime_manifest_sha256'] = '0'*64
            else:
                origin['notice_files'] = []
            (f.work/'provenance.json').write_text(json.dumps(origin))
            with self.assertRaises(ValueError):
                self.catalog()
            self.assertFalse(self.output.exists())
            (f.work/'provenance.json').write_text(json.dumps(f.provenance))

    def test_existing_output_and_nested_output_preserved(self):
        self.output.mkdir()
        sentinel = self.output/'sentinel'
        sentinel.write_text('keep')
        with self.assertRaises(FileExistsError):
            self.catalog()
        self.assertEqual(sentinel.read_text(), 'keep')
        with self.assertRaises(ValueError):
            self.catalog(self.fixture.output/'nested')
        self.assertFalse((self.fixture.output/'nested').exists())


if __name__ == '__main__':
    unittest.main()
