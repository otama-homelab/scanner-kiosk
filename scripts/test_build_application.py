import importlib.util
import io
import json
import hashlib
import os
from pathlib import Path
import tarfile
import stat
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('application_build', Path(__file__).with_name('build-application.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ApplicationBuildTests(unittest.TestCase):
    def test_native_archive_accepts_root_and_refuses_escape_or_duplicates(self):
        for bad in ['', 'parent', 'symlink', 'scope-link', 'mode', 'duplicate']:
            with self.subTest(bad=bad), tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);archive=root/'input.tar';target=root/'output';target.mkdir()
                with tarfile.open(archive,'w') as out:
                    entry=tarfile.TarInfo('native/');entry.type=tarfile.DIRTYPE;entry.mode=0o755;out.addfile(entry)
                    entry=tarfile.TarInfo('native/../escape' if bad=='parent' else 'native/data')
                    if bad in ['symlink','scope-link']:
                        entry.type=tarfile.SYMTYPE;entry.linkname='../../escape' if bad=='symlink' else '../escape';out.addfile(entry)
                    else:
                        entry.size=4;entry.mode=0o777 if bad=='mode' else 0o444;out.addfile(entry,io.BytesIO(b'data'))
                        if bad=='duplicate':out.addfile(entry,io.BytesIO(b'data'))
                if bad:
                    with self.assertRaises((ValueError,tarfile.FilterError)):
                        module.extract_native(archive,target)
                else:
                    module.extract_native(archive,target)
                    self.assertEqual((target/'native/data').read_bytes(),b'data')
                    self.assertEqual(stat.S_IMODE((target/'native/data').stat().st_mode),0o444)
                self.assertFalse((root/'escape').exists())

    def test_npm_client_archive_is_pinned_and_extracted_in_private_work(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);source=root/'source';(source/'web').mkdir(parents=True);(root/'bin').mkdir()
            blob=io.BytesIO()
            with tarfile.open(fileobj=blob,mode='w:gz') as archive:
                member=tarfile.TarInfo('package/bin/npm-cli.js');member.size=4;member.mode=0o755
                archive.addfile(member,io.BytesIO(b'data'))
            data=blob.getvalue();lock={'version':'11.21.1','url':'https://registry.npmjs.org/npm/-/npm-11.21.1.tgz',
                                      'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()}
            (source/'web/npm-tool.lock.json').write_text(json.dumps(lock))
            with patch.object(module,'WORK',root),patch.object(module.urllib.request,'build_opener') as opener:
                opener.return_value.open.return_value=io.BytesIO(data)
                self.assertEqual(module.install_npm(source),lock)
            self.assertEqual((root/'npm-tool/package/bin/npm-cli.js').read_bytes(),b'data')
            self.assertTrue((root/'bin/npm').is_symlink())

    def test_npm_lock_must_be_an_object(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);(root/'web').mkdir();(root/'web/npm-tool.lock.json').write_text('[]')
            with self.assertRaises(ValueError):module.install_npm(root)

    def test_npm_client_archive_is_verified_before_extraction(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);source=root/'source';(source/'web').mkdir(parents=True)
            data=b'untrusted archive'
            lock={'version':'11.21.1','url':'https://registry.npmjs.org/npm/-/npm-11.21.1.tgz',
                  'bytes':len(data),'sha256':hashlib.sha256(b'different').hexdigest()}
            (source/'web/npm-tool.lock.json').write_text(json.dumps(lock))
            with patch.object(module,'WORK',root), patch.object(module.urllib.request,'build_opener') as opener:
                opener.return_value.open.return_value=io.BytesIO(data)
                with self.assertRaises(ValueError):module.install_npm(source)
            self.assertFalse((root/'npm-tool').exists())

    def test_npm_client_url_is_restricted_before_network(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);(root/'web').mkdir()
            lock={'version':'11.21.1','url':'https://127.0.0.1/tool','bytes':20,'sha256':'a'*64}
            (root/'web/npm-tool.lock.json').write_text(json.dumps(lock))
            with patch.object(module.urllib.request,'build_opener') as opener:
                with self.assertRaises(ValueError):module.install_npm(root)
                opener.assert_not_called()

    def test_syft_archive_hash_and_selected_member_are_required(self):
        for wrong_hash,linked in [(False,False),(True,False),(False,True)]:
            with self.subTest(wrong_hash=wrong_hash,linked=linked),tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary);source=root/'source';(source/'native').mkdir(parents=True);(root/'bin').mkdir()
                blob=io.BytesIO()
                with tarfile.open(fileobj=blob,mode='w:gz') as archive:
                    member=tarfile.TarInfo('syft')
                    if linked:member.type=tarfile.SYMTYPE;member.linkname='/outside'
                    else:member.size=4
                    archive.addfile(member,None if linked else io.BytesIO(b'tool'))
                data=blob.getvalue()
                lock={'version':'1.55.0','url':'https://github.com/anchore/syft/releases/download/v1.55.0/syft_1.55.0_linux_amd64.tar.gz',
                      'bytes':len(data),'sha256':'0'*64 if wrong_hash else hashlib.sha256(data).hexdigest()}
                (source/'native/syft-tool.lock.json').write_text(json.dumps(lock))
                with patch.object(module,'WORK',root),patch.object(module.urllib.request,'build_opener') as opener:
                    opener.return_value.open.return_value=io.BytesIO(data)
                    if wrong_hash or linked:
                        with self.assertRaises(ValueError):module.install_syft(source)
                        self.assertFalse((root/'bin/syft').exists())
                    else:
                        module.install_syft(source)
                        self.assertEqual((root/'bin/syft').read_bytes(),b'tool')

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

    def test_application_sources_bind_runtime_modules_and_reject_wrong_sums(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);source=root/'source';source.mkdir();cache=root/'modules';cache.mkdir()
            (source/'go.sum').write_text('example.test/module v1.0.0 h1:runtime\nexample.test/module v1.0.0/go.mod h1:mod\n')
            for name in ['app-buildinfo','launcher-buildinfo']:
                (root/(name+'.json')).write_text(json.dumps({'Deps':[{'Path':'example.test/module','Version':'v1.0.0','Sum':'h1:runtime'}]}))
            for name in ['module.zip','module.mod']:(cache/name).write_bytes(b'source data')
            item={'Path':'example.test/module','Version':'v1.0.0','Sum':'h1:runtime','GoModSum':'h1:mod',
                  'Zip':str(cache/'module.zip'),'GoMod':str(cache/'module.mod')}
            def output(command,**kwargs):
                self.assertEqual(kwargs['env']['GOPROXY'],'off')
                if command[:3]==['git','archive','--format=tar']:return b'project source archive'
                return json.dumps(item).encode()
            with patch.object(module,'WORK',root),patch.object(module.subprocess,'check_output',side_effect=output):
                module.retain_sources('a'*40)
                with tarfile.open(root/'application-sources.tar') as archive:
                    self.assertEqual(archive.extractfile('project.tar').read(),b'project source archive')
                    index=json.load(archive.extractfile('INDEX.json'))
                    self.assertEqual(index['source_revision'],'a'*40)
                    self.assertEqual(len(index['modules']),1)
                    self.assertFalse(index['publication_approved'])
                (root/'application-sources.tar').unlink()
                item['Sum']='h1:wrong'
                with self.assertRaises(ValueError):module.retain_sources('a'*40)
                self.assertFalse((root/'application-sources.tar').exists())

    def test_build_refuses_changed_dependency_inputs_before_compiling(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'acquisition.json').write_text('{"source_revision":"'+'a'*40+'","locks":{}}')
            with patch.object(module,'WORK',root), patch.object(module,'run') as run:
                with self.assertRaises((ValueError,FileNotFoundError)):
                    module.build('a'*40,None,None)
                self.assertFalse(any('build-web.py' in str(c) for c in run.call_args_list))


if __name__=='__main__':unittest.main()
