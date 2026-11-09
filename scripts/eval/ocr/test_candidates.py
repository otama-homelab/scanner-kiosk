"""Bounded adapters reject malformed OCR and changed pinned evidence."""
import importlib.util
import hashlib
import tempfile
import unittest
from pathlib import Path


def module(name):
    path = Path(__file__).with_name(name + ".py")
    if not path.exists():
        return None
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class CandidateTests(unittest.TestCase):
    def test_ndlocr_parser_preserves_order_and_rejects_malformed(self):
        adapter = module("candidate")
        self.assertIsNotNone(adapter, "real NDLOCR output normalization is required")
        raw = {"contents": [[{"id": 2, "text": "日本", "isVertical": "true",
                               "boundingBox": [[10, 20], [30, 20], [30, 60], [10, 60]],
                               "confidence": 0.9}]], "imginfo": {}}
        spans = adapter.parse_ndlocr(raw, 100, 100)
        self.assertEqual(spans[0]["text"], "日本")
        self.assertEqual(spans[0]["writing_direction"], "vertical")
        self.assertEqual(spans[0]["box"], {"x": 10, "y": 20, "width": 20, "height": 40})
        for bad in [{}, {"contents": "text"}, {"contents": [[{"text": "x"}]]}]:
            with self.assertRaises(ValueError):
                adapter.parse_ndlocr(bad, 100, 100)
        raw["contents"][0][0]["boundingBox"][0][0] = float("nan")
        with self.assertRaises(ValueError):
            adapter.parse_ndlocr(raw, 100, 100)

    def test_byte_bound_identity_and_root_containment(self):
        prepare = module("prepare_candidates")
        self.assertIsNotNone(prepare, "byte-bound source/model/config verification is required")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            model = root / "model.onnx"
            model.write_bytes(b"fixed model")
            ref = {"path": "model.onnx", "sha256": hashlib.sha256(model.read_bytes()).hexdigest()}
            self.assertEqual(prepare.verify_ref(root, ref), model)
            model.write_bytes(b"mutated model")
            with self.assertRaises(ValueError):
                prepare.verify_ref(root, ref)
            for path in ["../escape", "/etc/passwd"]:
                with self.assertRaises(ValueError):
                    prepare.verify_ref(root, {**ref, "path": path})
            (root / "link").symlink_to("/etc/passwd")
            with self.assertRaises(ValueError):
                prepare.verify_ref(root, {**ref, "path": "link"})


if __name__ == "__main__":
    unittest.main()
