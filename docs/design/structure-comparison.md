# Structure comparison — Docling unavailable, adoption deferred

On 2026-11-10 JST the single pinned **Docling CPU 2.149.0** attempt ended
**unavailable at isolation**, with exit **32** from the actual read-only bind
remount of `/usr`. The retained probe's exact command, stderr and referenced
files were reopened and hash-checked. No further confinement attempt, candidate,
model, threshold or resource-limit change was made.

**Decision: defer / non-adoption.** There are **zero candidate measurement rows**.
This is an execution prerequisite failure, not a quality rejection. No runtime
installation, weight acquisition, model import or inference started. There is no
Docling table artifact, cold run, warm run or p50/p95 to report. The unchanged
ceilings of 900s, 2048MiB RSS and 1024MiB sampled temporary disk describe the
intended experiment; none is an observed candidate measurement.

| Independent Docling dimension | Result |
|---|---|
| Text errors / transcript fidelity | unresolved |
| Reading order | unresolved |
| Matched regions / geometry | unresolved |
| Matched cells / row-column topology | unresolved |
| Elapsed time / peak RSS / sampled temporary disk | unresolved |
| Installed dependency closure / complete model terms | unresolved |
| Generated summary, classification or filename quality | unresolved |

## Existing measured gap remains visible

The unchanged Phase 46 oracle and final 64-run comparison authorize this one
attempt. The analyzer retains all **16 existing standalone baseline rows** for
eight layouts and two engines, and requires their complete standalone/cold1/warm2
coverage and consistent fixture/config identities. These rows are **Phase 46
measurements**, not Docling output. Text errors and geometry stay separate:

| Frozen layout | Tesseract 5.5.4 text errors | NDLOCR-Lite 1.3.0 text errors | Tesseract matched regions | NDLOCR matched regions |
|---|---:|---:|---:|---:|
| Horizontal | 0 | 0 | 2/2 | 2/2 |
| Upright vertical | 10 | 0 | 0/10 | 0/10 |
| Mixed Japanese/English/fullwidth | 12 | 9 | 0/2 | 0/2 |
| Right/left columns | 6 | 6 | 4/4 | 0/4 |
| Text table | 10 | 2 | 3/4 | 3/4 |
| One character per line | 5 | 0 | 0/5 | 0/5 |
| Blank | 0 | 0 | 0/0 | 0/0 |
| Two-page PDF | 0 | 0 | 2/2 | 2/2 |

Both baseline engines matched **3/4 table cells**. Tesseract had **75**
matched-region reading-order inversions on columns and **12** on the table.
NDLOCR's zero matched-region inversions cannot establish correct order where
regions did not match. Nothing here resolves the vertical/wrapped positioning,
table or mixed-text gaps. Warm2 was actually observed only in Phase 46; it is a
two-process observation rather than a population guarantee or resident-model
speed claim. See [the original comparison](japanese-ocr-comparison.md).

## Identities and license observations

The attempted official Docling source is v2.149.0, commit
`8103d817983eeaaa2234d110072fa70d5c59a075`, with retained MIT source-license bytes.
The three direct package pins are docling-slim 2.149.0, docling-core 2.106.0 and
docling-ibm-models 4.0.4. The attempt acquired and verified 71 official wheels,
including torch 2.9.1+cpu and torchvision 0.24.1+cpu; installation did not occur.
Wheel notices and supplemental source-license bytes were retained, which does
not establish an installed closure or complete redistribution clearance.
The research **SUS** package-legitimacy heuristic is retained.

The unchanged configuration is CPU, two threads, accurate TableFormer, retained
Tesseract 5.5.4 with Japanese/English, one page/process, remote processing off and
enrichment off. TableFormer accurate revision is
`fc0f2d45e2218ea24bce5045f58a389aed16dc23`; Heron revision is
`8f39ad3c0b4c58e9c2d2c84a38465abf757272d8`. Config/model-card bytes were obtained;
weight bytes and complete model-license terms were not verified. Upstream
CDLA/Apache model-license declarations alone do not close that review.

| Retained evidence | SHA256 |
|---|---|
| Final isolation-unavailable disposition | `2dca9c05676811b9f25d7fc2e939b743a664dab73183b27b4803140d0c895d19` |
| Structure analysis manifest | `df71a24a9de1b84c4eb0a664f20a4eecfc7c1a0828683fd2063d047403df7d60` |
| Final structure comparison JSON | `7d068bb6cc92cc79c1c6770bb165e171f8a7d5d4959fe6183f47ee4e6f58a2e6` |
| Frozen OCR catalog | `2d9b0f17fe3802ec8284c23c17d78e5e36896ae0cb6a027a2fe2e980fbba0abc` |
| Frozen OCR oracle | `e5f91c00ff449274611b24fcfbfe431188b82903cd11ba94188f9152de4f435c` |
| Phase 46 final manifest | `0ffb6c24b6504d228852f43551ae5d2ecb846022433d348ce93df86ff93a040d` |
| Phase 46 final comparison | `63cc28a655ce140da3cb11b5d455d9bc8188871db527a592901e3bf2a25ee0e2` |
| Wheel inspections | `11ea858aa7f5d7ce705a9161b1c1ccdf8be600591006902ead71f4c633b11576` |
| Dependency lock | `fe84dcfa493f9a349be7241afd33293f98fcac2ac5c98c6d0d923186073a5afa` |
| Actual failed isolation probe | `92d0d57e5628c1cb1b292fff61d2dfeed65d66392084028e3e4873cbc86f8b58` |

The disposition binds the exact acquisition/operator steps and available source,
package, model/config and retained Tesseract identities. An expected weight hash
is not an acquired weight hash. Acquisition byte counts are not runtime resource
measurements. The attempt's [preparation instructions](../../scripts/eval/structure/README.md)
retain its boundaries; do not repeat inference or change confinement to reproduce
this analysis.

## Independent structure/generated boundary

The evaluation-only validators match the actual adapter's
`scanner-eval-structure` version 1: immutable source artifact/hash/page/dimensions,
runtime-lock hash, observed block tree/order/geometry and table cells/spans.
Missing geometry or cells remain unavailable. Duplicate, orphan, cyclic,
foreign-page, overlapping, out-of-bounds or oversized topology is rejected.
No text box is subdivided to improve an oracle score.

`scanner-eval-generated` version 1 has an explicit `generated` role, source
revision/artifact/hash, structure-artifact hash, algorithm/version, and nonempty
records. Each record is `summary`, `classification` or `filename` and cites
existing same-snapshot page/block/cell IDs. Generated-to-generated links,
missing/foreign/stale references and unknown/trailing JSON are rejected. The
artifact readers reopen hash/size-bound, owner-only files and enforce the
existing role/media/parent/algorithm metadata. Callers must supply their
authorized immutable snapshot; these evaluation helpers are not a new owner
authorization API. Generated content cannot satisfy structure, transcript,
positioned-text or PDF completeness.

These tests use synthetic boundary fixtures. They prove validation, not model
generation, generated quality or a shipped filename/summary/classification UI.
Actual product behavior belongs to subsequent plans. No household-quality claim
follows from the public synthetic corpus.

## Revalidation and limits

Run `scanner-eval analyze-structure --oracle <frozen-oracle> --catalog
<frozen-catalog> --manifest <owner-private-analysis-manifest> --require-candidate
docling-cpu-2.149.0 --require-complete-evidence --allow-validated-unavailable
--output <new-owner-only-output>`. Output creation is exclusive. The analyzer
reopens the pinned disposition, all six referenced files, actual probe/input
bindings, the frozen baseline comparison/manifest and the synthetic table source.
Public JSON contains hashes and synthetic aggregates without raw text or private
paths; errors are content-free.

This implementation deliberately accepts **only the recorded exact isolation
disposition**. Successful/mixed candidate evidence, another failure stage or
changed identities fail closed. A successful runtime/comparison acceptance
branch is **not implemented or verified**: it remains unresolved after the
actual Plan 01 failure, and needs its own real evidence before extension.

Production registration, OCR/correction/save defaults and adopted results are
unchanged. Phase 46 remains **human_needed** with its three unresolved human
advisories: comparison honesty, non-destructive explicit adoption, and faithful
source display. Physical-phone, screen-reader and household-document quality
acceptance remain unperformed. No private household documents, external
inference, scanner/GPU operations, deployment or push were used. Independent
verification owns requirement acceptance.
