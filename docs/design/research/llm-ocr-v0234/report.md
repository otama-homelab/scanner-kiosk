# LLM OCR preferences and distribution — v0.2.34

[Release](https://github.com/otama-homelab/scanner-kiosk/releases/tag/v0.2.34) · [Verification record](release.json)

The provider now accepts explicit normalized `[left, top, right, bottom]` boxes and derives pixel extents once. This repairs the observed failure where lower-right coordinates were returned as width and height. Strict shape, range and byte validation remain.

LLM interpretation is optional and off by default. An ordinary checkbox remembers the choice in this browser; the setting is validated and retained through the immutable request, worker and provider. Off does not ask for interpretation and discards an unsolicited supplement string. On embeds separately labeled searchable PDF interpretation. Existing files stay immutable; updating one requires new recognition and explicit adoption.

OCR text and positions load concurrently without caching away identity, expiry or abort checks. The shared viewer reserves scrollbar space so zoom retains its fit geometry. Native model selection performs no fetch. Actual device popup latency remains unverified.

The maintained English documentation leads with Docker run and offers Compose as an alternative. Eleven guides,95 local links and17 shell examples were checked. Existing English UI screenshots use synthetic data with explicit provenance; they do not measure model accuracy. Deployment archive contents were verified against the frozen source.

All seven formal CI gates passed attempt1. Publication used only the publisher on attempt2 and reset its flag to false. Provider/document/admission/HTTP race checks, worker paths, five focused final-source browser scenarios and real-backend PDF downloads passed. Actual-endpoint synthetic probes produced zero supplement bytes when off and nonempty interpretation when on. Four runtime-image PDFs/eight A4 pages were independently inspected. Two broader local browser attempts timed out; those failures are retained, not counted as passes.

The public artifacts, paired OCI images and aliases were read back. Production kept schema27, profile/font, storage and deployment identities; only five image references changed across three infrastructure files. Intake resumed after job0/device-uncertainty0 inspection. All three containers were Ready with zero restarts; ten served assets and health/auth boundaries matched. The infrastructure commit is local on the existing unrelated branch and was not pushed.

The same12 candidate advisory findings remain, with the raw threshold false explicitly retained. No dependency or native-license changes were made. This bounded release does not qualify a correction model or complete the original milestone: seven of14 requirements remain complete.
