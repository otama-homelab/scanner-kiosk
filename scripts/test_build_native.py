import importlib.util
import json
import os
import io
import sys
import tarfile
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('build_native', Path(__file__).with_name('build-native.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
ROOT = Path(__file__).resolve().parents[1]


class NativePipelineTests(unittest.TestCase):
    def invoke(self, failure='', empty=False):
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary)/'candidate'
            calls = []
            def command(args, **kw):
                calls.append(args)
                text = ' '.join(args)
                if failure == 'cleanup-timeout' and args[:3] == ['docker','rm','--force']:
                    raise subprocess.TimeoutExpired(args, 1)
                if args[:2] == ['docker','run']:
                    for a in args:
                        if a.startswith('type=bind,src='):
                            source = Path(a.split(',src=',1)[1].split(',dst=',1)[0])
                            if source.is_file():
                                self.assertTrue(source.stat().st_mode & 0o004, 'bound input must be readable by UID10001')
                if failure and failure in text:
                    raise subprocess.CalledProcessError(19, args)
                stdout = kw.get('stdout')
                if hasattr(stdout, 'write') and not empty:
                    stdout.write(b'fixture data'*1024)
                if args[:3] == ['docker','volume','create']:
                    value = ('fixture-volume-'+str(len(calls))+'\n').encode()
                elif args[:3] == ['git','rev-parse','HEAD']:
                    value = b'a'*40+b'\n'
                else:
                    value = b''
                return subprocess.CompletedProcess(args, 0, value)
            with patch.object(module.subprocess, 'run', side_effect=command):
                try:
                    module.build(ROOT, out)
                    error = None
                except Exception as exc:
                    error = exc
            receipt = json.loads((out/'result.json').read_text())
            return error, calls, receipt, (out/'native.tar').exists()

    def test_fetch_failure_stops_before_build_and_cleans(self):
        error, calls, receipt, artifact = self.invoke('fetch-native-apks.py')
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)
        self.assertFalse(any(c[:2]==['docker','build'] and 'Dockerfile.sane-sdk' in ' '.join(c) for c in calls))
        self.assertEqual(len([c for c in calls if c[:3]==['docker','volume','rm']]), 2)

    def test_volume_creation_failure_still_attempts_owned_cleanup(self):
        error, calls, receipt, artifact = self.invoke('volume create')
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)
        self.assertTrue(any(c[:3]==['docker','volume','rm'] for c in calls))

    def test_bootstrap_build_failure_still_attempts_owned_tag_cleanup(self):
        error, calls, receipt, artifact = self.invoke('Dockerfile.apk-fetch')
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)
        self.assertTrue(any(c[:3]==['docker','image','rm'] for c in calls))

    def test_compiler_failure_cannot_reach_assembly(self):
        error, calls, receipt, artifact = self.invoke('build-tesseract.sh')
        self.assertIsNotNone(error)
        self.assertFalse(artifact or receipt['passed'])
        self.assertFalse(any('assemble-native.py' in ' '.join(c) for c in calls))

    def test_success_requires_offline_nonroot_builds_and_explicit_inputs(self):
        error, calls, receipt, artifact = self.invoke()
        self.assertIsNone(error)
        self.assertTrue(artifact and receipt['passed'])
        runs = [c for c in calls if c[:2]==['docker','run'] and '--detach' not in c]
        for c in runs:
            self.assertIn('--read-only', c)
            self.assertEqual(c[c.index('--user')+1], '10001:10001')
            self.assertEqual(c[c.index('--cap-drop')+1], 'ALL')
            if c[c.index('--network')+1] != 'none':
                self.assertTrue('fetch-native-apks.py' in ' '.join(c) or 'fetch-tesseract-source.py' in ' '.join(c))
        self.assertTrue(any('assemble-native.py' in ' '.join(c) for c in runs))
        self.assertTrue(any('build-sane-parameters.sh' in ' '.join(c) for c in runs))
        self.assertFalse(receipt['publication_approved'])

    def test_controls_are_prepared_before_read_only_input_mount(self):
        error, calls, receipt, artifact = self.invoke()
        self.assertIsNone(error)
        builds = [c for c in calls if c[:2]==['docker','run'] and 'build-tesseract.sh' in ' '.join(c)]
        self.assertEqual(len(builds), 1)
        self.assertFalse(any(',dst=/inputs/' in a for a in builds[0]))
        copies = [c for c in calls if c[:2]==['docker','run'] and 'copyfile' in ' '.join(c)]
        self.assertTrue(any('/inputs/tesseract-source.SHA256SUMS' in ' '.join(c) for c in copies))
        self.assertTrue(all(c[c.index('--network')+1]=='none' for c in copies))

    def test_cleanup_failure_is_not_success(self):
        error, _, receipt, artifact = self.invoke('volume rm')
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)

    def test_empty_output_is_not_a_candidate(self):
        error, _, receipt, artifact = self.invoke(empty=True)
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)

    def test_container_cleanup_timeout_is_recorded_and_other_resources_are_cleaned(self):
        error, calls, receipt, artifact = self.invoke('cleanup-timeout')
        self.assertIsNotNone(error)
        self.assertFalse(receipt['passed'] or artifact)
        self.assertEqual(len([c for c in calls if c[:3]==['docker','volume','rm']]), 2)

    def test_restrictive_umask_keeps_sandbox_inputs_readable(self):
        previous = os.umask(0o077)
        try:
            error, _, receipt, artifact = self.invoke()
        finally:
            os.umask(previous)
        self.assertIsNone(error)
        self.assertTrue(receipt['passed'] and artifact)

    def test_source_snapshot_accepts_git_archive_root_and_refuses_escape(self):
        for escape in [False, True]:
            with self.subTest(escape=escape), tempfile.TemporaryDirectory() as temporary:
                work = Path(temporary); archive = work/'source.tar'; destination = work/'snapshot'
                destination.mkdir()
                with tarfile.open(archive,'w') as output:
                    root = tarfile.TarInfo('source/');root.type=tarfile.DIRTYPE;root.mode=0o755
                    output.addfile(root)
                    member = tarfile.TarInfo('source/../outside' if escape else 'source/main.go')
                    member.size=4;member.mode=0o644;output.addfile(member,io.BytesIO(b'data'))
                program = module.EXTRACT_SOURCE.replace("'/source.tar'",repr(str(archive))).replace("'/snapshot'",repr(str(destination)))
                result = subprocess.run([sys.executable,'-c',program],capture_output=True,timeout=10)
                self.assertEqual(result.returncode==0, not escape, result.stderr.decode())
                self.assertFalse((work/'outside').exists())
                if not escape:
                    self.assertEqual((destination/'source/main.go').read_bytes(), b'data')

    def test_existing_directory_is_untouched(self):
        with tempfile.TemporaryDirectory() as temporary:
            out = Path(temporary);(out/'keep').write_text('keep')
            with patch.object(module.subprocess, 'run') as call:
                with self.assertRaises(FileExistsError):
                    module.build(ROOT, out)
                call.assert_not_called()
            self.assertEqual((out/'keep').read_text(), 'keep')


if __name__ == '__main__':
    unittest.main()
