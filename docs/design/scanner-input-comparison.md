# Scanner input comparison

This is a nonproduction experiment for Canon TS8130 input behavior. The shipped product continues to use explicit fixed-area SANE capture; direct eSCL and `ScanIntent` are not part of scanner admission.

## Arms

| Arm | Path | Region and processing |
| --- | --- | --- |
| C1 | existing `scanimage` / sane-airscan | Flatbed, fixed paper area, explicit color/DPI, operator-timed |
| E1 | `scanner-escl-eval` | direct eSCL fixed region with `MustHonor=true`, no Intent |
| E2 | `scanner-escl-eval` | same region plus one device-advertised Intent |

All arms use the same physical target, region, color and DPI. Other-model evidence is not TS8130 evidence. The helper requests the TS8130-advertised `image/jpeg` in both ScanSettings and HTTP Accept. It accepts `image/jpeg` as well as `image/png`; both are decoded under byte/pixel bounds and retained for comparison as PNG. JPEG media type, returned byte count and returned-byte SHA256 remain in the receipt, while the normalized PNG is the local image evidence.

## Target oracle

Create one owner-controlled oracle file. The target hash identifies the physical artwork; it is not a scanned-page hash.

```json
{
  "format": "scanner-escl-input-target",
  "version": 1,
  "target_sha256": "64 lowercase sha256 hex characters",
  "device": "canon-ts8130",
  "firmware": "recorded firmware",
  "region": {"x": 0, "y": 0, "width": 2480, "height": 3508},
  "resolution": 300,
  "color": "rgb",
  "background_rgb": [255, 255, 255],
  "edge_regions": [
    {"id": "top-left", "x": 0, "y": 0, "width": 0.1, "height": 0.1}
  ],
  "content_regions": [
    {"id": "centre-text", "x": 0.4, "y": 0.4, "width": 0.2, "height": 0.2}
  ]
}
```

Region coordinates and sizes are normalized to the returned image, with origin at top-left. Each has a unique bounded ID, positive width/height and stays inside 0..1. At least one edge and one content region are required. A C1 capture must contain foreground in every declared region.

Use an edge-marker target with content within 2mm of the intended boundary, four corner markers, one central text block, one faint area and a unique identifier. Do not commit private target images or scans.

## Receipts

Each arm receipt is `scanner-escl-input-comparison` version 1 and contains:

- `target_sha256`, `device`, `firmware`, and a positive global `sequence`;
- requested `region`, `resolution`, `color`, arm and any E2 Intent;
- original returned `media_type`, `bytes` and `sha256`, decoded `width`/`height`, and positive `elapsed_millis`;
- `image`, a clean path relative to the receipt directory.

`image` must name a regular bounded PNG beneath the receipt directory. Absolute paths, `.`/`..` traversal, symlinks, duplicate receipt paths, duplicate image references and images over 64 MiB or 80 million pixels are rejected. Repeated byte-identical captures are valid when they have distinct sequence values and image references. Receipts with unknown fields or trailing JSON are rejected.

## Capture schedule

Use at least three captures per arm, with global sequence values, alternating groups:

```text
1 C1
2 E1
3 E2
4 E2
5 E1
6 C1
7 E1
8 C1
9 E2
```

Every request is one explicit operation. A timeout, lost response or uncertain device state stops the sequence. Never automatically retry. At least three groups are required; every consecutive three-sequence group must contain exactly C1, E1 and E2.

## C1

Keep the scanned PNG and receipt in the same receipt directory. `--elapsed-millis` is required and must be the operator's monotonic measurement of the external fixed-area `scanimage` invocation. Zero or missing time is invalid.

```sh
go run ./cmd/scanner-escl-eval evaluate-c1 \
  --input /private/eval/ts8130/c1-01/capture.png \
  --region 0,0,2480,3508 \
  --resolution 300 --color rgb \
  --target /private/eval/ts8130/target.json \
  --sequence 1 --elapsed-millis 21390 \
  --receipt /private/eval/ts8130/c1-01/receipt.json
```

## E1/E2

```sh
go run ./cmd/scanner-escl-eval capture \
  --endpoint http://scanner.example/eSCL \
  --arm e1 \
  --region 0,0,2480,3508 \
  --resolution 300 --color rgb \
  --target /private/eval/ts8130/target.json \
  --sequence 2 \
  --output /private/eval/ts8130/e1-01 \
  --receipt /private/eval/ts8130/e1-01/receipt.json
```

E2 adds `--arm e2 --intent Document` only when the device advertises that Intent.

The helper performs one `POST /ScanJobs`, one `NextDocument` GET and one job DELETE. After the image is fully read and validated, DELETE 404 is accepted as an already-removed completed job; other non-2xx cleanup responses still fail. Errors identify the operation and HTTP status without exposing endpoints or document bytes. It disables proxy, redirects, inherited cookies/credentials, compression and automatic retries, and validates bounded PNG/JPEG bytes and pixels.

## Compare

`compare` reads only local receipts and images. It does not contact a scanner or change product scanner arguments.

```sh
go run ./cmd/scanner-escl-eval compare \
  --target /private/eval/ts8130/target.json \
  --receipt /private/eval/ts8130/c1-01/receipt.json \
  --receipt /private/eval/ts8130/e1-01/receipt.json \
  --receipt /private/eval/ts8130/e2-01/receipt.json
```

Pass all 9 or more receipts. Target hash, device, firmware, requested region, resolution and color must match the oracle and every receipt. Sequence values must be unique positive integers. Missing arms, fewer than three captures per arm, duplicate sequence values, a non-alternating group, mixed identities/settings or unsafe images make the result `incomplete` and exit non-zero.

Decoded width/height differences are measured rather than rejected. For each capture the output records expected dimensions, width/height crop delta, X/Y scale and dimension-inferred rotation (`0`, `90`, or `null` when dimensions cannot establish it).

## Evidence output

The successful output is `scanner-escl-input-comparison-evidence` version 1 with `status: complete` only when every completeness and identity rule passes. It has one arm object for C1, E1 and E2 containing:

- capture count and sequence-ordered captures;
- original media/hash/bytes, elapsed time and decoded geometry observations;
- per-region foreground pixels and normalized retention relative to the summed C1 baseline;
- nearest-rank elapsed p50 and p95 milliseconds.

The foreground rule matches the correction evaluator: a normalized-region pixel differs from `background_rgb` when the sum of absolute RGB channel differences is greater than 24. Region pixel bounds use floor for the origin and ceil for the far edge.

Incomplete output retains the groups, readable arms/captures and bounded `issues`; it is never sorted or relabelled as successful evidence. Receipt target hashes are declarations from the physical session. For retained PNG receipts, compare also recomputes size and SHA256; a JPEG's original returned bytes are represented by receipt metadata and are not reconstructed from the normalized PNG.

## Review limits

Record firmware/tool identity, region, PNG hash/bytes/dimensions/time, corner/faint/content retention, unexpected crop/scale/rotation, operator steps and unknown outcomes. Geometry differences inferred from dimensions do not locate a crop or detect content rotation inside equal-sized images.

Local tests prove helper safety and evidence calculation only. They do not prove TS8130 firmware, Intent semantics, edge retention, speed or production readiness. Completion means evidence is reviewable, not accepted. Fixed-area SANE remains the product default; only a human owner can keep C1, defer, or carry the evidence into a later adoption decision.
