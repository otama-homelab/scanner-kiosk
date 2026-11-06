import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest


class RuntimePayloadTest(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        spec=importlib.util.spec_from_file_location('runtime_payload',Path(__file__).with_name('runtime-image-payload.py'))
        self.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.module)
        self.native={'lib/loader':{'size':6,'mode':0o555,'sha256':hashlib.sha256(b'loader').hexdigest()},
                     'usr/lib/library.so':{'size':0,'mode':0,'sha256':'','link':'library.so.1'},
                     'usr/lib/library.so.1':{'size':7,'mode':0o444,'sha256':hashlib.sha256(b'library').hexdigest()}}
        self.manifest=self.root/'native.json';self.manifest.write_text(json.dumps({'files':self.native}))
        self.expected={'binary':{'path':'scanner','bytes':3,'sha256':hashlib.sha256(b'app').hexdigest()},
                       'native_manifest_sha256':hashlib.sha256(self.manifest.read_bytes()).hexdigest(),
                       'native_provenance_sha256':hashlib.sha256(b'provenance').hexdigest(),
                       'license_sha256':hashlib.sha256(b'license').hexdigest()}
        self.container=[{'Id':'a'*64,'Image':'sha256:'+'b'*64,'State':{'Running':False,'ExitCode':0,'OOMKilled':False}}]
        self.rows=[('lib',None,'usr/lib',0o777),('usr/lib/loader',b'loader',None,0o555),
                   ('usr/lib/library.so.1',b'library',None,0o444),('usr/lib/library.so',None,'library.so.1',0o777),
                   ('usr/local/bin/scanner',b'app',None,0o555),
                   ('usr/share/scanner-kiosk/runtime-manifest.json',self.manifest.read_bytes(),None,0o444),
                   ('usr/share/scanner-kiosk/runtime-provenance.json',b'provenance',None,0o444),
                   ('usr/share/doc/scanner-kiosk/LICENSE',b'license',None,0o444)]

    def inspect(self,rows=None,container=None):
        path=self.root/'rootfs.tar'
        with tarfile.open(path,'w') as archive:
            for name,data,link,mode in self.rows if rows is None else rows:
                m=tarfile.TarInfo(name);m.mode=mode
                if link is not None:m.type=tarfile.SYMTYPE;m.linkname=link;archive.addfile(m)
                else:m.size=len(data);archive.addfile(m,io.BytesIO(data))
        return self.module.inspect(path,self.manifest,self.expected,'core','sha256:'+'b'*64,container or self.container)

    def test_native_bytes_modes_aliases_and_exact_executable_are_verified(self):
        proof=self.inspect()
        self.assertTrue(proof['passed']);self.assertEqual(proof['native_paths'],len(self.native))
        self.assertEqual(proof['binary_sha256'],self.expected['binary']['sha256'])
        self.assertEqual(proof['parent_links'],{'lib':'usr/lib'})

    def test_changed_bytes_mode_link_missing_or_duplicate_path_fail(self):
        for changed in ['bytes','mode','link','missing','duplicate','escape','cycle','unexpected-ocr']:
            with self.subTest(changed=changed):
                rows=list(self.rows)
                if changed=='bytes':rows[1]=('usr/lib/loader',b'changed',None,0o555)
                if changed=='mode':rows[1]=('usr/lib/loader',b'loader',None,0o777)
                if changed=='link':rows[3]=('usr/lib/library.so',None,'loader',0o777)
                if changed=='missing':rows.pop(1)
                if changed=='duplicate':rows.append(rows[1])
                if changed=='escape':rows[0]=('lib',None,'../../outside',0o777)
                if changed=='cycle':rows[0]=('lib',None,'lib',0o777)
                if changed=='unexpected-ocr':rows.append(('usr/share/tessdata/hidden.traineddata',b'weights',None,0o444))
                with self.assertRaises(ValueError):self.inspect(rows)

    def test_rootfs_must_come_from_the_verified_successful_container(self):
        for state in [{'Running':True,'ExitCode':0,'OOMKilled':False},
                      {'Running':False,'ExitCode':0,'OOMKilled':True},
                      {'Running':False,'ExitCode':1,'OOMKilled':False}]:
            row={**self.container[0],'State':state}
            with self.assertRaises(ValueError):self.inspect(container=[row])
        with self.assertRaises(ValueError):self.inspect(container=[{**self.container[0],'Image':'sha256:'+'c'*64}])


if __name__=='__main__':unittest.main()
