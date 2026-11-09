#!/usr/bin/env python3
"""Render public OCR sources; use scanner-eval for execution and measurement."""
import argparse
import hashlib
import io
import json
import shutil
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont, __version__ as pillow_version


FONT_SHA256 = "503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e"
FONT_SOURCE = "https://ipafont.ipa.go.jp/old/ipafont/download.html"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def generate(font_path, expected_hash, output_root):
    if expected_hash != FONT_SHA256 or digest(font_path.read_bytes()) != expected_hash:
        raise ValueError("unapproved font identity")
    font = ImageFont.truetype(str(font_path), 40)
    repo = Path(__file__).resolve().parents[3]
    base = json.loads((repo / "scripts/fixtures/evaluation/catalog.json").read_text())
    if output_root.exists() and any(output_root.iterdir()):
        raise ValueError("output root must be empty")
    output_root.mkdir(mode=0o700, parents=True, exist_ok=True)
    (output_root / "evaluation").mkdir()
    (output_root / "ocr").mkdir()
    for fixture in base["fixtures"]:
        source = fixture["recipe"].get("source")
        if source:
            destination = output_root / source
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(repo / "scripts/fixtures" / source, destination)
    entries = []
    layouts = {
        "horizontal": [("日本語の書類", 80, 80, ""), ("検索可能な文書", 80, 160, "")],
        "vertical": [(char, x, 80 + i * 52, "")
                     for x, text in [(680, "日本語縦書"), (560, "右から左へ")]
                     for i, char in enumerate(text)],
        "mixed": [("日本語 OCR Test 2026", 80, 80, ""), ("金額 １２３円 / 123", 80, 180, "")],
        "columns": [("右欄の先頭", 480, 80, ""), ("右欄の末尾", 480, 180, ""),
                    ("左欄の先頭", 80, 80, ""), ("左欄の末尾", 80, 180, "")],
        "table": [("品名", 90, 90, "r1c1"), ("金額", 450, 90, "r1c2"),
                  ("書籍", 90, 230, "r2c1"), ("1200円", 450, 230, "r2c2")],
        "wrapped": [(char, 80, 80 + i * 60, "") for i, char in enumerate("一文字改行")],
        "blank": [],
        "two-page": [("第一頁固有甲", 80, 80, "")],
    }
    for layout, definitions in layouts.items():
        fixture_id = "fixture_ocr_" + layout.replace("-", "_") + "_001"
        pages, bodies = [], []
        page_definitions = [definitions]
        if layout == "two-page":
            page_definitions.append([("第二頁固有乙", 80, 80, "")])
        for index, spans in enumerate(page_definitions, 1):
            page_id = "page_ocr_" + layout.replace("-", "_") + f"_00{index}"
            image = Image.new("RGB", (900, 700), "white")
            draw = ImageDraw.Draw(image)
            if layout == "table":
                for x in (70, 420, 800):
                    draw.line((x, 70, x, 340), fill="black", width=2)
                for y in (70, 200, 340):
                    draw.line((70, y, 800, y), fill="black", width=2)
            expected = []
            for order, (text, x, y, cell) in enumerate(spans):
                box = draw.textbbox((x, y), text, font=font, anchor="lt")
                draw.text((x, y), text, font=font, fill="black", anchor="lt")
                expected.append({"text": text, "order": order,
                                 "box": [box[0], box[1], box[2] - box[0], box[3] - box[1]],
                                 **({"cell": cell} if cell else {})})
            stream = io.BytesIO()
            image.save(stream, format="PNG", optimize=False, compress_level=9)
            body = stream.getvalue()
            source = f"ocr/{page_id}.png"
            (output_root / source).write_bytes(body)
            bodies.append((source, body))
            pages.append({"page_id": page_id, "source_sha256": digest(body),
                          "width": 900, "height": 700, "blank": not expected, "spans": expected})
        # A multi-page image fixture is represented by a lossless image-only PDF.
        if len(bodies) == 2:
            body = image_pdf([data for _, data in bodies])
            source = f"ocr/{fixture_id}.pdf"
            (output_root / source).write_bytes(body)
            media = "application/pdf"
        else:
            source, body = bodies[0]
            media = "image/png"
        base["fixtures"].append({"id": fixture_id, "source_class": "public_synthetic",
                                 "privacy": "public-synthetic", "media_type": media,
                                 "page_ids": [p["page_id"] for p in pages], "languages": ["ja", "en"],
                                 "document_type": "a4-text", "expected_outputs": ["ocr-text", "positioned-text", "pdf"],
                                 "sha256": digest(body), "bytes": len(body),
                                 "recipe": {"kind": "copy", "source": source}})
        entries.append({"fixture_id": fixture_id, "fixture_sha256": digest(body),
                        "layout": layout, "pages": pages})
    oracle = {"format": "scanner-eval-ocr-oracles", "version": 1,
              "normalization": "NFC", "units": "unicode-code-points",
              "font": {"name": "IPA Gothic 003.03", "sha256": FONT_SHA256,
                       "source": FONT_SOURCE, "license": "IPA-1"},
              "renderer": {"name": "Pillow", "version": pillow_version}, "entries": entries}
    write_json(output_root / "evaluation/ocr-catalog.json", base)
    write_json(output_root / "evaluation/ocr-oracles.json", oracle)


def image_pdf(pngs):
    """Fixed image-only PDF bytes, without date/random metadata or font embedding."""
    import zlib
    objects = [b"<< /Type /Catalog /Pages 2 0 R >>", b""]
    kids = []
    for png in pngs:
        image = Image.open(io.BytesIO(png)).convert("RGB")
        page = len(objects) + 1
        kids.append(f"{page} 0 R")
        objects.append(f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 900 700] /Resources << /XObject << /I {page+1} 0 R >> >> /Contents {page+2} 0 R >>".encode())
        data = zlib.compress(image.tobytes(), 9)
        objects.append(f"<< /Type /XObject /Subtype /Image /Width 900 /Height 700 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length {len(data)} >>\nstream\n".encode() + data + b"\nendstream")
        content = b"q 900 0 0 700 0 0 cm /I Do Q"
        objects.append(f"<< /Length {len(content)} >>\nstream\n".encode() + content + b"\nendstream")
    objects[1] = f"<< /Type /Pages /Kids [{' '.join(kids)}] /Count {len(kids)} >>".encode()
    output = bytearray(b"%PDF-1.4\n")
    offsets = [0]
    for i, obj in enumerate(objects, 1):
        offsets.append(len(output))
        output.extend(f"{i} 0 obj\n".encode() + obj + b"\nendobj\n")
    start = len(output)
    output.extend(f"xref\n0 {len(offsets)}\n0000000000 65535 f \n".encode())
    for offset in offsets[1:]:
        output.extend(f"{offset:010d} 00000 n \n".encode())
    output.extend(f"trailer << /Size {len(offsets)} /Root 1 0 R >>\nstartxref\n{start}\n%%EOF\n".encode())
    return bytes(output)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--font", required=True, type=Path)
    parser.add_argument("--font-sha256", required=True)
    parser.add_argument("--output-root", required=True, type=Path)
    args = parser.parse_args()
    try:
        generate(args.font, args.font_sha256, args.output_root)
    except (ValueError, OSError):
        parser.exit(1, "OCR corpus unavailable\n")
