import hashlib
import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('base_source',Path(__file__).with_name('base-source.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class BaseSourceTests(unittest.TestCase):
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
