# Evaluation harness

`scanner-eval` is an evaluation-only command. It does not call the product API,
UI, scanner, document store or worker runtime. Phase 44 proves fixture, artifact,
validation and measurement contracts; it does not accept an OCR/correction
candidate or establish production resource budgets.

## Prerequisites

- Linux with Go 1.25 or newer and the repository's existing Go modules.
- Poppler `pdfinfo`, `pdfimages`, `pdftotext` and `pdftoppm`.
- GNU `/usr/bin/time` with `%e`, `%U`, `%S` and `%M`.
- A candidate executable owned by the administrator and represented by a local
  JSON configuration file.

The observed evaluation host used Poppler 24.02.0 and GNU Time from the host
package. The commands below are repository-relative and write only to temporary
or explicitly selected private directories.

## Fixtures

Generate the committed synthetic corpus twice into separate empty directories:

```sh
go run ./cmd/scanner-eval generate-fixtures \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --output /tmp/scanner-eval-fixtures-a \
  --manifest /tmp/scanner-eval-manifest-a.json

go run ./cmd/scanner-eval generate-fixtures \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --output /tmp/scanner-eval-fixtures-b \
  --manifest /tmp/scanner-eval-manifest-b.json

cmp /tmp/scanner-eval-manifest-a.json /tmp/scanner-eval-manifest-b.json
```

List the public corpus:

```sh
go run ./cmd/scanner-eval list-fixtures \
  --catalog scripts/fixtures/evaluation/catalog.json
```

The output is sorted by fixture ID and includes the fixture ID, privacy class,
media type, page IDs, languages, document class, expected output roles, expected
failure class, SHA256, byte size and availability. The public corpus contains 13
fixed synthetic classes: A4 ja/en text, receipt, card, photo, table/form, vertical
text, faint print, skew, shadow, white-on-white, invalid, oversized and
dependency failure.

### Owner-private fixtures

Keep private bytes and metadata outside the source tree. A local private catalog
uses this shape:

```json
{
  "format": "scanner-eval-fixtures",
  "version": 1,
  "fixtures": [
    {
      "id": "private_household_form_001",
      "privacy": "owner-private",
      "media_type": "image/png",
      "page_ids": ["page_private_001"],
      "languages": ["ja"],
      "document_type": "form",
      "expected_outputs": ["ocr-text", "positioned-text", "pdf"],
      "relative_path": "forms/household.png",
      "sha256": "<64 lowercase hex characters>",
      "bytes": 12345
    }
  ]
}
```

Attach it at runtime:

```sh
go run ./cmd/scanner-eval list-fixtures \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --private-catalog /private/eval/private-catalog.json \
  --private-root /private/eval/documents
```

The private root must be absolute. Resolution is confined beneath that root;
symlinks, non-regular files, public permissions, size/hash mismatches and missing
files remain `unavailable`. Source files are not copied into the repository.
Private paths, bytes and recognized text are not written to list output, errors,
artifact catalogs or benchmark receipts.

## Candidate protocol

The administrator supplies a candidate configuration. Required provenance is
the candidate ID/version, executable path and SHA256, fixed argv, source pins
with licenses, every declared dependency/model pin with name, version, license
and SHA256, and an explicit `operator_steps` array. A source pin may omit its
archive SHA256. An empty `operator_steps` array means no operator action was
required; an omitted field is invalid evidence:

```json
{
  "format": "scanner-eval-candidate",
  "version": 1,
  "candidate_id": "candidate_tesseract_001",
  "candidate_version": "5.5.4",
  "executable": "/opt/eval/tesseract-candidate",
  "executable_sha256": "<64 lowercase hex characters>",
  "argv": ["--mode", "searchable-pdf"],
  "environment": ["CANDIDATE_CACHE=/private/eval/cache"],
  "parameters": {"lang": "jpn+eng"},
  "sources": [
    {
      "name": "tesseract",
      "version": "5.5.4",
      "license": "Apache-2.0",
      "sha256": "<optional source/archive hash>"
    }
  ],
  "dependencies": [
    {
      "name": "jpn.traineddata",
      "version": "pinned-version",
      "license": "reviewed-license",
      "sha256": "<64 lowercase hex characters>"
    }
  ],
  "operator_steps": ["prepare the candidate cache before the run"]
}
```

Use `"operator_steps": []` when no operator action is required. The harness
preserves the explicit empty array in run and benchmark receipts. A hand-written
receipt from an earlier harness version that omits `operator_steps` is rejected
and must be regenerated rather than interpreted as an empty array.

The candidate receives only an explicit environment containing `PATH`,
`TMPDIR` and these fixed values:

- `SCANNER_EVAL_INPUT`
- `SCANNER_EVAL_OUTPUT`
- `SCANNER_EVAL_ORIGINAL_ID`
- `SCANNER_EVAL_SOURCE_PAGES`

The process does not inherit the caller's environment. Configuration entries
cannot override the `SCANNER_EVAL_*` names. The executable is hash-verified
before launch.

The candidate writes `result.json` beneath `SCANNER_EVAL_OUTPUT`:

```json
{
  "format": "scanner-eval-candidate-result",
  "version": 1,
  "candidate_id": "candidate_tesseract_001",
  "artifacts": [
    {
      "id": "artifact_candidate_ocr_001",
      "role": "ocr-text",
      "path": "text/ocr.txt",
      "media_type": "text/plain; charset=utf-8",
      "source_pages": ["page_a4_ja_en_001"],
      "parents": ["artifact_original_..."],
      "algorithm": "tesseract",
      "algorithm_version": "5.5.4",
      "parameters": {"lang": "jpn+eng"}
    }
  ]
}
```

Paths are relative to the candidate output root and cannot traverse or point to
symlinks/non-regular files. IDs and paths are unique. Source pages must belong to
the fixture. Parent IDs must exist and form an acyclic graph. Roles/media types
are closed:

| Role | Media type |
| --- | --- |
| `correction` | `image/png` |
| `ocr-text` | `text/plain; charset=utf-8` |
| `positioned-text` | `application/json` |
| `structure` | `application/json` |
| `generated` | `application/json` or `text/plain; charset=utf-8` |
| `pdf` | `application/pdf` |

The harness creates `original` itself and verifies its bytes before and after the
candidate. It computes every SHA256 and byte size from normalized copies;
candidate-declared hashes are optional confirmation and can only fail the run.
Generated content never substitutes for OCR, positioned text, structure or PDF.

Run one candidate:

```sh
go run ./cmd/scanner-eval run \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --candidate /private/eval/candidate.json \
  --fixture fixture_a4_ja_en_001 \
  --output /private/eval/runs/run-001
```

The output directory must not already exist. `artifact-catalog.json` records the
original, derived artifacts, missing requested outputs and validation state.
Incomplete or failed runs exit non-zero while retaining machine-readable evidence.

## Independent PDF checks

When a PDF artifact is present, the harness uses fixed trusted paths for all four
Poppler tools and records each resolved path and version. For every fixture page
it checks:

- page count and ordered fixture-page binding;
- MediaBox geometry and rotation;
- presence, count and encoding of embedded images;
- per-page extracted-text SHA256/byte size and public expected-text matches;
- two 72dpi PNG renders with matching hashes;
- decoded lossless source/embedded pixel equality when the caller requests that
  comparison.

Raw extracted text is not copied into receipts. Corrupt PDFs, wrong page counts,
missing images, failed tool execution and unavailable tools are `failed` or
`unavailable`, never `passed`.

## Cold/warm benchmark

```sh
go run ./cmd/scanner-eval benchmark \
  --catalog scripts/fixtures/evaluation/catalog.json \
  --candidate /private/eval/candidate.json \
  --fixture fixture_photo_001 \
  --output /private/eval/benchmarks/benchmark-001 \
  --cold 1 \
  --warm 5 \
  --timeout-seconds 900 \
  --max-rss-mib 2048 \
  --max-temp-disk-mib 1024
```

Defaults are 1 cold and 5 warm repetitions, a 900-second ceiling, 2GiB peak-RSS
ceiling and 1GiB sampled-temporary-disk ceiling. At least one repetition is
required and the total is capped at 100.

“Cold” means the first requested invocation in this benchmark run. “Warm” means
subsequent invocations after the same fixture/candidate setup has run once. The
harness does not drop operating-system page caches, so this is a sequence label,
not a hardware-cold guarantee.

Each repetition uses the Plan 44-02 candidate and validator path. The coordinator
measures wall time around the supervised run. `/usr/bin/time` reports user CPU,
system CPU and peak RSS (`%M`, KiB converted to bytes). Logical file bytes under
the private run root are sampled every 20ms; this can miss transient or unlinked
files and excludes directory/journal overhead.

Nearest-rank percentiles are computed separately per mode:

```text
rank = ceil(percentile / 100 * completed_sample_count)
pXX  = sorted_wall_millis[rank - 1]
```

No percentile is emitted for a mode with zero completed samples. Failed samples
remain in sequence order and are not mixed into cold/warm timing populations.

## Receipts and comparison

Each benchmark creates a new mode-0700 run directory and writes:

- `receipts/NNN-cold.json` or `receipts/NNN-warm.json` for each sample;
- `repetitions/NNN-mode/artifact-catalog.json` for normalized artifacts;
- `benchmark.json` after all requested repetitions finish.

All JSON files use exclusive create and are never overwritten. Failed
repetitions preserve their receipts; the summary is still incomplete and exits
non-zero. The summary records run/fixture/candidate identity, config/executable/
fixture hashes, explicit environment names, command template, candidate/model/
dependency/source licenses and versions, parameters, operator steps, observer
tool/hash/version, resource ceilings, raw ordered samples and separate cold/warm
metrics. Each sample records wall/CPU/RSS/sampled-disk values, exit code/class,
validation state, source hashes before/after and relative links to artifacts and
its receipt.

Compare summaries or their containing directories:

```sh
go run ./cmd/scanner-eval compare \
  --receipt /private/eval/benchmarks/benchmark-001 \
  --receipt /private/eval/benchmarks/benchmark-002 \
  --output /private/eval/comparison-001.json
```

`compare` rejects unknown schemas, duplicate run IDs, missing provenance and one
candidate ID with conflicting executable/config identities. Multiple fixtures
are allowed, but every distinct fixture ID, fixture SHA256 and complete benchmark
settings tuple is emitted as a separate group:

```json
{
  "format": "scanner-eval-comparison",
  "version": 1,
  "groups": [
    {
      "fixture_id": "fixture_photo_001",
      "fixture_sha256": "<64 lowercase hex characters>",
      "settings": {
        "cold_repetitions": 1,
        "warm_repetitions": 5,
        "timeout_seconds": 900,
        "max_rss_bytes": 2147483648,
        "max_temp_disk_bytes": 1073741824
      },
      "entries": [
        {
          "candidate_id": "candidate_tesseract_001",
          "candidate_version": "5.5.4",
          "mode": "cold",
          "status": "complete",
          "samples": 1,
          "p50_millis": 1234,
          "p95_millis": 1234,
          "failure_classes": []
        }
      ]
    }
  ]
}
```

The group owns the fixture and settings identity; entries own only candidate,
mode, status, metrics and failure classes. Cold and warm rows are distinct, and
failure classes stay with the mode that produced them. A same-ID fixture with a
different hash, or any timeout/RSS/temp-disk difference, forms another group
instead of merging into one percentile population. Separate groups are not one
fair population and must not be aggregated as if they shared conditions.
A shorter wall time is not a quality or adoption decision.

## Command reference

All five subcommands emit machine-readable JSON on stdout unless noted.

| Command | Required flags | Optional flags |
| --- | --- | --- |
| `generate-fixtures` | `--catalog`, `--output`, `--manifest` | none |
| `list-fixtures` | `--catalog` | `--private-catalog` and `--private-root` together |
| `run` | `--catalog`, `--candidate`, `--fixture`, `--output` | none |
| `benchmark` | `--catalog`, `--candidate`, `--fixture`, `--output` | `--cold`, `--warm`, `--timeout-seconds`, `--max-rss-mib`, `--max-temp-disk-mib` |
| `compare` | one or more `--receipt` | `--output` |

Benchmark defaults are cold 1, warm 5, timeout 900 seconds, peak RSS 2048 MiB
and sampled temporary disk 1024 MiB. `generate-fixtures` and `run` write their
documented files instead of printing a JSON document; every command still uses
a non-zero exit for invalid, unavailable or incomplete evidence.

## Proof limits

Synthetic and private-fixture runs prove the harness contracts and can compare
candidate behavior under recorded conditions. They do not establish real-document
OCR quality, correction quality, reading-order accuracy, scanner behavior,
container isolation, mobile UX, external-service behavior, production resource
budgets or candidate adoption. Later phases must use the same fixture, artifact,
validation and metric contracts and must measure any missing corpus or runtime
class before making an acceptance decision.
