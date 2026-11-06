import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
import test_partition_native as fixtures


class RuntimeImagesTest(unittest.TestCase):
    def setUp(self):
        self.fixture=fixtures.PartitionNativeTest();self.fixture.setUp();self.addCleanup(self.fixture.doCleanups)
        self.part=self.fixture.partition();self.root=self.fixture.root
        self.source=self.root/'source';self.source.mkdir()
        for name,data in {'Dockerfile':'core recipe','Dockerfile.tesseract':'service recipe',
                          '.dockerignore':'context allowlist','LICENSE':'license'}.items():
            (self.source/name).write_text(data)
        self.app=self.root/'app-scanner';self.app.write_bytes(b'core executable')
        self.api=self.root/'scanner-tesseract-api';self.api.write_bytes(b'service executable')
        self.out=self.root/'images';self.revision='a'*40
        spec=importlib.util.spec_from_file_location('prepare_runtime',Path(__file__).with_name('prepare-runtime-images.py'))
        self.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.module)

    def prepare(self):
        return self.module.prepare(self.source,self.fixture.output,self.app,self.api,self.revision,self.out)

    def test_role_contexts_copy_only_manifest_files_and_exact_binaries(self):
        (self.fixture.output/'core/native/unregistered-secret').write_bytes(b'must not ship')
        proof=self.prepare()
        self.assertEqual(proof['source_revision'],self.revision)
        self.assertEqual(proof['parent_manifest_sha256'],self.part['parent_manifest_sha256'])
        for role,binary,name,recipe in [('core',self.app,'scanner','core recipe'),
                                       ('tesseract',self.api,'scanner-tesseract-api','service recipe')]:
            root=self.out/role
            self.assertEqual((root/name).read_bytes(),binary.read_bytes())
            self.assertEqual((root/'Dockerfile').read_text(),recipe)
            self.assertFalse((root/'native/unregistered-secret').exists())
            row=proof['roles'][role]
            self.assertEqual(row['binary']['sha256'],hashlib.sha256(binary.read_bytes()).hexdigest())
            self.assertEqual(row['native_manifest_sha256'],self.part['roles'][role]['native_manifest_sha256'])
            files=json.loads((root/'native-manifest.json').read_text())['files']
            self.module.partition.native.verify_native(root/'native',files)
        self.assertFalse((self.out/'core/scanner-tesseract-api').exists())
        self.assertFalse((self.out/'tesseract/scanner').exists())

    def test_tampered_partition_record_and_binary_symlink_fail_before_context(self):
        p=self.fixture.output/'core/native-manifest.json';p.write_bytes(p.read_bytes()+b' ')
        with self.assertRaises(ValueError):self.prepare()
        self.assertFalse(self.out.exists())
        # Restore the exact original bytes, then reject a binary outside its assigned path.
        p.write_bytes(p.read_bytes()[:-1]);self.app.unlink();self.app.symlink_to(self.api)
        with self.assertRaises(ValueError):self.prepare()
        self.assertFalse(self.out.exists())

    def test_existing_output_and_input_tree_are_preserved(self):
        self.out.mkdir();(self.out/'keep').write_bytes(b'keep')
        with self.assertRaises(FileExistsError):self.prepare()
        self.assertEqual((self.out/'keep').read_bytes(),b'keep')
        self.out=self.fixture.output/'unsafe'
        with self.assertRaises(ValueError):self.prepare()
        self.assertFalse(self.out.exists())


if __name__=='__main__':unittest.main()
