# Real Japanese OCR evaluation

This directory supplies thin real-engine adapters for the existing `scanner-eval`
protocol. The frozen corpus/oracle lives in `scripts/fixtures/evaluation/ocr-*.json`.
It contains only public synthetic Japanese sources; no household quality claim is
inferred. NDLOCR is evaluation-only, not a production provider.

Use Tesseract **5.5.4**, retaining independently versioned dictionaries. NDLOCR-Lite
**1.3.0-preview** is official commit
`c1c9669177642daed98be14240197d971621b16a`. Its source checkout reports
`0.0.0.dev0`; identify it by commit and Git blob hashes, not that generated version
string. Verify all four actual ONNX blobs, YAML dictionaries/config, Python source,
and the official requirements before execution. Source is CC-BY-4.0; model origins
and dependencies have separate terms. Unresolved licenses remain unresolved.

Preparation uses owner-only local storage. Download the immutable official GitHub
archive, check every file's Git blob identity against the official recursive tree,
download the exact official requirements as wheels, check each wheel against its
versioned PyPI SHA256, and retain wheel METADATA/license files. Install offline in a
new venv with `--no-index --find-links WHEELS --require-hashes -r requirements.lock`.
Do not change a failing package name or version. Keep the inspected source, weights,
wheel artifacts, source/blob identities and acquisition receipts together.

`prepare_candidates.prepare` accepts those inspected byte-bound identities and
creates a configuration/lock. `candidate.py` verifies the locked identities before
recognition, normalizes bounded TSV/NDLOCR JSON without invented character boxes,
and calls `compose.go`, which uses the production `pdftextlayer.RenderPositioned`
and `pdfassemble.BuildWithLayers` for both engines. Build the glue with
`go build -o "$ROOT/compose" scripts/eval/ocr/compose.go`. The approved font is IPA
Gothic003.03, SHA256
`503af4a8b84d1079b8e2e358dc7f7a7fb8cb7a1f212f35eaef6782dbfc75a55e`, IPA-1.
Keep a venv executable regular (the harness rejects executable symlinks), and keep
the verified standalone Tesseract helper's basename and runtime root intact.

For each exact engine/fixture pair, resolve these variables to unique directories:

```sh
go run ./cmd/scanner-eval run --catalog "$SCANNER_OCR_CATALOG" --candidate "$SCANNER_OCR_CANDIDATE" --fixture "$SCANNER_OCR_FIXTURE" --output "$SCANNER_OCR_RUN_DIR"
go run ./cmd/scanner-eval benchmark --catalog "$SCANNER_OCR_CATALOG" --candidate "$SCANNER_OCR_CANDIDATE" --fixture "$SCANNER_OCR_FIXTURE" --output "$SCANNER_OCR_SAMPLE_DIR" --cold 1 --warm 2 --timeout-seconds 900 --max-rss-mib 2048 --max-temp-disk-mib 1024
```

Only after every real measurement, create a pairs JSON indexing the completed
receipts. `prepare_candidates.py --root ROOT --oracle ORACLE --pairs PAIRS
--output MANIFEST` hashes existing bounded private-root-relative files; it never
launches or times engines. Each sample has its own receipt and artifact catalog
identity. PDF hashes can differ between samples. Never recalculate hashes to hide
subsequent mutation. Run `scanner-eval analyze-ocr` with both required engine IDs
and `--require-complete-evidence` against this final manifest.

Raw OCR paths/text and all raw measurement artifacts stay private. The public
comparison contains bounded metrics/opaque hashes. Completeness, recognition,
reading order/table regions, PDF/source-pixel/geometry, resource and license
findings are independent: measured failed quality remains failed, and unresolved
license does not become adoption approval. Process-cold/warm here each start a
fresh process; they do not promise a resident model cache.

Run adapter checks with:
`python3 -m unittest discover -s scripts/eval/ocr -p test_candidates.py -v`.
