"""Offline contract tests; these do not measure Docling inference quality."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import copy
import sys


def module(name):
    path = Path(__file__).with_name(name + ".py")
    if not path.exists():
        return None
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(path.parent))
    try:
        spec.loader.exec_module(result)
    finally:
        sys.path.pop(0)
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


def raw_document():
    box = {"l": 10, "t": 180, "r": 40, "b": 160, "coord_origin": "BOTTOMLEFT"}
    return {"pages": {"1": {"page_no": 1, "size": {"width": 100, "height": 200}}},
            "body": {"self_ref": "#/body", "children": [{"$ref": "#/tables/0"}, {"$ref": "#/texts/0"}]},
            "texts": [{"self_ref": "#/texts/0", "parent": {"$ref": "#/body"}, "children": [],
                       "label": "text", "text": "日本", "prov": [{"page_no": 1, "bbox": box}]}],
            "tables": [{"self_ref": "#/tables/0", "parent": {"$ref": "#/body"}, "children": [],
                        "label": "table", "prov": [{"page_no": 1, "bbox": box}],
                        "data": {"num_rows": 1, "num_cols": 2, "table_cells": [
                            {"bbox": box, "row_span": 1, "col_span": 2, "start_row_offset_idx": 0,
                             "end_row_offset_idx": 1, "start_col_offset_idx": 0, "end_col_offset_idx": 2,
                             "text": "observed cell"}]}}]}


class NormalizationTests(unittest.TestCase):
    source = {"artifact_id": "artifact_original_001", "sha256": "a" * 64,
              "page_id": "page_ocr_table_001", "width": 200, "height": 400}

    def adapter(self):
        adapter = module("candidate")
        self.assertTrue(hasattr(adapter, "normalize_docling"), "bounded source-bound normalization is required")
        return adapter

    def test_normalization_retains_observed_order_cells_and_pixel_geometry(self):
        result = self.adapter().normalize_docling(raw_document(), self.source, "b" * 64)
        self.assertEqual(result["format"], "scanner-eval-structure")
        self.assertEqual(result["source"], self.source)
        self.assertEqual([b["id"] for b in result["blocks"]], ["#/tables/0", "#/texts/0"])
        self.assertEqual(result["blocks"][1]["box"], {"x": 20, "y": 40, "width": 60, "height": 40})
        cell = result["cells"][0]
        self.assertEqual((cell["row"], cell["column"], cell["row_span"], cell["column_span"]), (0, 0, 1, 2))
        self.assertEqual(cell["table_id"], "#/tables/0")
        self.assertEqual(cell["page_id"], self.source["page_id"])
        self.assertNotIn("ocr_text", result)

    def test_missing_geometry_and_cells_remain_unavailable(self):
        raw = raw_document()
        raw["texts"][0]["prov"] = []
        raw["tables"][0]["data"]["table_cells"][0]["bbox"] = None
        result = self.adapter().normalize_docling(raw, self.source, "b" * 64)
        self.assertIsNone(result["blocks"][1]["box"])
        self.assertEqual(result["blocks"][1]["geometry_status"], "unavailable")
        self.assertIsNone(result["cells"][0]["box"])
        raw["tables"][0]["data"]["table_cells"] = []
        result = self.adapter().normalize_docling(raw, self.source, "b" * 64)
        self.assertEqual(result["cells"], [])
        self.assertEqual(result["blocks"][0]["cells_status"], "unavailable")

    def test_malformed_relationships_geometry_and_bounds_are_rejected(self):
        adapter = self.adapter()
        corruptions = [
            lambda r: r["texts"][0].update(self_ref="#/tables/0"),
            lambda r: r["body"]["children"].append({"$ref": "#/body"}),
            lambda r: r["texts"][0].update(parent={"$ref": "#/missing"}),
            lambda r: r["texts"][0]["prov"][0].update(page_no=2),
            lambda r: r["texts"][0]["prov"][0]["bbox"].update(l=float("nan")),
            lambda r: r["texts"][0]["prov"][0]["bbox"].update(r=101),
            lambda r: r["tables"][0]["data"]["table_cells"][0].update(row_span=0),
            lambda r: r["tables"][0]["data"]["table_cells"].append(copy.deepcopy(r["tables"][0]["data"]["table_cells"][0])),
            lambda r: r["texts"][0].update(text="x" * 65537),
            lambda r: r.update(sha256="c" * 64),
            lambda r: r.update(source={"sha256": "c" * 64}),
            lambda r: r.update(summary="invented"),
            lambda r: r.update(extra=[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[0]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]),
        ]
        for mutate in corruptions:
            raw = raw_document()
            mutate(raw)
            with self.assertRaises(ValueError):
                adapter.normalize_docling(raw, self.source, "b" * 64)
        with self.assertRaises(ValueError):
            adapter.normalize_docling(raw_document(), {**self.source, "sha256": "changed"}, "b" * 64)
        with self.assertRaises(ValueError):
            adapter.normalize_docling(raw_document(), self.source, "changed")


if __name__ == "__main__":
    unittest.main()
