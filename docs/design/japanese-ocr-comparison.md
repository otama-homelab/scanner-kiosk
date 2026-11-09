# Japanese OCR comparison — measured synthetic corpus

On 2026-11-10 JST, the existing scanner-eval harness completed **16 engine/fixture
pairs**, each with a standalone run and cold1/warm2 benchmark: **64 actual runs**.
The final analyze-ocr completeness gate reopened the byte-bound receipts, original
sources and every sample's own artifacts, returning `complete` with64 rows.

Tesseract5.5.4 used its retained native runtime and independently versioned
5.5.2-r0 Japanese/English dictionaries. NDLOCR-Lite1.3.0 is the official **preview**,
commit `c1c9669177642daed98be14240197d971621b16a`; all93 Git blobs were checked
against the pinned official tree, including four actual ONNX weights. Its source
version module reports0.0.0.dev0; the commit is authoritative. All33 dependency
wheels were checked against their exact-version PyPI SHA256 and installed offline
in an isolated environment. No model substitution or production integration occurred.

Both adapters use the production positioned-text renderer and PDF compositor.
Image inputs retain their original lossless pixels. The two-page PDF retains its
original page objects through the production importer/compositor, rather than
using recognition raster images as replacement pages. The approved IPA Gothic
003.03 font is SHA256
`503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e` (IPA-1).

The independent oracle uses NFC Unicode code points without width/whitespace
folding. Region/cell matching uses source geometry independently of transcription.
Large detected text lines do not imply character-sized boxes; a correct vertical
transcript can therefore still fail the fine region requirement. No threshold was
changed to improve these results.

| Frozen layout | Tesseract text errors | NDLOCR text errors | Tesseract matched regions | NDLOCR matched regions |
|---|---:|---:|---:|---:|
| Horizontal |0|0|2/2|2/2|
| Upright vertical |10|0|0/10|0/10|
| Mixed Japanese/English/fullwidth |12|9|0/2|0/2|
| Right/left columns |6|6|4/4|0/4|
| Text table |10|2|3/4|3/4|
| One character per line |5|0|0/5|0/5|
| Blank |0|0|0/0|0/0|
| Two-page PDF |0|0|2/2|2/2|

The table reports standalone results; all cold/warm runs remain separately recorded.
Tesseract had75 reading-order inversions on columns and12 on the table; NDLOCR
had0 matched-region inversions, which does **not** prove correct order where its
regions did not match. Table cell matches were3/4 for both. Only horizontal,
blank and two-page fixtures passed every oracle quality dimension for both engines.

All64 independently reopened PDFs passed page-bound recognized-text extraction,
geometry, source-pixel and render-stability checks. This proves faithful PDF
carriage of actual recognition, not correct recognition. Native vertical/mixed
search-box geometry is a separate production test. Exact validator tool hashes
(Poppler24.02.0 for this comparison) are bound in the manifest; they are distinct
from the official25.12.0 bundled worker raster runtime.

| Warm process measurement | Tesseract5.5.4 | NDLOCR-Lite1.3.0-preview |
|---|---:|---:|
| Single-page p50 range |974–1171ms|7280–7402ms|
| Single-page p95 range |1035–1253ms|7371–7483ms|
| Two-page p50 / p95 |1906 /1986ms|14674 /16618ms|
| Maximum warm peak RSS |195.3MiB|622.0MiB|
| Maximum warm sampled temporary disk |11.94MiB|11.94MiB|

Every benchmark stayed within900s,2048MiB RSS and1024MiB sampled temporary-disk
ceilings. Two warm samples only give an observed pair/order statistic, not a robust
population percentile. Each sample starts a new process; no resident-model speed
claim follows. Preparation and external read-only host activity are outside the
candidate timer. Operator steps and full cold/warm measurements are retained.

Tesseract/font/source notices are retained with their component identities.
NDLOCR source is CC-BY-4.0, but separate model-origin/redistribution and dependency
license review remains **unresolved**. Every downloaded wheel retains METADATA
and license files. Neither an official repository nor a successful run converts
unreviewed component terms into clearance. **Retain Tesseract as baseline; defer
NDLOCR production adoption** pending those terms and more representative quality
evidence. The measured vertical/wrapped transcription improvement alone does not
meet fine positioning, table and mixed-text requirements.

Manifest SHA256:
`0ffb6c24b6504d228852f43551ae5d2ecb846022433d348ce93df86ff93a040d`.
Oracle SHA256:
`e5f91c00ff449274611b24fcfbfe431188b82903cd11ba94188f9152de4f435c`.
The private local evidence includes exact source/model/config/dictionary/font/tool
identities, standalone receipts, cold/warm receipts and per-sample artifact hashes.
PDF hashes are allowed to differ between samples. Failed preliminary attempts are
retained separately; a PNG-only source validator gap was fixed by a failing real
PDF-retention test before the final matrix. No acceptance thresholds were weakened.

Reproduce with [the adapter instructions](../../scripts/eval/ocr/README.md).
Raw artifacts stay in owner-only local storage; this public report contains only
synthetic metrics and opaque hashes. No private household documents, physical
scanner, external OCR provider, deployment or publication was used. Independent
phase verification owns requirement acceptance.
