import copy
import hashlib
import importlib.util
import json
import os
import io
import tarfile
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('assemble', Path(__file__).with_name('assemble-native.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class AssembleNativeTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.work = Path(temp.name)
        self.roots = {name: self.work/name for name in ['apk', 'built', 'notices']}
        for root in self.roots.values():
            root.mkdir()
        self.files = {}
        for role, name, data in [('apk', 'lib/runtime.so.1', b'library'),
                                 ('apk', 'share/roots.pem', b'public certificate'),
                                 ('built', 'bin/helper', b'helper'),
                                 ('notices', 'share/LICENSE', b'license')]:
            source = 'etc/roots.pem' if name == 'share/roots.pem' else name
            path = self.roots[role]/source
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            self.files[name] = {'size': len(data), 'mode': 0o555 if role == 'built' else 0o444,
                                'sha256': hashlib.sha256(data).hexdigest()}
        (self.roots['apk']/'lib/runtime.so').symlink_to('runtime.so.1')
        self.files['lib/runtime.so'] = {'size': 0, 'mode': 0, 'sha256': '', 'link': 'runtime.so.1'}
        self.manifest = self.work/'manifest.json'
        self.manifest.write_text(json.dumps({'files': self.files}))
        self.provenance = {
            'runtime_manifest_sha256': hashlib.sha256(self.manifest.read_bytes()).hexdigest(),
            'apk_source_components': {'fixture': {
                'retained_files': ['lib/runtime.so.1', 'lib/runtime.so', 'share/roots.pem'],
                'relocated_from': {'share/roots.pem': 'etc/roots.pem'}}},
            'upstream_source_components': [],
            'firstparty_components': [{'files': {'bin/helper': self.files['bin/helper']}}],
            'notice_files': ['share/LICENSE'],
        }
        self.output = self.work/'output'

    def assemble(self, provenance=None):
        path = self.work/'provenance.json'
        path.write_text(json.dumps(provenance or self.provenance))
        return module.assemble(self.manifest, path, self.roots['apk'],
                               self.roots['built'], self.roots['notices'], self.output)

    def archive(self, name='build.tar', data=b'helper', kind=tarfile.REGTYPE,
                member='./native/bin/helper', link=''):
        path = self.work/name
        with tarfile.open(path, 'w') as archive:
            info = tarfile.TarInfo(member)
            info.type, info.linkname = kind, link
            info.size = len(data) if kind == tarfile.REGTYPE else 0
            archive.addfile(info, io.BytesIO(data) if info.size else None)
        return path

    def from_archives(self, archives):
        path = self.work/'provenance.json'
        path.write_text(json.dumps(self.provenance))
        return module.assemble(self.manifest, path, self.roots['apk'], None,
                               self.roots['notices'], self.output,
                               built_archives=archives)

    def test_build_archive_selects_only_native_and_preserves_identity(self):
        archive = self.archive()
        with tarfile.open(archive, 'a') as stream:
            info = tarfile.TarInfo('./build.log')
            info.size = 4
            stream.addfile(info, io.BytesIO(b'logs'))
        self.assertEqual(self.from_archives([archive])['entries'], len(self.files))
        self.assertEqual((self.output/'bin/helper').read_bytes(), b'helper')
        self.assertFalse((self.output/'build.log').exists())

    def test_build_archive_malformed_changed_and_duplicate_members_refused(self):
        cases = [self.archive('wrong.tar', data=b'broken'),
                 self.archive('escape.tar', member='./native/../outside'),
                 self.archive('hardlink.tar', kind=tarfile.LNKTYPE, link='bin/helper'),
                 self.archive('fifo.tar', kind=tarfile.FIFOTYPE),
                 self.archive('extra.tar', member='./native/bin/unregistered'),
                 self.archive('missing.tar', member='./build.log')]
        for archive in cases:
            with self.subTest(archive=archive.name), self.assertRaises(ValueError):
                self.from_archives([archive])
            self.assertFalse(self.output.exists())
        valid = self.archive()
        with self.assertRaises(ValueError):
            self.from_archives([valid, valid])
        self.assertFalse(self.output.exists())

    def test_build_archive_link_checked_and_normalized_after_regular_files(self):
        entry = {'size': 0, 'mode': 0, 'sha256': '', 'link': 'helper'}
        self.files['bin/helper-link'] = entry
        self.provenance['firstparty_components'][0]['files']['bin/helper-link'] = entry
        self.manifest.write_text(json.dumps({'files': self.files}))
        self.provenance['runtime_manifest_sha256'] = hashlib.sha256(self.manifest.read_bytes()).hexdigest()
        archive = self.archive(kind=tarfile.SYMTYPE, member='./native/bin/helper-link', link='helper')
        with tarfile.open(archive, 'a') as stream:
            info = tarfile.TarInfo('./native/bin/helper')
            info.size = 6
            stream.addfile(info, io.BytesIO(b'helper'))
        self.from_archives([archive])
        self.assertEqual((self.output/'bin/helper-link').read_bytes(), b'helper')
        self.assertEqual((self.output/'bin/helper-link').readlink().as_posix(), 'helper')
        import shutil
        shutil.rmtree(self.output)
        with self.assertRaises(ValueError):
            self.from_archives([self.archive(kind=tarfile.SYMTYPE,
                                member='./native/bin/helper-link', link='../../outside')])
        self.assertFalse(self.output.exists())

    def test_build_archive_input_is_not_a_symlink_or_fifo(self):
        target = self.archive()
        alias = self.work/'alias.tar'
        alias.symlink_to(target)
        fifo = self.work/'pipe.tar'
        os.mkfifo(fifo)
        for path in [alias, fifo]:
            with self.subTest(path=path.name), self.assertRaises(ValueError):
                self.from_archives([path])
        with self.assertRaises(FileExistsError):
            self.output.mkdir()
            self.from_archives([target])

    def test_selects_exact_files_relocates_and_normalizes_modes(self):
        (self.roots['apk']/'unselected-secret').write_text('dummy, must not copy')
        result = self.assemble()
        self.assertEqual(result['entries'], len(self.files))
        self.assertFalse(result['publication_approved'])
        actual = {p.relative_to(self.output).as_posix() for p in self.output.rglob('*')
                  if p.is_file() or p.is_symlink()}
        self.assertEqual(actual, set(self.files))
        for name, entry in self.files.items():
            path = self.output/name
            if entry.get('link'):
                self.assertEqual(path.readlink().as_posix(), entry['link'])
                self.assertTrue(path.resolve().is_file())
            else:
                self.assertEqual(path.stat().st_mode & 0o777, entry['mode'])
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), entry['sha256'])

    def test_changed_input_removes_partial_output(self):
        (self.roots['notices']/'share/LICENSE').write_bytes(b'changed')
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_existing_output_is_never_modified(self):
        self.output.mkdir()
        sentinel = self.output/'sentinel'
        sentinel.write_bytes(b'keep')
        with self.assertRaises(FileExistsError):
            self.assemble()
        self.assertEqual(sentinel.read_bytes(), b'keep')

    def test_origin_coverage_duplicates_and_stale_manifest_rejected(self):
        missing = copy.deepcopy(self.provenance)
        missing['notice_files'] = []
        duplicate = copy.deepcopy(self.provenance)
        duplicate['notice_files'].append('bin/helper')
        stale = copy.deepcopy(self.provenance)
        stale['runtime_manifest_sha256'] = '0'*64
        mismatch = copy.deepcopy(self.provenance)
        mismatch['firstparty_components'][0]['files']['bin/helper'] = self.files['share/LICENSE']
        for case in [missing, duplicate, stale, mismatch]:
            with self.subTest(case=case), self.assertRaises(ValueError):
                self.assemble(case)
            self.assertFalse(self.output.exists())

    def test_source_symlink_escape_and_special_files_rejected(self):
        target = self.roots['notices']/'share/LICENSE'
        target.unlink()
        target.symlink_to(self.manifest)
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())
        target.unlink()
        target.mkdir()
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())
        target.rmdir()
        os.mkfifo(target)
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_relocated_parent_escape_rejected(self):
        directory = self.roots['apk']/'etc'
        (directory/'roots.pem').unlink()
        directory.rmdir()
        directory.symlink_to(self.roots['notices']/'share', target_is_directory=True)
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_unsafe_paths_and_link_cycles_are_rejected(self):
        for name in ['../outside', '/absolute', 'lib//bad', 'lib/./bad', 'lib\\bad']:
            with self.subTest(name=name):
                with self.assertRaises(ValueError):
                    module.safe_name(name)
        self.files['lib/runtime.so']['link'] = 'runtime.so'
        self.manifest.write_text(json.dumps({'files': self.files}))
        self.provenance['runtime_manifest_sha256'] = hashlib.sha256(self.manifest.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
