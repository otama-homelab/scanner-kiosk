import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('native_secfixes', Path(__file__).with_name('check-native-secfixes.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class NativeSecfixesTests(unittest.TestCase):
    def setUp(self):
        module.compare.cache_clear()

    def test_advisory_fixed_in_an_older_branch_is_not_reported_again(self):
        with patch.object(module, 'compare', side_effect=lambda apk, current, fixed: {'1.0-r0': '>', '2.0-r0': '<'}[fixed]):
            report = module.evaluate('/apk', '1.5-r0', {'1.0-r0': ['old'], '2.0-r0': ['old', 'new']})
        self.assertEqual(report['newer_fix_records'], {'new': ['2.0-r0']})
        self.assertEqual(report['addressed_advisories'], ['old'])
        self.assertEqual(len(report['comparisons']), 2)

    def test_native_apk_owns_version_order_and_invalid_output_fails(self):
        with patch.object(module.subprocess, 'run', return_value=SimpleNamespace(stdout='<\n')) as run:
            self.assertEqual(module.compare('/apk', '2.0_rc1-r0', '2.0-r0'), '<')
            self.assertEqual(run.call_args.args[0], ['/apk', '--no-network', 'version', '-t', '2.0_rc1-r0', '2.0-r0'])
        module.compare.cache_clear()
        with patch.object(module.subprocess, 'run', return_value=SimpleNamespace(stdout='unknown')):
            with self.assertRaises(ValueError):
                module.compare('/apk', 'current', 'fixed')

    def test_malformed_advisory_lists_are_rejected(self):
        for data in [{'1.0-r0': 'CVE-example'}, {'1.0-r0': [None]}, {'': ['CVE-example']}]:
            with self.subTest(data=data), self.assertRaises(ValueError):
                module.evaluate('/apk', '1.0-r0', data)

    def test_unlisted_origins_remain_unknown_and_mismatched_inputs_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps({'files': {'usr/lib/example.so': {}}}))
            provenance = root / 'provenance.json'
            record = {'runtime_manifest_sha256': hashlib.sha256(manifest.read_bytes()).hexdigest(),
                      'apk_source_components': {'example': {'version': '1.0-r0', 'source_metadata': {'originPackage': 'example'}, 'retained_files': ['usr/lib/example.so']}},
                      'upstream_source_components': [{'name': 'tesseract', 'version': '5.5.4'}]}
            provenance.write_text(json.dumps(record))
            databases = {}
            for repo in ['main', 'community']:
                path = root / (repo + '.json')
                path.write_text(json.dumps({'distroversion': 'v3.24', 'reponame': repo, 'packages': []}))
                databases[repo] = path
            with patch.object(module, 'compare') as compare:
                result = module.inspect('/apk', manifest, provenance, databases)
                compare.assert_not_called()
            self.assertFalse(result['release_gate_passed'])
            self.assertEqual(len(result['not_listed_components']), 1)
            self.assertEqual(result['upstream_source_components'], record['upstream_source_components'])
            manifest.write_text('{}')
            with self.assertRaisesRegex(ValueError, 'does not match'):
                module.inspect('/apk', manifest, provenance, databases)


if __name__ == '__main__':
    unittest.main()
