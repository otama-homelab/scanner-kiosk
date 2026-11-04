import hashlib
import importlib.util
import json
import io
import urllib.error
from unittest.mock import Mock, patch
from pathlib import Path
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('base_source',Path(__file__).with_name('base-source.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class BaseSourceTests(unittest.TestCase):
    def test_official_cdn_and_snapshot_fallback_require_identical_locked_bytes(self):
        class Response(io.BytesIO):
            status=200
            headers={}
        for fallback,bad in [(False,False),(True,False),(False,True)]:
            with self.subTest(fallback=fallback,bad=bad),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);lock=root/'lock.json'
                url='https://snapshot.debian.org/archive/debian/20261102T000000Z/pool/main/e/example/source.tar.xz'
                entry={'filename':'source.tar.xz','url':url,'size':4,'sha256':hashlib.sha256(b'data').hexdigest()}
                lock.write_text(json.dumps({'format':1,'base_digest':'sha256:'+'a'*64,'packages':[],
                                            'sources':[{'files':[entry]}],'image_files':[]}))
                snapshot,cdn=Mock(),Mock()
                snapshot.open.return_value=Response(b'data')
                if fallback:cdn.open.side_effect=urllib.error.HTTPError(url,504,'timeout',{},None)
                else:cdn.open.return_value=Response(b'evil' if bad else b'data')
                with patch.object(module.urllib.request,'build_opener',side_effect=[snapshot,cdn]):
                    if bad:
                        with self.assertRaises(ValueError):module.acquire(lock,root/'out')
                        snapshot.open.assert_not_called()
                    else:
                        module.acquire(lock,root/'out')
                        self.assertEqual((root/'out/source.tar.xz').read_bytes(),b'data')
                        self.assertEqual(snapshot.open.call_count,int(fallback))
                cdn.open.assert_called_once_with('https://deb.debian.org/debian/pool/main/e/example/source.tar.xz',timeout=60)

    def test_cdn_derivation_rejects_foreign_or_escaping_source_paths(self):
        for url in ['http://snapshot.debian.org/archive/debian/20261102T000000Z/pool/file',
                    'https://example.test/archive/debian/20261102T000000Z/pool/file',
                    'https://snapshot.debian.org/archive/debian/20261102T000000Z/pool/../secret',
                    'https://snapshot.debian.org/archive/debian/20261102T000000Z/pool/%2e%2e/secret']:
            with self.subTest(url=url),self.assertRaises(ValueError):module.cdn_url(url)

    def test_current_image_and_verified_source_subset_are_bound(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'image').mkdir();(root/'image/file').write_bytes(b'base')
            digest=hashlib.sha256(b'base').hexdigest()
            lock=root/'lock.json';lock.write_text(json.dumps({'format':1,'base_digest':'sha256:'+'a'*64,
                'packages':[],'sources':[],'image_files':[{'file':'image/file','bytes':4,'sha256':digest}]}))
            metadata=root/'image.json';metadata.write_text(json.dumps([{'Id':'sha256:'+'b'*64}]))
            output=root/'base.tar'
            receipt=module.pack(lock,root,root,metadata,output)
            self.assertTrue(receipt['passed']);self.assertEqual(receipt['image_id'],'sha256:'+'b'*64)
            with tarfile.open(output) as archive:self.assertEqual(archive.extractfile('image/file').read(),b'base')
            output.unlink();(root/'image/file').write_bytes(b'evil')
            with self.assertRaises(ValueError):module.pack(lock,root,root,metadata,output)
            self.assertFalse(output.exists())


if __name__=='__main__':unittest.main()
