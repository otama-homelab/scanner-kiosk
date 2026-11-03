# SANE backend capability inspection

`Adapter.Probe` reads the selected backend's option constraints without starting
acquisition. It runs unmodified `scanimage` with exactly the configured device,
PNG output selection and `--all-options`. It uses the same explicit environment,
private directory and process-group supervision as capture.

The pinned [upstream scanimage 1.2.1 source](https://gitlab.com/sane-project/backends/-/raw/1.2.1/frontend/scanimage.c)
exits after printing all options. In contrast, its help path also enumerates
available devices. The probe therefore does not use `--help`, global discovery,
`--test`, batch mode, option-changing arguments or an exploratory scan.

## Accepted constraints

The parser is deliberately tied to the tested English `scanimage -A` output.
Required options must appear once under the exact selected-device header and
must be active. Missing, duplicate, unrecognized or partial constraints return no
capabilities. Descriptive prose and the current/default value are not evidence
that a setting is supported.

- Modes expose only Gray and Color as application gray/color. A Flatbed source
  must be explicitly available.
- DPI accepts a discrete list or numeric range with optional step. The result is
  intersected with 75, 100, 150, 200, 300 and 600. Advertising 1200 does not enable it.
- Left, top, width and height require explicit millimetre ranges and retain their
  minimum, maximum and optional quantization. Unknown units and unsupported
  constraint shapes fail closed.

These are backend constraints, not expected image pixel dimensions, measured
optical DPI or a promise that every setting combination works. A production
resolver still needs to apply mode/source-dependent constraints, supported preset
geometry and authoritative expected pixel dimensions before creating a capture
plan. It must not derive a readiness claim from this listing alone.

## Process and scratch lifecycle

The probe has a deadline, file-backed output and a 25-ms growth watchdog. Combined
stdout/stderr is capped at the smaller of the adapter's byte limit and 256 KiB,
with a final size check. Watchdog observations can overshoot; this is not a hard
filesystem quota. Any stderr warning rejects the result. Parsing occurs only
after the owned process group is confirmed gone.

A returned `ProbeAttempt` owns the private inspection directory on success and
failure. It has no publishable artifact or Release operation. The caller closes
it after confirmed local stop, before subsequent capture allocations. Cleanup
removes only its directory and syncs the parent. Unknown local stop refuses
cleanup and requires reconciliation. Probe success never proves physical idle
or fences the old scanservjs writer.

## Evidence and limits

Unit fixtures cover command arguments and secret isolation, range/list/step
interpretation, rejecting unsupported constraints, cancellation, byte bounds and
cleanup ownership. The official SANE test backend supplies actual capability
listings, including a synthetic 300-mm area; its read path is configured busy to
make accidental acquisition fail. The test still succeeds because it only reads
options. The final checker rejects skipped opt-in cases.

No real Canon query or production image acceptance follows from the virtual
backend. Combined configured-driver construction, per-mode plan resolution,
worker readiness/dispatch, durable recovery and actual device acceptance remain
pending. Existing scanservjs is unchanged.


## Selected-option inspection

`Adapter.ProbeSelection` now applies the same enum-only flatbed, color, resolution and area arguments as capture, followed by `--all-options`. A shared argument builder keeps inspection and capture aligned. It re-parses the resulting active constraints and checks the CLI's selected source/mode/DPI/left/top/width/height against the request. Both advertised support and selected read-back must match. Unknown enums are rejected before creating scratch or starting the CLI; adjusted values, unsupported constraints and any warning/diagnostic fail closed. It does not switch to another source, size, color or DPI to make the request work.

The probe retains the same process-group supervision, deadlines, output bounds and owned ProbeAttempt lifecycle. It is configuration inspection, not acquisition, pixel geometry, physical idle or readiness. The official test backend's read status is set to DEVICE_BUSY, so accidental acquisition would fail. Tests demonstrate A4/gray/300 on a 300-mm bed and photo/color/600, and refusal of A4 on the default smaller test bed. Fixture tests also cover changed source/mode/resolution/area/origin, mismatched active constraints, exact native arguments and cleanup.

Production driver composition and capture API/UI remain pending. In particular, selected millimetres do not establish image pixel dimensions. A separate [parameter helper](scanner-geometry.md) now supplies native read-back for capture-plan verification; combined production driver composition remains pending. These probes are not used to mark configured scanners ready.
