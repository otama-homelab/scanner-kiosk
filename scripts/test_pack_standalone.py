"""Offline package checks; fixture bytes never stand in for native acceptance."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('pack_standalone', Path(__file__).with_name('pack-standalone.py'))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class StandalonePackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='scanner-package-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.native = self.root / 'native'
        self.app = self.root / 'scanner'
        self.app.write_bytes(b'synthetic app')
        self.app.chmod(0o755)
        for name in ('lib/ld-musl-x86_64.so.1', 'usr/bin/scanimage', 'usr/bin/tesseract',
                     'usr/libexec/scanner/sane-parameters', 'usr/share/tessdata/eng.traineddata',
                     'usr/share/tessdata/jpn.traineddata', 'usr/share/tessdata/osd.traineddata',
                     'usr/share/scanner-kiosk/ca-certificates.crt'):
            path = self.native / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'synthetic native fixture')
        self.destination = self.root / 'package'

    def pack(self, destination=None):
        with contextlib.redirect_stdout(io.StringIO()):
            packager.pack(self.app, self.native, destination or self.destination)

    def test_deterministic_package_and_contained_link(self):
        (self.native/'lib/libc.so').symlink_to('ld-musl-x86_64.so.1')
        self.pack()
        other = self.root/'other'
        self.pack(other)
        for name in ('payload.tar.gz', 'manifest.json'):
            self.assertEqual((self.destination/name).read_bytes(), (other/name).read_bytes())
        manifest = json.loads((other/'manifest.json').read_text())
        self.assertEqual(manifest['files']['lib/libc.so']['link'], 'ld-musl-x86_64.so.1')
        self.assertEqual(manifest['files']['app/scanner']['mode'], 0o555)

    def test_application_mode_independent_of_host(self):
        self.app.chmod(0o644)
        self.pack()
        self.app.chmod(0o755)
        other = self.root / 'executable-input'
        self.pack(other)
        for name in ('payload.tar.gz', 'manifest.json'):
            self.assertEqual((self.destination / name).read_bytes(), (other / name).read_bytes())
        manifest = json.loads((other / 'manifest.json').read_text())
        self.assertEqual(manifest['files']['app/scanner']['mode'], 0o555)

    def test_rejects_escaping_link_before_publication(self):
        (self.native/'escape').symlink_to('../scanner')
        with self.assertRaises(AssertionError):
            self.pack()
        self.assertFalse(self.destination.exists())

    def test_rejects_reserved_runner_path(self):
        (self.native/'tools').mkdir()
        (self.native/'tools/runner').write_bytes(b'untrusted runner')
        with self.assertRaises(AssertionError):
            self.pack()

    def test_rejects_missing_dictionary(self):
        (self.native/'usr/share/tessdata/jpn.traineddata').unlink()
        with self.assertRaises(AssertionError):
            self.pack()

    def test_rejects_missing_orientation_model(self):
        (self.native/'usr/share/tessdata/osd.traineddata').unlink()
        with self.assertRaises(AssertionError):
            self.pack()

    def test_rejects_missing_ca_bundle(self):
        (self.native/'usr/share/scanner-kiosk/ca-certificates.crt').unlink()
        with self.assertRaises(AssertionError):
            self.pack()

    def test_tagged_launcher_contracts_without_real_native_payload(self):
        self.pack()
        repo = Path(__file__).resolve().parents[1]
        for source in (repo/'cmd/scanner-standalone').glob('*.go'):
            shutil.copyfile(source, self.destination/source.name)
        (self.destination/'go.mod').write_text('module scanner-standalone-check\n\ngo 1.27.4\n')
        result = subprocess.run(['go', 'test', '-race', '-count=1', '-tags', 'bundled', './...'],
                                cwd=self.destination, capture_output=True, text=True, timeout=120,
                                env=dict(os.environ, GOWORK='off'))
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)


if __name__ == '__main__':
    unittest.main()
