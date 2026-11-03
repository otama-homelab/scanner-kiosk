import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('prepare', Path(__file__).with_name('prepare-native-apks.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PrepareNativeAPKsTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.work = Path(temp.name)
        self.archives = self.work/'archives'
        self.keys = self.work/'keys'
        self.archives.mkdir()
        self.keys.mkdir()
        (self.keys/'fixture.pub').write_bytes(b'dummy test key')
        self.output = self.work/'output'
        self.files = {'lib/library.so': {'size': 7, 'mode': 0o555,
                                        'sha256': hashlib.sha256(b'library').hexdigest()}}
        self.manifest = self.work/'manifest.json'
        self.manifest.write_text(json.dumps({'files': self.files}))
        self.origin = self.work/'provenance.json'
        self.origin.write_text(json.dumps({
            'runtime_manifest_sha256': hashlib.sha256(self.manifest.read_bytes()).hexdigest(),
            'apk_source_components': {'fixture': {'retained_files': ['lib/library.so'],
                'source_metadata': {'version': '1-r0', 'originPackage': 'fixture',
                                    'gitCommitOfApkPort': 'a'*40}}},
            'upstream_source_components': [], 'firstparty_components': [], 'notice_files': []}))
        self.record = {'name': 'fixture', 'version': '1-r0', 'filename': 'fixture-1-r0.apk',
                       'archive_architecture': 'noarch', 'origin': 'fixture', 'source_commit': 'a'*40}
        self.lock = self.work/'lock.json'
        self.make_archive()

    def make_archive(self, data=b'library', duplicate=False, arch='noarch', symlink=False):
        path = self.archives/self.record['filename']
        with tarfile.open(path, 'w:gz') as archive:
            control = ('pkgname = fixture\npkgver = 1-r0\narch = '+arch+
                       '\norigin = fixture\ncommit = '+'a'*40+'\n').encode()
            for name, content in [('.PKGINFO', control), ('../../escape', b'ignored'),
                                  ('.post-install', b'never execute package scripts'),
                                  ('lib/library.so', data)]:
                info = tarfile.TarInfo(name)
                info.size = len(content)
                if name == 'lib/library.so' and symlink:
                    info.type, info.linkname, info.size = tarfile.SYMTYPE, '/outside', 0
                archive.addfile(info, io.BytesIO(content))
                if name == 'lib/library.so' and duplicate:
                    archive.addfile(info, io.BytesIO(content))
        self.record.update(bytes=path.stat().st_size, sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        self.lock.write_text(json.dumps({
            'runtime_provenance_sha256': hashlib.sha256(self.origin.read_bytes()).hexdigest(),
            'trusted_keys_sha256': {'fixture.pub': hashlib.sha256(b'dummy test key').hexdigest()},
            'packages': [self.record]}))

    def prepare(self):
        return module.prepare(self.lock, self.manifest, self.origin, self.archives,
                              self.keys, Path('/trusted/apk'), self.output)

    def test_exact_selection_and_clean_offline_verifier_environment(self):
        with mock.patch.object(module.subprocess, 'run') as verify:
            result = self.prepare()
        self.assertEqual(result['entries'], 1)
        self.assertFalse(result['publication_approved'])
        self.assertEqual((self.output/'lib/library.so').read_bytes(), b'library')
        self.assertEqual((self.output/'lib/library.so').stat().st_mode & 0o777, 0o555)
        self.assertFalse((self.work/'escape').exists())
        self.assertFalse((self.output/'.post-install').exists())
        command = verify.call_args.args[0]
        self.assertEqual(command[:3], ['/trusted/apk', '--no-network', '--keys-dir'])
        self.assertEqual(command[4], 'verify')
        self.assertEqual(len(command), 6)
        env = verify.call_args.kwargs['env']
        self.assertEqual(set(env), {'PATH', 'HOME', 'TMPDIR', 'LC_ALL'})
        self.assertTrue(verify.call_args.kwargs['check'])

    def test_signature_failure_never_creates_output(self):
        with mock.patch.object(module.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'apk')):
            with self.assertRaises(subprocess.CalledProcessError):
                self.prepare()
        self.assertFalse(self.output.exists())

    def test_archive_mutation_after_verification_cannot_change_snapshot(self):
        def mutate(*args, **kwargs):
            (self.archives/self.record['filename']).write_bytes(b'changed after verification')
        with mock.patch.object(module.subprocess, 'run', side_effect=mutate):
            self.prepare()
        self.assertEqual((self.output/'lib/library.so').read_bytes(), b'library')

    def test_wrong_archive_or_key_hash_refused_before_verifier(self):
        (self.archives/self.record['filename']).write_bytes(b'corrupt')
        with mock.patch.object(module.subprocess, 'run') as verify:
            with self.assertRaises(ValueError):
                self.prepare()
            verify.assert_not_called()
        self.make_archive()
        (self.keys/'fixture.pub').write_bytes(b'wrong key')
        with mock.patch.object(module.subprocess, 'run') as verify:
            with self.assertRaises(ValueError):
                self.prepare()
            verify.assert_not_called()

    def test_payload_control_duplicate_and_symlink_failures_remove_output(self):
        for options in [{'data': b'changed'}, {'arch': 'wrong'}, {'duplicate': True}, {'symlink': True}]:
            with self.subTest(options=options):
                self.make_archive(**options)
                with mock.patch.object(module.subprocess, 'run'), self.assertRaises(ValueError):
                    self.prepare()
                self.assertFalse(self.output.exists())

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        (self.output/'keep').write_bytes(b'keep')
        with mock.patch.object(module.subprocess, 'run') as verify:
            with self.assertRaises(FileExistsError):
                self.prepare()
            verify.assert_not_called()
        self.assertEqual((self.output/'keep').read_bytes(), b'keep')

    def test_provenance_binding_is_required(self):
        self.origin.write_text(self.origin.read_text()+'\n')
        with mock.patch.object(module.subprocess, 'run') as verify:
            with self.assertRaises(ValueError):
                self.prepare()
            verify.assert_not_called()


if __name__ == '__main__':
    unittest.main()
