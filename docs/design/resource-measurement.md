# Local resource measurements — 2026-11-02

These measurements establish a reproducible local processing baseline. They do
not pass G5 or accept container budgets. All inputs are synthetic retained pages;
no scanner, native OCR process, real IdP, browser/mobile or production store was
used. Resource receipts live in the parent workspace's plan43 artifacts.

## Method and environment

`scripts/check-resources.py` builds the normal (non-race, non-web-embedded) scanner
CLI and runs separate API and worker processes against private temporary stores.
Only retained source/page fixtures are seeded in SQL. Guest bootstrap, render
admission, immutable reservation and repeated job status reads use the real HTTP
API. The internal loopback fixture supplies the fixed public Host/Origin and
manually forwards its synthetic Secure cookie; it is not a TLS/client-transport
test. No native scanner or OCR plugin is enabled, no credentials are inherited,
and original files are ordinary private files rather than sparse/hardlinked data.

Go 1.27.3, Linux WSL2 x86_64, 12 visible CPUs, no cgroup CPU/memory limit. API
GOMAXPROCS=1, worker=2; this is not a CPU quota. API has no Go memory limit. Inputs
use 2480×3508 pixels at 300dpi-equivalent and 4960×7016 at 600dpi-equivalent. Noise
PNG/JPEG fixtures exercise poor compression; the gray 50-page pattern compresses
well. The color 50-page JPEG fixture uses quality 10 and distinct seeded pixels to
fit the real default owner/job admission budgets. This is a stress fixture, not a
quality recommendation. Defaults remain 40M pixels/page, 50 pages, 200MiB derived
reservation, 500MiB/owner and 1GiB global.

Worker and API command peak RSS/CPU come from GNU time measuring its own native
forked command. Python's direct wait4 can include the fixture controller's large
pre-exec RSS peak: a 200MiB controller allocation made /usr/bin/true report about
215MiB, while the native-time child reported about 1.2MiB. Earlier draft RSS
observations are superseded. Current measurements also sample target RSS and
private file logical/allocated bytes every 20ms. Disk peaks can miss transient
files and exclude directory/journal overhead and unlinked open files. CPU/time
exclude fixture generation, compilation and independent output validation.
Native-time CPU precision is 0.01s. PIDfd identity/parent checks bind stop signals
to owned Go commands; the observer aborts at 2GiB sampled RSS or 300s.

A single synthetic guest polls status at 100ms intervals while the worker runs.
The table's p95 is the nearest-rank value of those samples, including request
latency and lock/storage contention. Very short cases have few samples. Prior
observations included 577–828ms outliers; the final matrix below is within 500ms,
but that variability is not resolved into a stable acceptance claim. Repetitions,
actual deployed storage/CPU quotas, many sessions and real network/TLS/browser
latency remain required. Do not infer a worst-case bound from one run.

## No Go soft memory limit

| Case | Pages | Worker peak MiB | Wall s | CPU s | Sampled temp growth MiB | Status p95 ms | Samples | Job result |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| a4-300-gray-jpeg | 1 | 59.7 | 0.50 | 0.52 | 7.2 | 6.9 | 5 | succeeded |
| a4-300-gray-lossless | 1 | 93.5 | 0.31 | 0.31 | 8.5 | 16.7 | 3 | succeeded |
| a4-300-color-jpeg | 1 | 157.6 | 1.14 | 0.98 | 8.2 | 95.0 | 10 | succeeded |
| a4-300-color-lossless | 1 | 292.6 | 1.44 | 1.05 | 25.1 | 334.7 | 10 | succeeded |
| a4-600-gray-jpeg | 1 | 215.6 | 2.23 | 1.83 | 28.1 | 368.9 | 18 | succeeded |
| a4-600-gray-lossless | 1 | 266.7 | 1.21 | 1.13 | 33.3 | 120.4 | 11 | succeeded |
| a4-600-color-jpeg | 1 | 520.0 | 3.70 | 3.90 | 32.4 | 9.1 | 36 | succeeded |
| a4-600-color-lossless | 1 | 1132.3 | 4.12 | 4.59 | 99.7 | 9.5 | 40 | succeeded |
| a4-300-gray-50-pages | 50 | 80.0 | 6.06 | 6.24 | 5.5 | 16.1 | 46 | succeeded |
| a4-600-color-50-low-jpeg | 50 | 1104.1 | 57.54 | 58.73 | 184.4 | 8.2 | 548 | succeeded |
| a4-600-color-jpeg-expansion | 8 | 1387.6 | 16.24 | 17.18 | 0.1 | 15.5 | 153 | failed |

The JPEG-to-lossless expansion case is intentionally expected to fail the
immutable output budget. The normal worker terminalizes it, cleans unpublished
output and releases its reservation while preserving originals; no final artifact
is published. Every successful case publishes the expected PDF page count and
512-edge JPEG thumbnails. Independent pikepdf/Pillow checks and SHA/byte checks
verify those outputs and original immutability. Receipt `passed` means measurement
execution and expected functional results, not acceptance of a memory or p95 SLO.

## GOMEMLIMIT=1024MiB comparison (worker only)

The same binary and input hashes were measured with the standard Go soft runtime
limit. API environment is unchanged. Kernel peak observations were:

| Case | Unset peak MiB | 1024MiB peak MiB | Unset wall s | 1024MiB wall s |
|---|---:|---:|---:|---:|
| a4-600-gray-jpeg | 215.6 | 214.3 | 2.23 | 1.64 |
| a4-600-gray-lossless | 266.7 | 266.8 | 1.21 | 0.98 |
| a4-600-color-lossless | 1132.3 | 898.9 | 4.12 | 5.40 |
| a4-600-color-50-low-jpeg | 1104.1 | 1007.9 | 57.54 | 54.87 |
| a4-600-color-jpeg-expansion | 1387.6 | 1085.2 | 16.24 | 16.45 |

The examples now pass GOMEMLIMIT=1024MiB to the worker, with the separate existing
1536MiB container limit unchanged. This leaves more observed headroom in the
memory-heavy plain Go paths. It is a soft Go runtime budget, not an RSS guarantee,
child/native-process limit or proof against OOM. Installed Go runtime/debug docs
explicitly exclude non-Go and external allocations. Real capture/Tesseract/LLM,
parallel devices and combined process/container peaks still need measurement.
No image, Pod or production deployment was changed or accepted.

## Reproduction

Requires Linux /proc, pidfds and /usr/bin/time. Python dependencies used were
Pillow 12.2.0, NumPy 2.5.0 and pikepdf 10.17.0. Run:

```sh
uv run --with pillow==12.2.0 --with numpy==2.5.0 --with pikepdf==10.17.0 python scripts/check-resources.py --receipt /tmp/resource-baseline.json
uv run --with pillow==12.2.0 --with numpy==2.5.0 --with pikepdf==10.17.0 python scripts/check-resources.py --gomemlimit 1024MiB --case a4-600-gray-jpeg --case a4-600-gray-lossless --case a4-600-color-lossless --case a4-600-color-50-low-jpeg --case a4-600-color-jpeg-expansion --receipt /tmp/resource-soft-limit.json
```

Keep source/binary/input hashes and environment with the results. Use --case for
a focused repeat. The caller explicitly selects a receipt path; files are never
uploaded. Temporary images/stores/logs disappear only after owned processes stop.
Observer-limit/timeout results must be retained as failed observations, not
silently restarted or promoted to functional success. Native-process/OOM recovery,
real hardware/mobile, container hard limits, disk-full behavior, storage placement,
p95 stability and release/cutover remain open gates.
