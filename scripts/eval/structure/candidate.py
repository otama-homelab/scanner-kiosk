#!/usr/bin/env python3
"""A single fixed offline Docling CPU adapter; scanner-eval owns measurements."""
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import tempfile

from prepare_candidate import (CANDIDATE, CONFIG, SELF_CHECK, digest, load_runtime,
                               sandbox_command, validate_isolation, write_json)


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
    structure = {"format": "scanner-eval-structure-raw", "version": 1, "candidate_id": CANDIDATE,
                 "runtime_lock_sha256": lock_hash, "source": {"artifact_id": original,
                 "sha256": digest(source), "page_id": page, "width": width, "height": height}, "docling": raw}
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
