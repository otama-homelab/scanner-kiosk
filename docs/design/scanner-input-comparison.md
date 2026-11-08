# Scanner input comparison

This is a nonproduction experiment for Canon TS8130 input behavior. The shipped product continues to use explicit fixed-area SANE capture; direct eSCL and `ScanIntent` are not part of scanner admission.

## Arms

| Arm | Path | Region and processing |
| --- | --- | --- |
| C1 | existing `scanimage` / sane-airscan | Flatbed, fixed paper area, explicit color/DPI |
| E1 | `scanner-escl-eval` | direct eSCL fixed region with `MustHonor=true`, no Intent |
| E2 | `scanner-escl-eval` | same region plus one device-advertised Intent |

All arms use the same physical target, region, color and DPI. Other-model evidence is not TS8130 evidence.

## Physical target

Use an edge-marker target with content within 2mm of the intended boundary, four corner markers, one central text block, one faint area and a unique identifier. Record the target hash privately; do not commit private scanned pages.

## Capture schedule

Use at least three captures per arm and alternate:

```text
C1 E1 E2
E2 E1 C1
E1 C1 E2
```

Every request is one explicit operation. A timeout, lost response or uncertain device state stops the sequence. Never automatically retry.

## C1

```sh
go run ./cmd/scanner-escl-eval evaluate-c1 \
  --input /private/eval/ts8130/c1-01.png \
  --region 0,0,2480,3508 \
  --resolution 300 --color rgb \
  --receipt /private/eval/ts8130/c1-01.json
```

## E1/E2

```sh
go run ./cmd/scanner-escl-eval capture \
  --endpoint http://scanner.example/eSCL \
  --arm e1 \
  --region 0,0,2480,3508 \
  --resolution 300 --color rgb \
  --output /private/eval/ts8130/e1-01 \
  --receipt /private/eval/ts8130/e1-01.json
```

E2 adds `--arm e2 --intent Document` only when the device advertises that Intent.

The helper performs one `POST /ScanJobs`, one `NextDocument` GET and one job DELETE. It disables proxy, redirects, inherited cookies/credentials, compression and automatic retries, and validates bounded PNG bytes/pixels.

```sh
go run ./cmd/scanner-escl-eval compare \
  --receipt /private/eval/ts8130/c1-01.json \
  --receipt /private/eval/ts8130/e1-01.json \
  --receipt /private/eval/ts8130/e2-01.json
```

## Review

Record firmware/tool identity, region, PNG hash/bytes/dimensions/time, corner/faint/content retention, unexpected crop/scale/rotation, operator steps and unknown outcomes. The owner then keeps C1, defers, or takes the evidence into a later explicit input decision.

Local tests prove helper safety only. They do not prove TS8130 firmware, Intent semantics, edge retention, speed or production readiness.
