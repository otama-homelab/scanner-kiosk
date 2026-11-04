# ADR 0005: Apache-2.0 for project-owned code

Status: accepted, 2026-11-04.

## Decision

Use the unmodified Apache License, Version 2.0, for project-owned scanner-kiosk
code and assets, unless a file identifies different terms. The operator asked
for a widely used OSS license with more conditions than MIT, without requiring
copies or derivatives to adopt the same license. This authorizes the project
license choice; it does not establish ownership of third-party material.

Apache-2.0 fits through redistribution notices, modification notices and an
explicit contributor patent grant with patent-litigation termination. It is
still permissive: commercial use, proprietary modifications and redistribution
are allowed. It introduces no homelab-only, noncommercial, same-license or
mandatory publication restriction. MIT lacks the requested explicit patent and
redistribution provisions; GPL/AGPL same-license obligations do not fit the
requested choice. No custom restrictive clauses are added.

## References and conditions

The official [Apache text](https://www.apache.org/licenses/LICENSE-2.0.txt) is
copied verbatim to [LICENSE](../../LICENSE). It matches the license text used by
[Kubernetes v1.34.0](https://github.com/kubernetes/kubernetes/blob/v1.34.0/LICENSE)
and the exact [Tesseract source](https://github.com/tesseract-ocr/tesseract/blob/7e548e6eff172f38f97664afe7f7ce40db36c8d6/LICENSE)
used here. These are adoption examples, not evidence that every dependency is
compatible or that the project is affiliated with those authors.

Section 4 requires a license copy, prominent notices in changed files, retention
of applicable attribution notices and preservation of any supplied NOTICE
attributions. It permits different terms for modifications or derivative works
subject to the Apache conditions. Sections 3 and 6 govern patents and trademarks.
Do not invent a project copyright holder, contributor consent, or a NOTICE
attribution that was never supplied.

## Distribution scope

This decision does not relicense Go dependencies, SANE, sane-airscan, Tesseract,
models, fonts or other third-party materials. Preserve their exact terms,
exceptions and source/replacement obligations separately. A project Apache
license does not override GPL/LGPL conditions or make a combined distribution
Apache-only. The distribution matrix remains in
[license-distribution](../design/license-distribution.md).

Record Apache-2.0 in the private frontend package and root lockfile metadata;
no dependency version/integrity changes or npm publication are required. Root
LICENSE covers source distribution. Include the project license with actual
binary/image release materials and complete the existing dependency/source
review before publication. Current development artifacts predate this adoption;
this decision alone does not approve or publish them.
