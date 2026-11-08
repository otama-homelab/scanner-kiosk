#!/usr/bin/env python3
import json
import re
import unittest
from collections import Counter
from pathlib import Path
from urllib.parse import urlparse


ROOT = Path(__file__).resolve().parents[3]
MATRIX = ROOT / "scripts/eval/correction/candidates.json"
HEX40 = re.compile(r"^[0-9a-f]{40}$")
HEX64 = re.compile(r"^[0-9a-f]{64}$")
ID = re.compile(r"^[a-z][a-z0-9_]{2,95}$")
FAMILIES = {"classical", "lightweight-ai", "heavyweight-ai"}
STATUSES = {"eligible", "unavailable", "rejected"}
RESOURCE_STATUSES = {"measured", "documented", "unavailable"}
MODES = {"cgo", "go-native", "onnx-runtime", "wasm-webgpu", "sidecar-container"}
PROTOCOLS = {"scanner-eval run", "scanner-eval benchmark"}
EXECUTIONS = {"not-run", "measured", "unavailable"}
REQUIRED = {
    "id", "family", "task", "source_url", "source_revision", "source_license",
    "weights_url", "weights_sha256", "weights_license", "framework",
    "input_contract", "output_contract", "model_bytes", "parameter_count",
    "deployment_modes", "cpu_requirement", "gpu_requirement", "min_ram_bytes",
    "min_disk_bytes", "offline", "determinism", "failure_modes",
    "integration_cost", "prototype_status", "reason", "resource_status",
}


class CandidateSurveyTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.matrix = json.loads(MATRIX.read_text())
        cls.candidates = cls.matrix["candidates"]
        cls.by_id = {candidate["id"]: candidate for candidate in cls.candidates}

    def assert_public_https(self, value):
        parsed = urlparse(value)
        self.assertEqual(parsed.scheme, "https", value)
        self.assertTrue(parsed.netloc, value)
        self.assertNotRegex(value, r"(?:/private/|file:|[A-Za-z]:\\)")

    def test_required_family_coverage(self):
        counts = Counter(candidate["family"] for candidate in self.candidates)
        self.assertGreaterEqual(counts["classical"], 2)
        self.assertGreaterEqual(counts["lightweight-ai"], 2)
        self.assertGreaterEqual(counts["heavyweight-ai"], 2)

    def test_closed_schema_and_public_pins(self):
        self.assertEqual(self.matrix["format"], "scanner-correction-candidate-survey")
        self.assertEqual(self.matrix["version"], 1)
        self.assertRegex(self.matrix["surveyed_at"], r"^\d{4}-\d{2}-\d{2}$")
        self.assertEqual(self.matrix["prototype_policy"]["max_non_heavy"], 2)
        self.assertEqual(self.matrix["prototype_policy"]["max_heavy"], 1)
        self.assertEqual(set(self.matrix["prototype_policy"]["protocols"]), PROTOCOLS)
        self.assertEqual(len(self.by_id), len(self.candidates))
        for candidate in self.candidates:
            with self.subTest(candidate=candidate.get("id")):
                self.assertEqual(set(candidate), REQUIRED)
                self.assertRegex(candidate["id"], ID)
                self.assertIn(candidate["family"], FAMILIES)
                self.assertIn(candidate["prototype_status"], STATUSES)
                self.assertIn(candidate["resource_status"], RESOURCE_STATUSES)
                self.assert_public_https(candidate["source_url"])
                self.assertRegex(candidate["source_revision"], HEX40)
                self.assertTrue(candidate["source_license"])
                self.assertTrue(candidate["task"])
                self.assertTrue(candidate["framework"])
                self.assertTrue(candidate["input_contract"])
                self.assertTrue(candidate["output_contract"])
                self.assertIsInstance(candidate["offline"], bool)
                self.assertTrue(candidate["determinism"])
                self.assertTrue(candidate["failure_modes"])
                self.assertTrue(candidate["integration_cost"])
                self.assertTrue(candidate["reason"])
                self.assertTrue(candidate["deployment_modes"])
                self.assertLessEqual(set(candidate["deployment_modes"]), MODES)
                self.assertIsInstance(candidate["model_bytes"], (int, type(None)))
                self.assertIsInstance(candidate["parameter_count"], (int, type(None)))
                self.assertIsInstance(candidate["min_ram_bytes"], (int, type(None)))
                self.assertIsInstance(candidate["min_disk_bytes"], (int, type(None)))
                for value in (
                    candidate["model_bytes"], candidate["parameter_count"],
                    candidate["min_ram_bytes"], candidate["min_disk_bytes"],
                ):
                    if value is not None:
                        self.assertGreaterEqual(value, 0)
                if candidate["resource_status"] == "unavailable":
                    self.assertTrue(candidate.get("resource_note"))
                if candidate["weights_url"] == "none":
                    self.assertEqual(candidate["weights_sha256"], "none")
                    self.assertEqual(candidate["weights_license"], "none")
                    self.assertEqual(candidate["model_bytes"], 0)
                    self.assertEqual(candidate["parameter_count"], 0)
                else:
                    self.assert_public_https(candidate["weights_url"])
                    if candidate["prototype_status"] == "eligible":
                        self.assertRegex(candidate["weights_sha256"], HEX64)
                        self.assertIsInstance(candidate["model_bytes"], int)
                        self.assertGreater(candidate["model_bytes"], 0)
                if candidate["prototype_status"] != "eligible":
                    self.assertIn(candidate["weights_sha256"], (None, "unavailable", "none"))

    def test_deployment_coverage(self):
        modes = {
            mode
            for candidate in self.candidates
            for mode in candidate["deployment_modes"]
        }
        self.assertTrue({"cgo", "onnx-runtime", "wasm-webgpu", "sidecar-container"} <= modes)

    def test_bounded_prototype_selection(self):
        selected = self.matrix["prototype_selection"]
        self.assertEqual(len(selected), len(set(selected)))
        self.assertLessEqual(len(selected), 3)
        non_heavy = 0
        heavy = 0
        for candidate_id in selected:
            self.assertIn(candidate_id, self.by_id)
            candidate = self.by_id[candidate_id]
            self.assertNotEqual(candidate["prototype_status"], "rejected")
            prototype = candidate.get("prototype")
            self.assertIsInstance(prototype, dict)
            self.assertIn(prototype.get("protocol"), PROTOCOLS)
            self.assertIn(prototype.get("execution_status"), EXECUTIONS)
            self.assertTrue(prototype.get("command"))
            self.assertNotRegex(prototype["command"], r"(?:/private/|file:)")
            if candidate["family"] == "heavyweight-ai":
                heavy += 1
            else:
                non_heavy += 1
        self.assertLessEqual(non_heavy, 2)
        self.assertLessEqual(heavy, 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
