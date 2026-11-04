"""Offline checks for source bundle identity, download and filesystem boundaries."""
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch
import urllib.error

spec = importlib.util.spec_from_file_location('collector', Path(__file__).with_name('collect-debian-sources.py'))
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)


class Response(io.BytesIO):
    status = 200
    headers = {}


class SourceBundleTests(unittest.TestCase):
    def setUp(self):
        self.data = b'fixture source archive, never executed'
        self.file = {'filename': 'example_1.orig.tar.xz', 'size': len(self.data),
                     'sha256': hashlib.sha256(self.data).hexdigest(),
                     'url': 'https://snapshot.debian.org/archive/debian/20261102T000000Z/pool/main/e/example/example_1.orig.tar.xz'}
        self.manifest = {'complete_exact_source_coverage': True, 'binary_packages': 1,
                         'source_groups': 1, 'sources': [{'source_package': 'example',
                         'source_version': '1-1', 'binary_packages': [{'name': 'libexample:amd64', 'version': '1-1+b1'}],
                         'files': [self.file]}]}
        self.sbom = {'artifacts': [{'type': 'deb', 'name': 'libexample', 'version': '1-1+b1',
                                   'metadata': {'source': 'example', 'sourceVersion': '1-1'}}]}

    def test_exact_binary_and_source_mapping_including_debian_defaults(self):
        self.assertEqual(collector.plan_files(self.manifest, self.sbom), [self.file])
        self.manifest['sources'][0]['source_version'] = '1-1+b1'
        self.sbom['artifacts'][0]['metadata']['sourceVersion'] = ''
        self.assertEqual(collector.plan_files(self.manifest, self.sbom), [self.file])

    def test_rejects_missing_duplicate_or_changed_binary_and_source(self):
        for change in ['missing', 'duplicate', 'binary', 'source', 'count', 'empty']:
            with self.subTest(change=change):
                manifest, sbom = copy.deepcopy(self.manifest), copy.deepcopy(self.sbom)
                if change == 'missing': sbom['artifacts'] = []
                if change == 'duplicate': sbom['artifacts'] *= 2
                if change == 'binary': sbom['artifacts'][0]['version'] = '2'
                if change == 'source': sbom['artifacts'][0]['metadata']['sourceVersion'] = '2'
                if change == 'count': manifest['source_groups'] = 2
                if change == 'empty': manifest['sources'][0]['files'] = []
                with self.assertRaises(ValueError): collector.plan_files(manifest, sbom)

    def test_rejects_unsafe_names_urls_hashes_and_sizes_before_download(self):
        for field, value in [('filename', '../outside'), ('filename', '/outside'),
                             ('filename', '.'), ('filename', 'receipt.json'), ('url', 'http://snapshot.debian.org/file/a'),
                             ('url', 'https://evil.example/file/a'), ('sha256', 'bad'),
                             ('size', 0), ('size', 2**40)]:
            with self.subTest(field=field, value=value):
                manifest = copy.deepcopy(self.manifest)
                manifest['sources'][0]['files'][0][field] = value
                if field == 'filename':
                    manifest['sources'][0]['files'][0]['url'] = self.file['url'].rsplit('/', 1)[0] + '/' + value
                with self.assertRaises(ValueError): collector.plan_files(manifest, self.sbom)

    def test_transient_gateway_failures_retry_but_bad_content_does_not(self):
        with tempfile.TemporaryDirectory() as tmp:
            error=urllib.error.HTTPError(self.file['url'],504,'Gateway Timeout',{},None)
            fetch=Mock(side_effect=[error,Response(self.data)])
            with patch('time.sleep') as sleep:
                self.assertEqual(collector.collect_file(self.file,Path(tmp),fetch),'downloaded')
                self.assertEqual(fetch.call_count,2);sleep.assert_called_once_with(2)
        for code,calls in [(504,3),(404,1)]:
            with self.subTest(code=code),tempfile.TemporaryDirectory() as tmp:
                fetch=Mock(side_effect=urllib.error.HTTPError(self.file['url'],code,'failed',{},None))
                with patch('time.sleep'),self.assertRaises(urllib.error.HTTPError):
                    collector.collect_file(self.file,Path(tmp),fetch)
                self.assertEqual(fetch.call_count,calls);self.assertEqual(list(Path(tmp).iterdir()),[])
        with tempfile.TemporaryDirectory() as tmp:
            fetch=Mock(return_value=Response(b'bad data'))
            with patch('time.sleep') as sleep,self.assertRaises(ValueError):
                collector.collect_file(self.file,Path(tmp),fetch)
            self.assertEqual(fetch.call_count,1);sleep.assert_not_called()

    def test_verified_file_is_reused_and_corruption_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            calls = []
            def fetch(url, timeout):
                calls.append(url)
                return Response(self.data)
            self.assertEqual(collector.collect_file(self.file, directory, fetch), 'downloaded')
            target = directory / self.file['filename']
            self.assertEqual(target.read_bytes(), self.data)
            self.assertEqual(collector.collect_file(self.file, directory, fetch), 'reused')
            self.assertEqual(len(calls), 1)
            target.write_bytes(b'corrupt')
            with self.assertRaises(ValueError): collector.collect_file(self.file, directory, fetch)
            self.assertEqual(target.read_bytes(), b'corrupt')
            self.assertEqual(len(calls), 1)

    def test_bad_transport_or_hash_leaves_no_final_or_partial_file(self):
        for body in [b'short', self.data + b'overflow', b'x' * len(self.data)]:
            with self.subTest(body=body), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                with self.assertRaises(ValueError):
                    collector.collect_file(self.file, directory, lambda url, timeout: Response(body))
                self.assertEqual(list(directory.iterdir()), [])

    def test_symlink_is_rejected_even_with_expected_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            original = directory / 'original'
            original.write_bytes(self.data)
            (directory / self.file['filename']).symlink_to(original)
            with self.assertRaises(ValueError):
                collector.collect_file(self.file, directory, lambda *a, **kw: self.fail('network used'))
            self.assertEqual(original.read_bytes(), self.data)

    def test_redirect_cannot_leave_snapshot_https_origin(self):
        handler = collector.SnapshotRedirect()
        for url in ['http://snapshot.debian.org/file/x', 'https://other.example/file/x',
                    'https://snapshot.debian.org@127.0.0.1/x']:
            with self.subTest(url=url), self.assertRaises(ValueError):
                handler.redirect_request(None, None, 302, 'redirect', {}, url)

    def test_cli_verifies_retained_bundle_and_rejects_wrong_manifest_pin(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            manifest, sbom, output = root / 'manifest.json', root / 'sbom.json', root / 'bundle'
            second = dict(self.file, filename='example_1-1.dsc', url=self.file['url'].rsplit('/', 1)[0] + '/example_1-1.dsc')
            self.manifest['sources'][0]['files'].append(second)
            manifest.write_text(json.dumps(self.manifest))
            sbom.write_text(json.dumps(self.sbom))
            command = [sys.executable, str(Path(collector.__file__)), '--manifest', str(manifest),
                       '--manifest-sha256', '0' * 64, '--sbom', str(sbom), '--output-dir', str(output)]
            result = subprocess.run(command, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())
            output.mkdir(mode=0o700)
            (output / self.file['filename']).write_bytes(self.data)
            (output / second['filename']).write_bytes(self.data)
            command[5] = hashlib.sha256(manifest.read_bytes()).hexdigest()
            result = subprocess.run(command, capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            receipt = json.loads((output / 'receipt.json').read_text())
            self.assertTrue(receipt['passed'])
            self.assertEqual(receipt['files'][0]['status'], 'reused')
            self.assertTrue(all(f['status'] == 'reused' for f in receipt['files']))
            self.assertEqual(receipt['source_bytes'], 2 * len(self.data))
            self.assertFalse(receipt['release_license_gate_passed'])
            damaged = output / self.file['filename']
            damaged.write_bytes(b'x' * len(self.data))
            result = subprocess.run(command, capture_output=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            receipt = json.loads((output / 'receipt.json').read_text())
            self.assertFalse(receipt['passed'])
            self.assertIn('existing source file differs', receipt['error'])
            self.assertEqual(damaged.read_bytes(), b'x' * len(self.data))


if __name__ == '__main__':
    unittest.main()
