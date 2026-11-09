# One fixed Docling CPU evaluation candidate

This is evaluation-only. It does not register a production provider or change scan,
OCR, save, model routing, or adoption settings. The frozen Phase46 table, catalog,
oracle and retained Tesseract5.5.4 must verify before acquisition. Parser fixtures
prove validation only; they are never measured candidate quality.

Use an owner-only new directory outside source:

```sh
python3 scripts/eval/structure/prepare_candidate.py --root "$STRUCTURE_ROOT" --phase46-root "$PHASE46_ROOT" --catalog scripts/fixtures/evaluation/ocr-catalog.json --oracle scripts/fixtures/evaluation/ocr-oracles.json
python3 -m unittest discover -s scripts/eval/structure -p 'test_*.py' -v
```

`--wheelhouse PATH` reuses previously downloaded wheels, rechecking every byte
against its exact versioned official index; it never trusts ambient packages.
Keep dependency-resolution.log and its exact command with reused inputs. Only
Docling source v2.149.0/8103d817983eeaaa2234d110072fa70d5c59a075,
docling-slim2.149.0 with convert-core/format-pdf/models-local, docling-core2.106.0,
docling-ibm-models4.0.4, TableFormer accurate fc0f2d45e2218ea24bce5045f58a389aed16dc23
and Heron8f39ad3c0b4c58e9c2d2c84a38465abf757272d8 are admitted. CPU wheels are
torch2.9.1+cpu and torchvision0.24.1+cpu from the official PyTorch CPU index;
all other wheels are checked against exact PyPI JSON. The research package audit
remains SUS; positive official provenance does not rewrite that heuristic.

Wheel METADATA and notice bytes are retained. Missing bundled notices can be
supplemented by the exact official source: latex2mathml3.81.1 annotated tag resolves
to fb724a91658cf41d2fad0192da0845b05446f208 (MIT LICENSE), and tokenizers0.23.3
contains its Apache license in the hash-verified PyPI sdist. This is not a claim
that an omitted wheel notice prohibits execution. Model cards and exact configs
are separate identities; unacquired weights/terms are not verified by metadata.

The wrapper uses a private mount/PID/network/user namespace, UID/GID10001,
read-only input/runtime mounts, empty explicit environment, no-new-privileges and
zero capability sets. Apply limits after namespace setup: CPU900s, RSS2GiB,
file1GiB,64 processes and256 descriptors, with two computation threads and one
page/process. Linux RLIMIT_RSS is only an observable limit; scanner-eval's actual
process-tree RSS/disk monitor is required for inference. `/tmp` is bounded to1GiB;
the harness also monitors writable output. No isolation or identity check can be
bypassed to continue. No network, automatic model downloads, VLM, enrichment,
remote inference, second candidate or implicit fallback is supported.

Only after preparation and a passing isolation self-check may the existing harness
run the prepared candidate (use unique private directories):

```sh
go run ./cmd/scanner-eval benchmark --catalog "$STRUCTURE_CATALOG" --candidate "$STRUCTURE_ROOT/candidate.json" --fixture fixture_ocr_table_001 --output "$STRUCTURE_RUN" --cold 1 --warm 0 --timeout-seconds 900 --max-rss-mib 2048 --max-temp-disk-mib 1024
```

Do not launch the adapter directly for measurement: the harness owns elapsed time,
peak RSS, sampled disk, source identity and final artifact hash/size. The adapter
emits only the `structure` role, with the original parent and exact source page.
Raw Docling output stays in private evidence and is separate from recognized text
and positioned-text artifacts.

The normalized format is `scanner-eval-structure` version1. `source` binds the
original artifact ID, SHA256, page ID and pixel dimensions; `runtime_lock_sha256`
binds the actual adapter/dependency/model/config snapshot. `blocks` retain Docling
`self_ref` IDs and tree order, parent IDs, labels, observed text and boxes. `cells`
use stable table-ref/list-index IDs and retain row/column offsets and spans.
Bottom-left coordinates become top-left source pixels exactly once, using the
observed Docling page dimensions. Missing geometry is `null` with
`geometry_status: unavailable`; absent cells stay absent with `cells_status:
unavailable`. No boxes, blank grid cells, text, regex tables or summaries are
created to match an oracle. JSON4MiB, total text2MiB, individual text64KiB,
4096 blocks,8192 cells,65536 JSON nodes and depth32 bound parser work. Foreign
pages, duplicate/cyclic/orphan references, overlapping cell spans, nonfinite or
out-of-bounds coordinates and candidate-declared source/hash/size authority fail.

Acquisition, identity/license, isolation, offline-install, model, timeout and
resource failures remain failures. Exit2 plus a `scanner-eval-structure-disposition`
receipt binds the attempted pins, frozen inputs, failure stage and actual command
evidence. Validate this receipt before downstream independent parser/schema work.
It never establishes inference success, resource measurements, table improvement
or adoption. Keep failed attempts and raw logs outside Git; do not retry with a
different candidate, model, mode or larger ceilings.
