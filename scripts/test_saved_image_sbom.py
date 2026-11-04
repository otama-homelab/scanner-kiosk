import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('saved_image',Path(__file__).with_name('saved-image-sbom.py'))
module=importlib.util.module_from_spec(spec)


class SavedImageTests(unittest.TestCase):
    def test_archive_config_and_every_layer_must_match(self):
        spec.loader.exec_module(module)
        for changed in ['', 'config', 'layer', 'duplicate']:
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);layer=b'layer data'
                config=json.dumps({'rootfs':{'diff_ids':['sha256:'+hashlib.sha256(layer).hexdigest()]}}).encode()
                info={'Id':'sha256:'+hashlib.sha256(config).hexdigest(),'RootFS':{'Layers':['sha256:'+hashlib.sha256(layer).hexdigest()]}}
                manifest=[{'Config':'config.json','Layers':['layer.tar']}]
                with tarfile.open(root/'image.tar','w') as archive:
                    directory_member=tarfile.TarInfo('blobs/');directory_member.type=tarfile.DIRTYPE;archive.addfile(directory_member)
                    rows=[('manifest.json',json.dumps(manifest).encode()),('config.json',b'wrong' if changed=='config' else config),
                          ('layer.tar',b'wrong' if changed=='layer' else layer)]
                    if changed=='duplicate':rows.append(rows[-1])
                    for name,data in rows:
                        member=tarfile.TarInfo(name);member.size=len(data);archive.addfile(member,io.BytesIO(data))
                if changed:
                    with self.assertRaises(ValueError):module.verify_archive(root/'image.tar',info)
                else:module.verify_archive(root/'image.tar',info)


if __name__=='__main__':unittest.main()
