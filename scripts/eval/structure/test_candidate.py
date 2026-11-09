"""Offline contract tests; these do not measure Docling inference quality."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


def module(name):
    path = Path(__file__).with_name(name + ".py")
    if not path.exists():
        return None
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class PreparationTests(unittest.TestCase):
    def test_exact_bytes_are_required_before_runtime_use(self):
        prepare = module("prepare_candidate")
        self.assertIsNotNone(prepare, "exact source/wheel/model/config/license gate is required")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for kind in ("source", "wheel", "model", "config", "license"):
                path = root / kind
                path.write_bytes(kind.encode())
                ref = {"path": kind, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                self.assertEqual(prepare.verify_ref(root, ref), path)
                path.write_bytes(b"changed")
                with self.assertRaises(ValueError):
                    prepare.verify_ref(root, ref)
            for path in ("../escape", "/etc/passwd"):
                with self.assertRaises(ValueError):
                    prepare.verify_ref(root, {"path": path, "sha256": "a" * 64})
            (root / "link").symlink_to("/etc/passwd")
            with self.assertRaises(ValueError):
                prepare.verify_ref(root, {"path": "link", "sha256": "a" * 64})

    def test_runtime_lock_change_is_rejected_before_import(self):
        prepare = module("prepare_candidate")
        self.assertIsNotNone(prepare, "runtime lock verification is required")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "runtime.json"
            path.write_text("{}")
            with self.assertRaisesRegex(ValueError, "changed runtime"):
                prepare.load_runtime(Path(directory), path, "a" * 64)

    def test_isolation_requires_all_observed_controls(self):
        prepare = module("prepare_candidate")
        self.assertIsNotNone(prepare, "observed isolation verification is required")
        valid = {"uid": 10001, "gid": 10001, "no_new_privs": 1,
                 "capabilities": [0, 0, 0, 0, 0], "network_denied": True,
                 "input_read_only": True, "root_read_only": True,
                 "cpu_seconds": 900, "rss_limit": 2 << 30,
                 "file_size": 1 << 30, "processes": 64, "open_files": 256,
                 "explicit_environment": True, "threads": 2}
        prepare.validate_isolation(valid)
        for key in valid:
            corrupt = dict(valid)
            corrupt[key] = False if type(valid[key]) is bool else -1
            with self.assertRaises(ValueError, msg=key):
                prepare.validate_isolation(corrupt)

    def test_unavailable_disposition_is_bound_and_never_quality_success(self):
        prepare = module("prepare_candidate")
        self.assertIsNotNone(prepare, "honest failure disposition is required")
        record = prepare.failure_record("isolation", "uid-map-denied", {"fixture_sha256": "a" * 64},
                                        {"exit_code": 1, "stderr": "Operation not permitted"})
        prepare.validate_failure(record)
        for key, value in (("status", "complete"), ("candidate_id", "other"),
                           ("quality_measured", True), ("stage", ""),
                           ("inputs", {}), ("evidence", {}), ("fallback", True)):
            corrupt = dict(record)
            corrupt[key] = value
            with self.assertRaises(ValueError, msg=key):
                prepare.validate_failure(corrupt)


if __name__ == "__main__":
    unittest.main()
