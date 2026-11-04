import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('prepare_candidate',Path(__file__).with_name('prepare-candidate.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class PrepareCandidateTests(unittest.TestCase):
    def test_source_archive_revalidates_inputs_and_preserves_project_correspondence(self):
        for changed in [False,True]:
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);project=root/'project.tar';project.write_bytes(b'project')
                tesseract=root/'tesseract.tar.gz';tesseract.write_bytes(b'source')
                source={'path':'upstream/apk/fixture.tar.gz','bytes':4,'sha256':hashlib.sha256(b'data').hexdigest()}
                lock={'files':[source]}
                with tarfile.open(root/'external.tar','w') as archive:
                    module.tar_bytes(archive,'source-inputs.lock.json',json.dumps(lock).encode())
                    module.tar_bytes(archive,source['path'],b'evil' if changed else b'data')
                index={};args=[root/'external.tar',project,tesseract,lock,
                    {'bytes':6,'sha256':hashlib.sha256(b'source').hexdigest()},index,root/'native.tar']
                if changed:
                    with self.assertRaises(ValueError):module.native_sources(*args)
                else:
                    module.native_sources(*args)
                    self.assertIn('build-controls/scanner-source.tar',[r['archive'] for r in index['files']])
                    with tarfile.open(root/'native.tar') as archive:
                        self.assertEqual(archive.extractfile(source['path']).read(),b'data')
                        self.assertEqual(archive.extractfile('build-controls/scanner-source.tar').read(),b'project')


if __name__=='__main__':unittest.main()
