#!/usr/bin/env python3
"""A single fixed offline Docling CPU adapter; scanner-eval owns measurements."""
import json
import math
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import tempfile

from prepare_candidate import (CANDIDATE, SELF_CHECK, digest, load_runtime,
                               sandbox_command, validate_isolation, write_json)


def normalize_docling(raw, source, lock_hash):
    """Retain observed tree order and cells; never use an oracle to repair output."""
    if (not isinstance(source, dict) or set(source) != {"artifact_id", "sha256", "page_id", "width", "height"}
            or not all(isinstance(source[k], str) and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", source[k])
                       for k in ("artifact_id", "page_id"))
            or not isinstance(source["sha256"], str) or not re.fullmatch(r"[a-f0-9]{64}", source["sha256"])
            or not isinstance(lock_hash, str) or not re.fullmatch(r"[a-f0-9]{64}", lock_hash)
            or any(type(source[k]) is not int or source[k] <= 0 for k in ("width", "height"))
            or source["width"] * source["height"] > 40_000_000):
        raise ValueError("invalid immutable source identity")
    if not isinstance(raw, dict) or set(raw) & {"source", "sha256", "bytes", "summary", "classification", "generated", "ocr_text", "positioned_text"}:
        raise ValueError("structure cannot declare source authority or generated/transcript output")
    stack, nodes, text_bytes = [(raw, 0)], 0, 0
    while stack:
        value, depth = stack.pop()
        nodes += 1
        if depth > 32 or nodes > 65536:
            raise ValueError("unbounded structure nesting")
        if isinstance(value, dict):
            if len(value) > 4096 or any(not isinstance(k, str) for k in value):
                raise ValueError("unbounded structure object")
            stack.extend((v, depth + 1) for v in value.values())
        elif isinstance(value, list):
            if len(value) > 8192:
                raise ValueError("unbounded structure array")
            stack.extend((v, depth + 1) for v in value)
        elif isinstance(value, str):
            size = len(value.encode("utf-8"))
            text_bytes += size
            if size > 65536 or text_bytes > 2 << 20:
                raise ValueError("unbounded structure text")
        elif value is not None and type(value) not in (int, float, bool):
            raise ValueError("invalid JSON value")
        elif type(value) in (int, float) and not math.isfinite(value):
            raise ValueError("nonfinite structure number")
    if len(json.dumps(raw, ensure_ascii=False, allow_nan=False).encode()) > 4 << 20:
        raise ValueError("oversized structure JSON")
    pages = raw.get("pages")
    if (not isinstance(pages, dict) or set(pages) != {"1"} or not isinstance(pages["1"], dict)
            or type(pages["1"].get("page_no")) is not int or pages["1"]["page_no"] != 1):
        raise ValueError("foreign or missing page")
    size = pages["1"].get("size", {})
    width, height = size.get("width"), size.get("height")
    if any(type(v) not in (int, float) or not math.isfinite(v) or not 0 < v <= 100000 for v in (width, height)):
        raise ValueError("invalid observed page dimensions")

    def box(value):
        if value is None:
            return None
        if not isinstance(value, dict) or set(value) != {"l", "t", "r", "b", "coord_origin"}:
            raise ValueError("malformed box")
        left, top, right, bottom = [value[k] for k in ("l", "t", "r", "b")]
        if any(type(v) not in (int, float) or not math.isfinite(v) for v in (left, top, right, bottom)):
            raise ValueError("nonfinite box")
        if value["coord_origin"] == "BOTTOMLEFT":
            top, bottom = height - top, height - bottom
        elif value["coord_origin"] != "TOPLEFT":
            raise ValueError("unknown coordinate origin")
        if not 0 <= left < right <= width or not 0 <= top < bottom <= height:
            raise ValueError("box outside source")
        return {"x": left * source["width"] / width, "y": top * source["height"] / height,
                "width": (right - left) * source["width"] / width,
                "height": (bottom - top) * source["height"] / height}

    items = {}
    roots = [key for key in ("body", "furniture") if key in raw]
    for key in roots:
        item = raw[key]
        if not isinstance(item, dict) or item.get("self_ref") != "#/" + key:
            raise ValueError("malformed document root")
        items[item["self_ref"]] = item
    for kind in ("texts", "tables", "pictures", "groups"):
        values = raw.get(kind, [])
        if not isinstance(values, list) or len(values) > 4096:
            raise ValueError("unbounded block count")
        for index, item in enumerate(values):
            identity = f"#/{kind}/{index}"
            if not isinstance(item, dict) or item.get("self_ref") != identity or identity in items:
                raise ValueError("duplicate or mismatched block ID")
            items[identity] = item
    if not roots or len(items) > 4096:
        raise ValueError("missing or unbounded document tree")
    blocks, cells, visited = [], [], set()

    def visit(identity, parent=None, depth=0):
        if depth > 32 or identity not in items or identity in visited:
            raise ValueError("foreign, duplicate or cyclic block relationship")
        visited.add(identity)
        item = items[identity]
        if parent is not None and item.get("parent") != {"$ref": parent}:
            raise ValueError("parent does not match observed tree")
        if identity not in ("#/body", "#/furniture"):
            provenance = item.get("prov", [])
            if not isinstance(provenance, list) or len(provenance) > 1:
                raise ValueError("ambiguous multi-page block geometry")
            if provenance and (not isinstance(provenance[0], dict) or type(provenance[0].get("page_no")) is not int or provenance[0]["page_no"] != 1):
                raise ValueError("foreign provenance page")
            geometry = box(provenance[0].get("bbox")) if provenance else None
            label, text = item.get("label", "group"), item.get("text")
            if not isinstance(label, str) or not label or len(label) > 96 or text is not None and not isinstance(text, str):
                raise ValueError("malformed observed block")
            block = {"id": identity, "page_id": source["page_id"], "parent_id": parent if parent not in ("#/body", "#/furniture") else None,
                     "kind": label, "order": len(blocks), "text": text, "box": geometry,
                     "geometry_status": "available" if geometry else "unavailable"}
            blocks.append(block)
            if identity.startswith("#/tables/"):
                data = item.get("data")
                block["cells_status"] = "unavailable"
                if data is not None:
                    if not isinstance(data, dict):
                        raise ValueError("malformed table data")
                    rows, columns = data.get("num_rows"), data.get("num_cols")
                    if any(type(v) is not int or not 0 <= v <= 512 for v in (rows, columns)) or rows * columns > 65536:
                        raise ValueError("unbounded table dimensions")
                    observed = data.get("table_cells")
                    if not isinstance(observed, list) or len(observed) > 4096:
                        raise ValueError("unbounded observed cells")
                    block.update(rows=rows, columns=columns, cells_status="available" if observed else "unavailable")
                    occupied = set()
                    for index, cell in enumerate(observed):
                        if not isinstance(cell, dict) or not isinstance(cell.get("text"), str):
                            raise ValueError("malformed observed cell")
                        row, column, end_row, end_col, row_span, col_span = [cell.get(k) for k in (
                            "start_row_offset_idx", "start_col_offset_idx", "end_row_offset_idx", "end_col_offset_idx", "row_span", "col_span")]
                        if (any(type(v) is not int for v in (row, column, end_row, end_col, row_span, col_span))
                                or not 0 <= row < end_row <= rows or not 0 <= column < end_col <= columns
                                or row_span != end_row - row or col_span != end_col - column):
                            raise ValueError("invalid observed cell span")
                        slots = {(r, c) for r in range(row, end_row) for c in range(column, end_col)}
                        if occupied & slots:
                            raise ValueError("overlapping or duplicate observed cells")
                        occupied.update(slots)
                        geometry = box(cell.get("bbox"))
                        cells.append({"id": f"{identity}/cells/{index}", "table_id": identity, "page_id": source["page_id"],
                                      "row": row, "column": column, "row_span": row_span, "column_span": col_span,
                                      "box": geometry, "geometry_status": "available" if geometry else "unavailable", "text": cell["text"]})
                        if len(cells) > 8192:
                            raise ValueError("unbounded cell count")
        children = item.get("children", [])
        if not isinstance(children, list):
            raise ValueError("malformed child list")
        for child in children:
            if not isinstance(child, dict) or set(child) != {"$ref"} or not isinstance(child["$ref"], str):
                raise ValueError("malformed child reference")
            visit(child["$ref"], identity, depth + 1)

    for key in roots:
        visit("#/" + key)
    if len(visited) != len(items):
        raise ValueError("orphan block relationship")
    return {"format": "scanner-eval-structure", "version": 1, "candidate_id": CANDIDATE,
            "runtime_lock_sha256": lock_hash, "source": dict(source), "blocks": blocks, "cells": cells}


def infer(root, lock_hash, source, output, page, original):
    lock = load_runtime(root, root / "runtime.json", lock_hash)
    # All optional package imports are behind verified bytes and the OS isolation gate.
    from PIL import Image
    from docling.datamodel.base_models import InputFormat
    from docling.datamodel.accelerator_options import AcceleratorDevice, AcceleratorOptions
    from docling.datamodel.pipeline_options import PdfPipelineOptions, TesseractCliOcrOptions, TableFormerMode
    from docling.document_converter import DocumentConverter, ImageFormatOption
    options = PdfPipelineOptions(artifacts_path=root / "models", enable_remote_services=False,
                                 allow_external_plugins=False, do_ocr=True, do_table_structure=True,
                                 do_code_enrichment=False, do_formula_enrichment=False,
                                 do_picture_classification=False, do_picture_description=False,
                                 document_timeout=850,
                                 accelerator_options=AcceleratorOptions(num_threads=2, device=AcceleratorDevice.CPU),
                                 ocr_options=TesseractCliOcrOptions(lang=["jpn", "eng"],
                                     tesseract_cmd=lock["tesseract"], path=str(Path(lock["phase46_root"]) / "tessdata")))
    options.table_structure_options.mode = TableFormerMode.ACCURATE
    converter = DocumentConverter(allowed_formats=[InputFormat.IMAGE],
                                  format_options={InputFormat.IMAGE: ImageFormatOption(pipeline_options=options)})
    result = converter.convert(source, raises_on_error=True, max_num_pages=1)
    if str(result.status.value) != "success":
        raise ValueError("Docling did not return a complete page")
    raw = result.document.export_to_dict()
    body = json.dumps(raw, ensure_ascii=False, allow_nan=False).encode()
    if len(body) > 4 << 20:
        raise ValueError("oversized raw structure")
    with Image.open(source) as image:
        width, height = image.size
    (output / "raw-docling.json").write_bytes(body)
    structure = normalize_docling(raw, {"artifact_id": original, "sha256": digest(source),
                                  "page_id": page, "width": width, "height": height}, lock_hash)
    write_json(output / "structure.json", structure)
    write_json(output / "result.json", {"format": "scanner-eval-candidate-result", "version": 1,
               "candidate_id": CANDIDATE, "artifacts": [{"id": "artifact_structure_001", "role": "structure",
               "path": "structure.json", "media_type": "application/json", "source_pages": [page],
               "parents": [original], "algorithm": "docling-cpu", "algorithm_version": "2.149.0"}]})


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--infer":
        _, _, root, lock_hash, source, output, page, original = sys.argv
        infer(Path(root), lock_hash, Path(source), Path(output), page, original)
        return
    root = Path(os.environ["STRUCTURE_ROOT"])
    lock_hash = os.environ["STRUCTURE_LOCK_SHA256"]
    lock = load_runtime(root, root / "runtime.json", lock_hash)
    source, output = Path(os.environ["SCANNER_EVAL_INPUT"]), Path(os.environ["SCANNER_EVAL_OUTPUT"])
    page, original = os.environ["SCANNER_EVAL_SOURCE_PAGES"], os.environ["SCANNER_EVAL_ORIGINAL_ID"]
    if ("," in page or not re.fullmatch(r"[A-Za-z0-9_-]{1,96}", page)
            or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", original) or source.stat().st_size > 32 << 20):
        raise ValueError("invalid single-page source identity")
    header = source.read_bytes()[:24]
    if header[:8] != b"\x89PNG\r\n\x1a\n" or len(header) != 24:
        raise ValueError("one frozen PNG page required")
    width, height = struct.unpack(">II", header[16:24])
    if not 0 < width * height <= 40_000_000:
        raise ValueError("oversized source pixels")
    with tempfile.TemporaryDirectory(prefix="scanner47-run-jail-") as jail:
        check = sandbox_command(jail, root, output, Path(lock["retained_root"]),
                                ["/usr/bin/env", "CHECK_INPUT=" + str(source), lock["python"], "-I", "-c", SELF_CHECK], source)
        proof = subprocess.run(check, check=True, capture_output=True, text=True, timeout=20,
                               env={"PATH": "/usr/bin:/bin"})
        validate_isolation(json.loads(proof.stdout))
        write_json(output / "isolation.json", json.loads(proof.stdout))
        command = sandbox_command(jail, root, output, Path(lock["retained_root"]),
                                  [lock["python"], "-I", "-c",
                                   "import sys; sys.path.insert(0,sys.argv.pop(1)); import candidate; candidate.main()",
                                   str(root / "tools"), "--infer", root, lock_hash, source, output, page, original], source)
        subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                       timeout=870, env={"PATH": "/usr/bin:/bin"})


if __name__ == "__main__":
    main()
