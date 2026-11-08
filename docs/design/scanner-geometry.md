# Scanner geometry boundary

The current capture Plan requires authoritative expected image width/height before acquisition. `Probe` and `ProbeSelection` expose active backend constraints and selected option read-back; neither exposes `SANE_Parameters`. `ProbeParameters` now reads them through a separate native helper described below. The [registered driver](registered-scanner-driver.md) now resolves these per-call values into capture plans in virtual-backend integration. The environment-fed Session constructor now owns configuration inputs, and worker startup invokes it behind explicit acquisition opt-in. Readiness/dispatch remain incomplete, and the public capability endpoint keeps device settings unknown.

Direct eSCL/ScanIntent remains a separate nonproduction comparison. See [Scanner input comparison](scanner-input-comparison.md); fixed-area SANE remains the product default.

The pinned [scanimage 1.2.1 source](https://gitlab.com/sane-project/backends/-/raw/1.2.1/frontend/scanimage.c) exits immediately after printing `--all-options`. Its `--dont-scan` path also exits before scan processing and does not provide a parameter-query response. Neither is a supported pixel-geometry API. Diagnostic scans, `--test`, global discovery and speculative acquisition are not substitutes for parameter inspection.

The official sane-airscan 0.99.29 source was inspected at commit `3a99066aefec5f8577cacc99a40c1a7e79443a34`, fetched read-only into `/tmp/scanner-kiosk-43-airscan-0.99.29`. No upstream edits/build were performed. Its [option parameter update](https://github.com/alexpevzner/sane-airscan/blob/3a99066aefec5f8577cacc99a40c1a7e79443a34/airscan-devops.c) calculates dimensions through [math_mm2px_res](https://github.com/alexpevzner/sane-airscan/blob/3a99066aefec5f8577cacc99a40c1a7e79443a34/airscan.h), which rounds a SANE fixed-point millimetre value. Existing official test-backend fixtures use a different pixel conversion. Their geometry must not be copied into an airscan production resolver.

The implementation now reads backend parameters through the standard SANE API instead of copying the rounding formula. The registered driver component is connected to the scan executor in tagged virtual-backend integration. The environment-fed configuration/lifecycle component is implemented. Worker startup invocation, executable/backend version binding, metadata publication/readiness, dispatch/recovery and physical acceptance remain pending.

## Parameter helper

`cmd/sane-parameters` is an own-product Go/cgo helper, built only with the `saneparams` tag and official libsane headers/library. It adds no Go module dependency or upstream patch. The main API/worker remains CGO-free; the canonical web build now explicitly sets CGO_ENABLED=0. The helper is a separate worker executable, not a linked dependency of the HTTP process. Environment-fed configuration and owned-input lifecycle are implemented as a Session; image packaging and worker startup/dispatch wiring are still pending.

It accepts only a registered native device plus application size/color/DPI enums. An existing private SANE_CONFIG_DIR with the single allowed DLL backend is required before initialization. It opens that exact device without enumeration, applies flatbed/mode/resolution/area, rejects inexact values, refreshes option descriptors, and verifies the final option set again because later changes may invalidate earlier ones. It reads parameters and closes/exits the library before emitting versioned JSON. There are no calls/imports for acquisition, image read, device discovery or cancel. The standard API contract distinguishes pre-start estimates from guaranteed post-start parameters ([SANE API](https://sane-project.gitlab.io/standard/api.html#sane-get-parameters)); therefore capture must still validate actual image geometry, and a mismatch must not trigger an automatic scan retry.

`Adapter.ProbeParameters` uses an explicitly configured helper path, with no implicit fallback. It shares bounded file-backed inspection, explicit environment, process-group deadline/stop proof and owned ProbeAttempt cleanup with existing probes. JSON rejects duplicate/unknown/case-mismatched/missing/null fields and trailing data. Protocol/SANE version, exact selection, single-frame gray/RGB 8-bit layout, positive dimensions/stride and max page pixels are checked before returning. Failed reads expose no partial parameters. These are per-call backend values, not a durable capability lease, physical-idle observation or permission to clear scanner leases.

## Verification

```sh
python3 scripts/check-sane-parameters.py \
  --binary /path/to/scanimage \
  --include-dir /path/to/include \
  --library-dir /path/to/lib \
  --library-dir /path/to/lib/sane \
  --receipt /tmp/scanner-parameters-check.json
```

The checker compiles the tagged helper against official libsane, checks its imported APIs, proves the main command builds without CGO or an ELF dynamic interpreter, and verifies refusal without private configuration. Tagged tests read A4/gray/300 and photo/color/600 parameters from the official test backend configured to fail image reads. Separate actual virtual captures then verify those returned dimensions against decoded PNGs. Unsupported area and over-budget pixels are refused. Default Go tests cover strict metadata, secret-isolated supervision and cleanup. This is virtual SANE evidence, not real Canon/airscan geometry or deployment acceptance. The official libsane-dev 1.2.1 package was downloaded/extracted under the existing /tmp fixture root, without host package installation.


Registered-driver update: the checker additionally exercises the combined driver and actual monitored scan executor against virtual acquisition and local HTTP status fixtures (7 checks, 11 tagged tests in total). Idle permits transactional page publication; Processing preserves the held device lease and reservation until a subsequent fresh Idle observation allows stopped-failure cleanup. This does not initialize production idle rows or complete environment/runtime driver wiring.


## Read-only Canon TS8130 observation (2026-11-02)

After the owner powered on the local printer, `scripts/check-device.py` exercised the existing supervised product probe against the registered eSCL endpoint using official scanimage/libsane/airscan and the separate product parameter helper. It never calls Capture, sane_start or discovery, and does not stop/change scanservjs, create durable leases or publish scanner readiness. The additional HTTP metadata requests use GET only, no proxy/redirects, bounded responses and whitelisted summaries omitting serials/UUIDs/job identifiers. The receipt records source/tool hashes and cleanup verification; repeat it explicitly against a registered device, not as an ordinary CI test.

```sh
python3 scripts/check-device.py --endpoint http://SCANNER_ADDRESS/eSCL \
  --device-id scanner_canon_ts8130 --binary /path/to/scanimage \
  --parameters-binary /path/to/sane-parameters \
  --library-dir /path/to/libs --library-dir /path/to/libs/sane \
  --receipt /tmp/canon-readonly.json
```

The real device advertised model TS8130 series, eSCL version 2.63, a platen with no ADF, RGB24/Grayscale8, JPEG/PDF transport, and symmetric discrete resolutions 75/100/150/200/300/600/1200. Product SANE capabilities correctly cap selectable resolution at 600. The backend bed was 215.9 by 297.011 mm. Selected options and pre-start parameters passed without quantization/fallback:

| Selection | Native pre-start dimensions | Stride |
|---|---|---|
| A4, gray, 300 dpi | 2480 × 3508 | 2480 bytes |
| A4, color, 300 dpi | 2480 × 3508 | 7440 bytes |
| 100 × 150 mm, color, 600 dpi | 2362 × 3543 | 7086 bytes |

All use 8-bit samples and a single last frame. The product eSCL observer accepted the real Idle response after the local probes stopped, and owned configuration/scratch cleanup completed. This supports real metadata compatibility, not exclusive hardware control, post-acquisition geometry, cancellation, image quality, PDF/OCR or a complete workflow. A previous harness invocation used an incorrect photo enum and was correctly rejected before that selection's native invocation; the harness was corrected to the existing photo10x15 enum, with the failed receipt retained separately. No production code was loosened.

The native JPEG/PDF transport also means an application PNG/lossless PDF option describes preservation of the returned bitmap; it cannot promise sensor-to-file losslessness. Actual image capture and geometry checks remain mandatory before device acceptance.

## Prepared-original manual capture (2026-11-03)

With explicit operator authorization and a controlled legacy-service stop/restore
window, the current standalone bundled native tools and product Capture adapter
acquired one A4/color/300 page: 2480 × 3508, PNG, 5,393,208 bytes.
Capture took approximately 21.39 seconds by monotonic timing. Actual decoded
geometry and private-file hash readback matched; local process stop preceded
a fresh device Idle observation. The legacy Deployment was restored to one
ready replica. Image contents remain outside source/evidence/build contexts.
This validates one manual component acquisition, not production fencing,
readiness/admission, OCR/PDF/image quality, cancellation or full resource limits.
The parent task retains `real-device-prepared-original-20261103/current-evidence.json`.
