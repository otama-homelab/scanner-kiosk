import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest


def elf(needed=(), interpreter=None):
    strings=b'\0';offsets=[]
    for name in needed:
        offsets.append(len(strings));strings+=name.encode()+b'\0'
    dynamic=[(5,0x400000+768),(10,len(strings)),*[(1,n) for n in offsets],(0,0)]
    data=bytearray(max(1024,768+len(strings)));count=3 if interpreter else 2
    ident=b'\x7fELF\x02\x01\x01'+b'\0'*9
    data[:64]=struct.pack('<16sHHIQQQIHHHHHH',ident,3,62,1,0,64,0,0,64,56,count,0,0,0)
    segments=[(1,5,0,0x400000,0,len(data),len(data),4096),(2,4,512,0x400000+512,0,len(dynamic)*16,len(dynamic)*16,8)]
    if interpreter:
        raw=interpreter.encode()+b'\0';data[400:400+len(raw)]=raw
        segments.append((3,4,400,0x400000+400,0,len(raw),len(raw),1))
    for i,segment in enumerate(segments):data[64+i*56:64+(i+1)*56]=struct.pack('<IIQQQQQQ',*segment)
    for i,pair in enumerate(dynamic):data[512+i*16:512+(i+1)*16]=struct.pack('<qQ',*pair)
    data[768:768+len(strings)]=strings
    return bytes(data)


class PartitionNativeTest(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);self.native=self.root/'native';self.native.mkdir()
        self.files={};loader='/lib/ld-musl-x86_64.so.1'
        for name,data in {
            'usr/bin/scanimage':elf(['libsane.so.1'],loader),
            'usr/libexec/scanner/sane-parameters':elf(['libsane.so.1'],loader),
            'usr/lib/sane/libsane-airscan.so.1':elf(['libshared.so.1']),
            'usr/lib/libsane.so.1':elf(['libshared.so.1']),
            'usr/bin/tesseract':elf(['libtesseract.so.5'],loader),
            'usr/lib/libtesseract.so.5':elf(['libshared.so.1']),
            'usr/lib/libshared.so.1.0':elf(['libc.musl-x86_64.so.1']),
            'lib/ld-musl-x86_64.so.1':elf(),
            'usr/share/scanner-kiosk/ca-certificates.crt':b'public certificates',
            'usr/share/scanner-kiosk/notices/LICENSE':b'fixture license',
            'usr/share/scanner-kiosk/notices/tesseract/LICENSE':b'engine license notice',
            'usr/share/tessdata/eng.traineddata':b'eng',
            'usr/share/tessdata/jpn.traineddata':b'jpn',
            'usr/share/tessdata/osd.traineddata':b'osd',
        }.items():self.put(name,data)
        self.link('usr/lib/libshared.so.1','libshared.so.1.0')
        self.link('usr/lib/libshared.so','libshared.so.1.0')
        self.link('lib/libc.musl-x86_64.so.1','ld-musl-x86_64.so.1')
        self.output=self.root/'output'

    def put(self,name,data):
        p=self.native/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)
        mode=0o555 if data.startswith(b'\x7fELF') else 0o444;p.chmod(mode)
        self.files[name]={'size':len(data),'mode':mode,'sha256':hashlib.sha256(data).hexdigest()}

    def link(self,name,target):
        p=self.native/name;p.parent.mkdir(parents=True,exist_ok=True);p.symlink_to(target)
        self.files[name]={'size':0,'mode':0,'sha256':'','link':target}

    def partition(self):
        spec=importlib.util.spec_from_file_location('partition_native',Path(__file__).with_name('partition-native.py'))
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);self.module=module
        manifest=self.root/'manifest.json';manifest.write_text(json.dumps({'files':self.files}))
        notices=[n for n in self.files if '/notices/' in n]
        provenance={'runtime_manifest_sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),
            'apk_source_components':{},'upstream_source_components':[],
            'firstparty_components':[{'name':'fixture','version':'1','files':{n:r for n,r in self.files.items() if n not in notices}}],
            'notice_files':notices}
        origin=self.root/'provenance.json';origin.write_text(json.dumps(provenance))
        return module.partition(manifest,origin,self.native,self.output)

    def test_exact_dependency_closures_and_sources_without_core_ocr_payload(self):
        proof=self.partition()
        for role in ['core','tesseract']:
            root=self.output/role
            origin=json.loads((root/'native-provenance.json').read_text())
            files,_=self.module.native.assembler.plan(root/'native-manifest.json',origin)
            self.module.native.verify_native(root/'native',files)
            self.assertIn('usr/lib/libshared.so.1',files)
            self.assertIn('usr/lib/libshared.so.1.0',files)
            self.assertIn('usr/lib/libshared.so',files)
            self.assertIn('lib/libc.musl-x86_64.so.1',files)
            self.assertIn('usr/share/scanner-kiosk/notices/LICENSE',files)
            self.assertEqual(proof['roles'][role]['native_manifest_sha256'],origin['runtime_manifest_sha256'])
        core=json.loads((self.output/'core/native-manifest.json').read_text())['files']
        tess=json.loads((self.output/'tesseract/native-manifest.json').read_text())['files']
        self.assertIn('usr/lib/sane/libsane-airscan.so.1',core)
        self.assertNotIn('usr/bin/tesseract',core)
        self.assertFalse(any(n.startswith(('usr/lib/libtesseract','usr/share/tessdata/')) for n in core))
        self.assertIn('usr/share/scanner-kiosk/notices/tesseract/LICENSE',core)
        self.assertIn('usr/bin/tesseract',tess)
        self.assertNotIn('usr/bin/scanimage',tess)
        self.assertFalse(any(n.startswith(('usr/lib/libsane','usr/lib/sane/','usr/libexec/scanner/')) for n in tess))
        self.assertEqual(proof['parent_manifest_sha256'],hashlib.sha256((self.root/'manifest.json').read_bytes()).hexdigest())

    def test_missing_or_ambiguous_dependency_fails_without_output(self):
        for needed in ['libmissing.so.1','../libshared.so.1']:
            with self.subTest(needed=needed):
                self.put('usr/bin/tesseract',elf([needed]))
                with self.assertRaises(ValueError):self.partition()
                self.assertFalse(self.output.exists())
        self.put('usr/bin/tesseract',elf(['libtesseract.so.5']))
        self.put('lib/libshared.so.1',elf())
        with self.assertRaises(ValueError):self.partition()
        self.assertFalse(self.output.exists())

    def test_changed_bytes_and_unsafe_elf_or_links_are_rejected(self):
        p=self.native/'usr/share/tessdata/jpn.traineddata';p.chmod(0o644);p.write_bytes(b'changed');p.chmod(0o444)
        with self.assertRaises(ValueError):self.partition()
        self.assertFalse(self.output.exists())
        self.put('usr/share/tessdata/jpn.traineddata',b'jpn')
        self.put('usr/bin/tesseract',b'not ELF')
        with self.assertRaises(ValueError):self.partition()
        self.assertFalse(self.output.exists())
        self.put('usr/bin/tesseract',elf())
        link=self.native/'usr/lib/libshared.so';link.unlink();link.symlink_to('/etc/passwd')
        self.files['usr/lib/libshared.so']['link']='/etc/passwd'
        with self.assertRaises(ValueError):self.partition()
        self.assertFalse(self.output.exists())

    def test_existing_output_is_never_deleted_or_overwritten(self):
        self.output.mkdir();(self.output/'keep').write_bytes(b'keep')
        with self.assertRaises(FileExistsError):self.partition()
        self.assertEqual((self.output/'keep').read_bytes(),b'keep')

    def test_malformed_elf_tables_cannot_escape_bounded_parsing(self):
        for offset,kind,value in [(18,'H',183),(32,'Q',1<<63),(56,'H',65535),
                                  (128,'Q',999999),(520,'Q',1<<63),(552,'Q',999999),
                                  (560,'q',1)]:
            with self.subTest(offset=offset):
                raw=bytearray(elf(['libshared.so.1'],'/lib/ld-musl-x86_64.so.1'))
                struct.pack_into('<'+kind,raw,offset,value)
                self.put('usr/bin/tesseract',bytes(raw))
                with self.assertRaises(ValueError):self.partition()
                self.assertFalse(self.output.exists())
        raw=bytearray(elf(['libshared.so.1'],'/lib/ld-musl-x86_64.so.1'))
        raw[176:232]=raw[64:120]  # Two load mappings for the same dynamic string table.
        self.put('usr/bin/tesseract',bytes(raw))
        with self.assertRaises(ValueError):self.partition()


if __name__=='__main__':unittest.main()
