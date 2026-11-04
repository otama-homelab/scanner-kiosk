import hashlib
import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest


class PackSourceInputsTest(unittest.TestCase):
    def test_verified_archive_and_reject_modified_or_linked_input(self):
        spec = importlib.util.spec_from_file_location('pack_sources', Path(__file__).with_name('pack-source-inputs.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            inputs = root/'inputs'
            (inputs/'upstream/apk').mkdir(parents=True)
            source = inputs/'upstream/apk/fixture.tar.gz'
            source.write_bytes(b'data')
            entry = {'path': 'upstream/apk/fixture.tar.gz', 'role': 'apk-upstream-source',
                     'url': 'https://distfiles.alpinelinux.org/distfiles/v3.24/fixture.tar.gz',
                     'bytes': 4, 'sha256': hashlib.sha256(b'data').hexdigest()}
            lock = root/'lock.json'
            lock.write_text(json.dumps({'format': 1, 'files': [entry]}))
            output = root/'sources.tar'
            module.pack(lock, inputs, output)
            with tarfile.open(output) as archive:
                self.assertEqual(archive.extractfile(entry['path']).read(), b'data')
                self.assertEqual(archive.extractfile('source-inputs.lock.json').read(), lock.read_bytes())
                self.assertTrue(all(m.isfile() and m.uid == 0 and m.mtime == 0 for m in archive))
            with self.assertRaises(FileExistsError):
                module.pack(lock, inputs, output)
            for linked in [False, True]:
                if linked:
                    source.unlink()
                    source.symlink_to(lock)
                else:
                    source.write_bytes(b'evil')
                destination = root/('bad-link.tar' if linked else 'bad-hash.tar')
                with self.assertRaises((ValueError, OSError)):
                    module.pack(lock, inputs, destination)
                self.assertFalse(destination.exists())


if __name__ == '__main__':
    unittest.main()
