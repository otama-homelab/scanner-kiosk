import contextlib
import io
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).with_name('check-vulnerabilities.py')


class VulnerabilityTimeoutTests(unittest.TestCase):
    def test_scan_timeout_preserves_partial_stderr_and_failed_receipt(self):
        for partial in (b'[DEBUG] opening vulnerability DB\n', None):
            with self.subTest(partial=partial), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                tool = root/'grype'
                tool.write_bytes(b'fixture, never executed')
                sbom = root/'sbom.json'
                sbom.write_text(json.dumps({'artifacts': [{'name': 'fixture'}]}))
                output = root/'output'
                argv = [str(SCRIPT), '--grype', str(tool), '--cache-dir', str(root/'cache'),
                        '--sbom', str(sbom), '--output-dir', str(output)]
                timeout = subprocess.TimeoutExpired(['grype'], 600, stderr=partial)
                version = subprocess.CompletedProcess(['grype', 'version'], 0, stdout=b'{}')
                with patch.object(sys, 'argv', argv), patch('subprocess.run', side_effect=[version, timeout]) as run:
                    with contextlib.redirect_stdout(io.StringIO()), self.assertRaises(subprocess.TimeoutExpired):
                        runpy.run_path(str(SCRIPT), run_name='__main__')
                self.assertEqual(run.call_count, 2)
                self.assertEqual((output/'scan.log').read_bytes(), partial or b'')
                receipt = json.loads((output/'receipt.json').read_text())
                self.assertFalse(receipt['passed'])
                self.assertFalse(receipt['scan_completed'])
                self.assertIn('timed out', receipt['error'])
                self.assertIn('finished_at', receipt)


if __name__ == '__main__':
    unittest.main()
