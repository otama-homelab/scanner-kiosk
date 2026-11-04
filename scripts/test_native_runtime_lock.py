import hashlib
import json
from pathlib import Path, PurePosixPath
import posixpath
import unittest

ROOT = Path(__file__).resolve().parents[1]


class NativeRuntimeLockTests(unittest.TestCase):
    def setUp(self):
        self.raw = (ROOT / 'native/runtime-manifest.json').read_bytes()
        self.manifest = json.loads(self.raw)
        self.files = self.manifest['files']
        self.provenance = json.loads((ROOT / 'native/runtime-provenance.json').read_text())

    def test_inventory_is_native_only_and_links_resolve_inside_it(self):
        self.assertEqual(self.manifest['platform'], 'linux/amd64')
        self.assertNotIn('app/scanner', self.files)
        for path, entry in self.files.items():
            self.assertFalse(PurePosixPath(path).is_absolute())
            self.assertNotIn('..', PurePosixPath(path).parts)
            if 'link' in entry:
                current = path
                seen = set()
                while 'link' in self.files[current]:
                    self.assertNotIn(current, seen)
                    seen.add(current)
                    link = self.files[current]['link']
                    self.assertFalse(PurePosixPath(link).is_absolute())
                    current = posixpath.normpath(posixpath.join(posixpath.dirname(current), link))
                    self.assertIn(current, self.files)
            else:
                self.assertRegex(entry['sha256'], r'^[0-9a-f]{64}$')
                self.assertIn(entry['mode'], [0o444, 0o555])
                self.assertGreaterEqual(entry['size'], 0)
        self.assertIn('usr/share/scanner-kiosk/ca-certificates.crt', self.files)
        for name in ['eng', 'jpn', 'osd']:
            self.assertIn('usr/share/tessdata/' + name + '.traineddata', self.files)

    def test_every_file_has_exactly_one_origin_or_notice_role(self):
        self.assertEqual(self.provenance['runtime_manifest_sha256'], hashlib.sha256(self.raw).hexdigest())
        owners = []
        for component in self.provenance['apk_source_components'].values():
            self.assertRegex(component['source_metadata']['gitCommitOfApkPort'], r'^[0-9a-f]{40}$')
            owners.extend(component['retained_files'])
        for component in self.provenance['upstream_source_components'] + self.provenance['firstparty_components']:
            for name, entry in component['files'].items():
                self.assertEqual(entry, self.files[name])
                owners.append(name)
        notices = self.provenance['notice_files']
        self.assertTrue(all(p.startswith('usr/share/scanner-kiosk/notices/') for p in notices))
        owners.extend(notices)
        self.assertEqual(len(owners), len(set(owners)))
        self.assertEqual(set(owners), set(self.files))
        self.assertFalse(self.provenance['publication_approved'])

    def test_source_managed_notices_match_the_manifest(self):
        for destination, source in self.provenance['notice_sources'].items():
            self.assertIn(destination, self.provenance['notice_files'])
            path = ROOT/'native/notice-files'/source
            self.assertFalse(path.is_symlink())
            data = path.read_bytes()
            self.assertEqual(len(data), self.files[destination]['size'])
            self.assertEqual(hashlib.sha256(data).hexdigest(), self.files[destination]['sha256'])
        attribution = ROOT/'native/notice-files/libjpeg-turbo-3.1.3/ATTRIBUTION.txt'
        self.assertEqual(attribution.read_text(),
                         'This software is based in part on the work of the Independent JPEG Group.\n')

    def test_firstparty_source_or_sdk_changes_require_native_rebuild(self):
        for component in self.provenance['firstparty_components']:
            self.assertEqual(hashlib.sha256((ROOT / 'native' / component['sdk_lock']).read_bytes()).hexdigest(), component['sdk_lock_sha256'])
            for name, digest in component['source_sha256'].items():
                self.assertEqual(hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), digest, name)


if __name__ == '__main__':
    unittest.main()
