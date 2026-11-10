import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from PIL import Image

import docaligner_candidate as candidate


class AdapterTests(unittest.TestCase):
    def test_rejects_unsafe_corners_without_repair(self):
        good = [[0, 0], [100, 0], [100, 80], [0, 80]]
        self.assertTrue(candidate.safe_quad(good, 100, 80))
        for bad in [[], good[:3], [good[i] for i in [0, 2, 1, 3]],
                    list(reversed(good)), [[0, 0], [1, 0], [1, 1], [0, 1]],
                    [[-1, 0], *good[1:]], [[float('nan'), 0], *good[1:]],
                    [[0, float('inf')], *good[1:]], [[101, 0], *good[1:]]]:
            with self.subTest(bad=bad):
                self.assertFalse(candidate.safe_quad(bad, 100, 80))

    def test_noop_never_loads_or_calls_model(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            source = root / 'source.png'
            output = root / 'out'
            output.mkdir()
            Image.new('RGB', (100, 80), 'white').save(source)
            before = hashlib.sha256(source.read_bytes()).digest()
            env = {'SCANNER_EVAL_INPUT': str(source), 'SCANNER_EVAL_OUTPUT': str(output),
                   'SCANNER_EVAL_ORIGINAL_ID': 'artifact_original_001',
                   'SCANNER_EVAL_SOURCE_PAGES': 'page_source_001',
                   'EVAL_CORRECTION_MODE': 'none'}
            with patch.dict(os.environ, env, clear=True), patch.object(candidate, 'infer', side_effect=AssertionError('inference')):
                candidate.main()
            self.assertEqual(before, hashlib.sha256(source.read_bytes()).digest())
            self.assertEqual(source.read_bytes(), (output / 'corrected.png').read_bytes())
            result = json.loads((output / 'result.json').read_text())
            self.assertEqual(result['artifacts'][0]['parameters']['fallback_reason'], 'not-requested')


if __name__ == '__main__':
    unittest.main()
