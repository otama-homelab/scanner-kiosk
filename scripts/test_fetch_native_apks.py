import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
import urllib.error
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('fetch_native', Path(__file__).with_name('fetch-native-apks.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

DATA = b'pinned archive fixture; never installed'
KEY = b'pinned public key fixture'


class NativeAPKFetchTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.keys = self.root/'keys'
        self.keys.mkdir()
        (self.keys/'fixture.pub').write_bytes(KEY)
        self.lock = {'format_version': 1, 'platform': 'linux/amd64',
                     'repositories': ['https://dl-cdn.alpinelinux.org/alpine/v3.24/main'],
                     'trusted_keys_sha256': {'fixture.pub': hashlib.sha256(KEY).hexdigest()},
                     'packages': [{'name': 'fixture', 'version': '1.0-r0', 'repository': 'main',
                                   'repository_architecture': 'x86_64', 'filename': 'fixture-1.0-r0.apk',
                                   'bytes': len(DATA), 'sha256': hashlib.sha256(DATA).hexdigest()}]}
        self.path = self.root/'lock.json'
        self.output = self.root/'output'
        self.calls = []
        self.payload = DATA
        self.fail = None

    def save(self):
        self.path.write_text(json.dumps(self.lock))

    def respond(self, request, timeout):
        self.calls.append('fetch')
        self.assertEqual(request.full_url, 'https://dl-cdn.alpinelinux.org/alpine/v3.24/main/x86_64/fixture-1.0-r0.apk')
        self.assertEqual(timeout, 30)
        if self.fail == 'fetch':
            raise urllib.error.HTTPError(request.full_url, 503, 'fixture unavailable', {}, None)
        return io.BytesIO(self.payload)

    def invoke(self, args, **options):
        self.calls.append('verify')
        self.assertEqual(args[0], '/sbin/apk')
        self.assertIs(options['stdin'], subprocess.DEVNULL)
        self.assertEqual(set(options['env']), {'PATH', 'HOME', 'TMPDIR', 'LC_ALL'})
        self.assertFalse(options.get('shell', False))
        keys = Path(args[args.index('--keys-dir')+1])
        self.assertEqual((keys/'fixture.pub').read_bytes(), KEY)
        self.assertIn('--no-network', args)
        verified = Path(args[-1])
        self.assertEqual(verified.read_bytes(), DATA)
        self.assertEqual(verified.stat().st_mode & 0o777, 0o444)
        if self.fail == 'verify':
            raise subprocess.CalledProcessError(1, args)
        return SimpleNamespace(returncode=0)

    def fetch(self):
        self.save()
        with patch.object(module.subprocess, 'run', side_effect=self.invoke), \
                patch.object(module.urllib.request, 'build_opener') as opener:
            opener.return_value.open.side_effect = self.respond
            result = module.fetch(self.path, self.keys, Path('/sbin/apk'), self.output)
            handlers = opener.call_args.args
            self.assertEqual(handlers[0].proxies, {})
            self.assertIsInstance(handlers[1], module.NoRedirect)
            self.assertIsNone(handlers[1].redirect_request(None, None, 302, '', {}, 'https://other.test/'))
            return result

    def test_fetch_fixed_url_then_offline_verify_before_output(self):
        result = self.fetch()
        self.assertEqual(self.calls, ['fetch', 'verify'])
        self.assertEqual(list(self.output.iterdir()), [self.output/'fixture-1.0-r0.apk'])
        self.assertEqual((self.output/'fixture-1.0-r0.apk').read_bytes(), DATA)
        self.assertEqual(result['packages'], 1)
        self.assertFalse(result['publication_approved'])
        self.assertEqual(result['lock_sha256'], hashlib.sha256(self.path.read_bytes()).hexdigest())

    def test_missing_archives_use_only_fixed_mirrors_before_signature_verification(self):
        for available in [1, 2]:
            with self.subTest(available=available):
                urls = []
                def respond(request, timeout):
                    urls.append(request.full_url)
                    if len(urls) <= available:
                        raise urllib.error.HTTPError(request.full_url, 404, 'missing', {}, None)
                    return io.BytesIO(DATA)
                with patch.object(self, 'respond', side_effect=respond):
                    self.fetch()
                self.assertEqual(urls, [origin+'/v3.24/main/x86_64/fixture-1.0-r0.apk'
                                        for origin in module.APK_ORIGINS[:available+1]])
                self.assertEqual(self.calls[-1], 'verify')
                (self.output/'fixture-1.0-r0.apk').unlink()
                self.output.rmdir()

    def test_mirror_failures_do_not_weaken_hash_or_signature_checks(self):
        for failure in ['missing', 'hash', 'verify']:
            with self.subTest(failure=failure):
                urls = []
                def respond(request, timeout):
                    urls.append(request.full_url)
                    if len(urls) == 1 or failure == 'missing':
                        raise urllib.error.HTTPError(request.full_url, 404, 'missing', {}, None)
                    return io.BytesIO(b'X'+DATA[1:] if failure == 'hash' else DATA)
                self.fail = 'verify' if failure == 'verify' else None
                with patch.object(self, 'respond', side_effect=respond):
                    with self.assertRaises((OSError, ValueError, subprocess.SubprocessError)):
                        self.fetch()
                self.assertEqual(len(urls), 3 if failure == 'missing' else 2)
                self.assertFalse(self.output.exists())

    def test_failure_or_bad_archive_never_publishes_output(self):
        for failure, payload in [('fetch', DATA), ('verify', DATA),
                                 (None, b'wrong'), (None, b'X'+DATA[1:]), (None, DATA+b'extra')]:
            with self.subTest(failure=failure, payload=payload):
                self.calls = []
                self.fail, self.payload = failure, payload
                with self.assertRaises((ValueError, OSError, subprocess.SubprocessError)):
                    self.fetch()
                self.assertFalse(self.output.exists())

    def test_failure_diagnostic_contains_only_public_identity_and_status(self):
        self.fail = 'fetch'
        stderr = io.StringIO()
        with patch.object(module.sys, 'stderr', stderr):
            with self.assertRaises(urllib.error.HTTPError):
                self.fetch()
        self.assertEqual(stderr.getvalue(), 'locked APK failure: fixture-1.0-r0.apk HTTPError status=503\n')
        self.assertFalse(self.output.exists())

    def test_invalid_lock_is_rejected_before_external_tool(self):
        original = copy.deepcopy(self.lock)
        variants = []
        for key, value in [('name', '../fixture'), ('version', '1.0-r0;echo bad'),
                           ('filename', '../fixture.apk'), ('repository', 'other'),
                           ('repository_architecture', 'aarch64'), ('bytes', True),
                           ('bytes', 129 << 20), ('sha256', 'bad')]:
            changed = copy.deepcopy(original)
            changed['packages'][0][key] = value
            variants.append(changed)
        changed = copy.deepcopy(original)
        changed['packages'] *= 2
        variants.append(changed)
        changed = copy.deepcopy(original)
        changed['repositories'] = ['https://example.test/main']
        variants.append(changed)
        changed = copy.deepcopy(original)
        changed['platform'] = 'linux/arm64'
        variants.append(changed)
        for candidate in variants:
            with self.subTest(candidate=candidate):
                self.lock = candidate
                with self.assertRaises(ValueError):
                    self.fetch()
                self.assertFalse(self.calls)
                self.assertFalse(self.output.exists())

    def test_existing_output_and_untrusted_keys_are_rejected(self):
        self.output.mkdir()
        marker = self.output/'keep'
        marker.write_bytes(b'keep')
        with self.assertRaises(FileExistsError):
            self.fetch()
        self.assertEqual(marker.read_bytes(), b'keep')
        marker.unlink()
        self.output.rmdir()
        (self.keys/'fixture.pub').write_bytes(b'wrong key')
        with self.assertRaises(ValueError):
            self.fetch()
        self.assertFalse(self.calls)

    def test_sdk_format_and_noarch_archive_use_x86_repository(self):
        self.lock['format'] = self.lock.pop('format_version')
        self.lock['packages'][0].pop('repository_architecture')
        self.lock['packages'][0]['archive_architecture'] = 'noarch'
        self.fetch()
        self.assertEqual(self.calls, ['fetch', 'verify'])

    def test_nonobject_and_symlinked_lock_are_rejected_before_tool(self):
        with patch.object(module.subprocess, 'run', side_effect=self.invoke):
            self.path.write_text('[]')
            with self.assertRaises(ValueError):
                module.fetch(self.path, self.keys, Path('/sbin/apk'), self.output)
            self.path.unlink()
            real = self.root/'real.json'
            real.write_text(json.dumps(self.lock))
            self.path.symlink_to(real)
            with self.assertRaises(OSError):
                module.fetch(self.path, self.keys, Path('/sbin/apk'), self.output)
        self.assertFalse(self.calls)
        self.assertFalse(self.output.exists())

    def test_committed_runtime_and_sdk_locks_are_supported(self):
        native = Path(__file__).resolve().parents[1]/'native'
        for filename, count in [('runtime-apks.lock.json', 48), ('sane-sdk.lock.json', 46),
                                ('tesseract-sdk.lock.json', 174)]:
            with self.subTest(filename=filename):
                records = module.validate(json.loads((native/filename).read_bytes()))
                self.assertEqual(len(records), count)

    def test_PDFRasterNativeInputs(self):
        del self.fail  # Fetch fixtures use this name; restore unittest assertions.
        native = Path(__file__).resolve().parents[1]/'native'
        lock = json.loads((native/'runtime-apks.lock.json').read_bytes())
        origin_raw = (native/'runtime-provenance.json').read_bytes()
        origin = json.loads(origin_raw)
        files = json.loads((native/'runtime-manifest.json').read_bytes())['files']
        self.assertEqual(hashlib.sha256(origin_raw).hexdigest(), lock['runtime_provenance_sha256'])
        packages = {p['name']: p for p in module.validate(lock)}
        for executable in ['pdftoppm', 'pdftotext', 'pdfinfo']:
            path = 'usr/bin/'+executable
            self.assertIn(path, files)
            self.assertIn(path, origin['apk_source_components']['poppler-utils']['retained_files'])
        for package in ['poppler', 'poppler-utils', 'poppler-data']:
            self.assertIn(package, packages)
            self.assertRegex(packages[package]['apk_control_checksum'], r'^Q1')
            self.assertRegex(packages[package]['source_commit'], r'^[0-9a-f]{40}$')
        sources = json.loads((native/'source-inputs.lock.json').read_bytes())['files']
        self.assertTrue(any('/poppler-' in item['path'] for item in sources))
        self.assertTrue(any('/poppler-data-' in item['path'] for item in sources))
        self.assertTrue(any('/poppler-' in path and path.endswith('/COPYING')
                            for path in origin['notice_files']))


if __name__ == '__main__':
    unittest.main()
