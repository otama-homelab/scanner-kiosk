"""Acquisition failure cannot reach parsing; both stages always clean owned resources."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class NativeBootstrapTests(unittest.TestCase):
    def invoke(self, failure=''):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            calls = work/'calls.jsonl'
            docker = work/'docker'
            docker.write_text('#!'+sys.executable+'\n'+'''import json, os, sys
args = sys.argv[1:]
with open(os.environ['CALLS'], 'a') as out:
    out.write(json.dumps(args)+'\\n')
if args[:2] == ['volume', 'create']:
    print('fixture-volume')
if args[:2] == ['volume', 'rm'] and os.environ['FAILURE'] == 'cleanup':
    sys.exit(29)
if 'fetch-native-apks.py' in ' '.join(args) and os.environ['FAILURE'] == 'fetch':
    sys.exit(17)
if 'prepare-native-apks.py' in ' '.join(args) and os.environ['FAILURE'] == 'prepare':
    sys.exit(23)
''')
            docker.chmod(0o755)
            result = subprocess.run(['/bin/sh', str(ROOT/'scripts/check-native-bootstrap.sh'),
                                     str(ROOT), 'fixture-image'], capture_output=True, text=True,
                                    env={'PATH': str(work)+':/usr/bin:/bin', 'CALLS': str(calls),
                                         'FAILURE': failure}, timeout=10)
            recorded = [json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
            return result, recorded

    def test_fetch_failure_stops_before_parsing_and_cleans(self):
        result, calls = self.invoke('fetch')
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertFalse(any('prepare-native-apks.py' in ' '.join(c) for c in calls))
        self.assertIn(['volume', 'rm', 'fixture-volume'], calls)

    def test_prepare_failure_is_not_hidden_by_cleanup(self):
        result, calls = self.invoke('prepare')
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertIn(['volume', 'rm', 'fixture-volume'], calls)

    def test_volume_is_held_between_stages_and_holder_is_cleaned(self):
        result, calls = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        keeper = next(i for i, c in enumerate(calls)
                      if c[:1] == ['run'] and 'scanner-inputs-fixture-volume' in c)
        fetch = next(i for i, c in enumerate(calls) if 'fetch-native-apks.py' in ' '.join(c))
        self.assertLess(keeper, fetch)
        self.assertTrue(any(c[:2] == ['rm', '--force'] and 'scanner-inputs-fixture-volume' in c
                            for c in calls))

    def test_cleanup_failure_cannot_report_success(self):
        result, _ = self.invoke('cleanup')
        self.assertEqual(result.returncode, 29, result.stderr)

    def test_parsing_has_no_network_and_read_only_archive_volume(self):
        result, calls = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        parser = next(c for c in calls if 'prepare-native-apks.py' in ' '.join(c))
        self.assertEqual(parser[parser.index('--network')+1], 'none')
        self.assertIn('type=volume,src=fixture-volume,dst=/cache,readonly=true', parser)
        self.assertIn(['volume', 'rm', 'fixture-volume'], calls)


if __name__ == '__main__':
    unittest.main()
