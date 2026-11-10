# Phone photo correction: next bounded work

Started2026-11-10 after the owner requested deployment and continuation. v0.2.5 is now live. This work reopens the existing DocAligner follow-up; it does not adopt a model into production or change the no-correction/full-original default.

## First observed runtime result

The existing DocAligner1.1.1 lcnet100 ONNX artifact was downloaded from its pinned upstream release and verified at4,767,987bytes / SHA256 `f4117b786e3a18470f3865c93f3c2bd69d9b998edd60f385574a5c665e79594e`. The Apache-2.0 license was checked at source revision `3275b0f07f8e99d8c01cb0774dea2549be1416b6`. ONNX Runtime1.23.2 was installed only in a disposable operator virtual environment; production images and dependencies are unchanged.

A network-isolated process with a1GiB address-space ceiling,30CPU-second ceiling and explicit2-thread CPU provider executed eleven forward passes on one zero-valued float32 input `[1,3,256,256]`. Every output was finite with shape `[1,4,128,128]`. Observed import/session startup250.94ms, first forward52.93ms, ten warm forwards median32.56ms and maximum33.87ms; process peak RSS149,180,416bytes (about142.3MiB). Runtime used NumPy2.5.0. These are local x86_64 raw-graph smoke measurements, not phone performance, four-corner accuracy, end-to-end correction cost, household quality or OCR benefit. No document was sent to any external model/provider.

The prior survey's unavailable-runtime result remains historical evidence. Runtime availability is now observed in this disposable environment; no correction candidate has passed the evaluation harness by this result.

## Next execution order

1. Reuse the Phase44/45 candidate protocol and pinned upstream preprocessing/corner ordering. Implement the smallest bounded external adapter, with geometric validation for finite/in-bounds/convex corners and minimum retained area; unreliable detections must preserve the original and allow manual adjustment. Do not use a bespoke success-only benchmark.
2. Compare no correction, the existing measured OpenCV perspective candidate and DocAligner on identical skew/edge/faint-content fixtures. Use the existing analyzer's `crop_quad` contract, original hashes, cold/warm/resource receipts and expected-failure accounting. Measure full resolution output and caller-owned perspective warp, not only the256px neural network.
3. Measure OCR text/reading-order benefit and numeric/fine-line preservation separately. Smartphone photographs and household documents require representative supplied fixtures; synthetic success cannot satisfy their acceptance. Keep correction optional, preview/apply/undo explicit, and Save/reOCR pixels consistent.
4. Reopen UVDoc only for residual curved-page failures and after a nonlinear sampling-field/quality contract is defined. Keep brightness/shadow correction as a separate measured operation. DocTr remains excluded by its surveyed non-commercial terms; TrOCR and Tesseract remain recognition models, not geometric correction substitutes.

## Boundaries

No production model weights/runtime/service, browser runtime, automatic apply, new physical scan, private transmission, default change or GPU deployment is introduced. No new milestone number or execution phase has been opened by this runtime smoke. Existing20 continuing prohibitions and dated owner acceptance remain in force. Full product implementation and real-phone quality testing remain outstanding.
