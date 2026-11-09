#!/usr/bin/env python3
"""Thin real OCR adapters; scanner-eval owns timing, resources and receipts."""
import csv
import hashlib
import io
import json
import math
import os
from pathlib import Path
import subprocess


def parse_ndlocr(raw, width, height):
    if not isinstance(raw, dict) or not isinstance(raw.get("contents"), list) or len(raw["contents"]) != 1:
        raise ValueError("malformed OCR contents")
    lines = raw["contents"][0]
    if not isinstance(lines, list) or len(lines) > 4096:
        raise ValueError("unbounded OCR contents")
    spans = []
    seen = set()
    for line in lines:
        if not isinstance(line, dict) or not isinstance(line.get("text"), str) or len(line["text"]) > 16384:
            raise ValueError("malformed OCR line")
        if line.get("isVertical") not in ("true", "false") or type(line.get("id")) is not int or line["id"] in seen:
            raise ValueError("malformed OCR order")
        seen.add(line["id"])
        box = line.get("boundingBox")
        if not isinstance(box, list) or len(box) != 4 or any(not isinstance(p, list) or len(p) != 2 for p in box):
            raise ValueError("malformed OCR box")
        values = [v for p in box for v in p]
        if any(type(v) not in (int, float) or not math.isfinite(v) for v in values):
            raise ValueError("nonfinite OCR box")
        x, y = min(p[0] for p in box), min(p[1] for p in box)
        right, bottom = max(p[0] for p in box), max(p[1] for p in box)
        if x < 0 or y < 0 or right > width or bottom > height or right <= x or bottom <= y:
            raise ValueError("OCR box outside source")
        if line["text"]:
            spans.append({"text": line["text"], "box": {"x": x, "y": y, "width": right-x, "height": bottom-y},
                          "writing_direction": "vertical" if line["isVertical"] == "true" else "horizontal"})
    return spans


def parse_tsv(body, width, height):
    spans = []
    for row in csv.DictReader(io.StringIO(body), delimiter="\t", quoting=csv.QUOTE_NONE):
        if row["level"] != "5" or not row["text"].strip():
            continue
        x, y, w, h = [int(row[key]) for key in ("left", "top", "width", "height")]
        if x < 0 or y < 0 or w <= 0 or h <= 0 or x+w > width or y+h > height:
            raise ValueError("TSV box outside source")
        spans.append({"text": row["text"], "box": {"x": x, "y": y, "width": w, "height": h}})
    return spans


def main():
    from PIL import Image
    from prepare_candidates import verify_ref
    root = Path(os.environ["OCR_EVIDENCE_ROOT"])
    lock_bytes = Path(os.environ["OCR_RUNTIME_LOCK"]).read_bytes()
    if hashlib.sha256(lock_bytes).hexdigest() != os.environ["OCR_RUNTIME_LOCK_SHA256"]:
        raise ValueError("changed runtime configuration")
    lock = json.loads(lock_bytes)
    for ref in lock["files"]:
        verify_ref(root, ref)
    source, output = Path(os.environ["SCANNER_EVAL_INPUT"]), Path(os.environ["SCANNER_EVAL_OUTPUT"])
    pages = os.environ["SCANNER_EVAL_SOURCE_PAGES"].split(",")
    if source.read_bytes()[:5] == b"%PDF-":
        import pypdfium2 as pdfium
        pdf = pdfium.PdfDocument(source)
        if len(pdf) != len(pages):
            raise ValueError("source page count changed")
        images = [pdf[i].render(scale=1).to_pil().convert("RGB") for i in range(len(pdf))]
    else:
        images = [Image.open(source).convert("RGB")]
    if len(images) != len(pages):
        raise ValueError("source page identity changed")
    artifacts = []
    positioned = []
    engine = os.environ["OCR_ENGINE"]
    for index, (page, image) in enumerate(zip(pages, images)):
        image_path = output / f"input-{index}.png"
        image.save(image_path)
        if engine == "tesseract-5.5.4":
            command = [lock["tesseract"], str(image_path), "stdout", "--tessdata-dir", lock["tessdata"], "-l", "jpn+eng", "--psm", "1", "tsv"]
            result = subprocess.run(command, check=True, capture_output=True, timeout=850)
            spans = parse_tsv(result.stdout.decode("utf-8"), *image.size)
        else:
            rawdir = output / f"raw-{index}"
            rawdir.mkdir(mode=0o700)
            command = [lock["python"], lock["ndlocr"], "--sourceimg", str(image_path), "--output", str(rawdir), "--device", "cpu", "--json-only"]
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=850)
            rawfile = rawdir / (image_path.stem + ".json")
            if rawfile.stat().st_size > 4 << 20:
                raise ValueError("unbounded OCR output")
            spans = parse_ndlocr(json.loads(rawfile.read_text()), *image.size)
        positioned.append({"page_id": page, "width": image.width, "height": image.height, "spans": spans})
        for role, suffix, body, media in [("ocr-text", "txt", "".join(s["text"] for s in spans), "text/plain; charset=utf-8"),
                                         ("positioned-text", "json", json.dumps(positioned[-1], ensure_ascii=False), "application/json")]:
            name = f"page-{index}.{suffix}"
            (output/name).write_text(body)
            artifacts.append({"id": f"artifact_ocr_{index}_{suffix}", "role": role, "path": name, "media_type": media,
                              "source_pages": [page], "parents": [os.environ["SCANNER_EVAL_ORIGINAL_ID"]],
                              "algorithm": engine, "algorithm_version": lock["version"]})
    request = {"pages": positioned, "images": [str(output/f"input-{i}.png") for i in range(len(pages))],
               "font": lock["font"], "output": str(output/"searchable.pdf")}
    if source.read_bytes()[:5] == b"%PDF-":
        request["source_pdf"] = str(source)
    subprocess.run([lock["compositor"]], input=json.dumps(request).encode(), check=True, stdout=subprocess.DEVNULL, timeout=60)
    artifacts.append({"id": "artifact_ocr_pdf_001", "role": "pdf", "path": "searchable.pdf", "media_type": "application/pdf",
                      "source_pages": pages, "parents": [os.environ["SCANNER_EVAL_ORIGINAL_ID"]],
                      "algorithm": "common-pdfassemble", "algorithm_version": "1"})
    (output/"result.json").write_text(json.dumps({"format": "scanner-eval-candidate-result", "version": 1,
                                               "candidate_id": engine, "artifacts": artifacts}))


if __name__ == "__main__":
    main()
