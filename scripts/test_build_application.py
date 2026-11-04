import importlib.util
import io
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('application_build', Path(__file__).with_name('build-application.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ApplicationBuildTests(unittest.TestCase):
    def test_native_archive_accepts_root_and_refuses_escape_or_duplicates(self):
        for bad in ['', 'parent', 'symlink', 'duplicate']:
            with self.subTest(bad=bad), tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);archive=root/'input.tar';target=root/'output';target.mkdir()
                with tarfile.open(archive,'w') as out:
                    entry=tarfile.TarInfo('native/');entry.type=tarfile.DIRTYPE;entry.mode=0o755;out.addfile(entry)
                    entry=tarfile.TarInfo('native/../escape' if bad=='parent' else 'native/data')
                    if bad=='symlink':
                        entry.type=tarfile.SYMTYPE;entry.linkname='../../escape';out.addfile(entry)
                    else:
                        entry.size=4;entry.mode=0o444;out.addfile(entry,io.BytesIO(b'data'))
                        if bad=='duplicate':out.addfile(entry,io.BytesIO(b'data'))
                if bad:
                    with self.assertRaises((ValueError,tarfile.FilterError)):
                        module.extract_native(archive,target)
                else:
                    module.extract_native(archive,target)
                    self.assertEqual((target/'native/data').read_bytes(),b'data')
                self.assertFalse((root/'escape').exists())

    def test_build_environment_is_explicit_and_offline(self):
        with patch.dict(os.environ,{'GITHUB_TOKEN':'do-not-forward','NODE_OPTIONS':'untrusted','GOPROXY':'unsafe'}):
            env=module.environment(True)
        self.assertNotIn('GITHUB_TOKEN',env)
        self.assertNotIn('NODE_OPTIONS',env)
        self.assertEqual(env['GOPROXY'],'off')
        self.assertEqual(env['GOSUMDB'],'off')
        self.assertEqual(env['NPM_CONFIG_OFFLINE'],'true')
        self.assertEqual(env['CGO_ENABLED'],'0')
        self.assertNotEqual(env['NPM_CONFIG_USERCONFIG'],env['NPM_CONFIG_GLOBALCONFIG'])

    def test_build_refuses_changed_dependency_inputs_before_compiling(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'acquisition.json').write_text('{"source_revision":"'+'a'*40+'","locks":{}}')
            with patch.object(module,'WORK',root), patch.object(module,'run') as run:
                with self.assertRaises((ValueError,FileNotFoundError)):
                    module.build('a'*40,None,None)
                self.assertFalse(any('build-web.py' in str(c) for c in run.call_args_list))


if __name__=='__main__':unittest.main()
