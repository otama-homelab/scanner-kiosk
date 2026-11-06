# Synthetic OCR input

`ocr-ja-en-300dpi.png` is a generated 2480 × 3508 RGB page at 300 dpi. It contains
only these artificial strings, rendered at (180, 200/400/600) with an 80 px font:

- 日本語の書類
- Scanner OCR 2026
- テスト文書 300 dpi

Generation uses Pillow and the installed IPA Gothic `ipag.ttf` on a white canvas.
The font file is not distributed in this fixture; its SHA256 at generation was
recorded in the task evidence. This is a clean input for transport, rotation and
PDF composition checks, not a physical-scanner OCR accuracy benchmark.

The actual-image tracer uses the same original pixels for two pages, with export
rotations 0° and 90°. Keep the expected Japanese/English text literal when
independently inspecting the output; do not normalize recognition errors away.
