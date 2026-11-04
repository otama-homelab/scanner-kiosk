# Preview and tagged releases

The trunk is `main`. A push to `main` is a preview input; it never constitutes a
stable release. A pushed `vMAJOR.MINOR.PATCH` tag is a release input. A tag with a
SemVer prerelease suffix, for example `v0.1.0-rc.1`, remains a prerelease and must
not advance the stable `latest` image alias. Numeric version identifiers cannot
have leading zeroes. Build-metadata (`+…`) tags are not supported.

This adopts the separation of preview and production used by
`otama-playground-astro`, adapted to the scanner's distribution: a complete
Linux amd64 executable including frontend/SANE/Tesseract/data, plus the
corresponding distroless image. Cloudflare Pages, its credentials and browser-only
hosting are unrelated to this application.

## Exact source and channels

Every build uses the event commit, including a tag's peeled commit; a tag build
must never check out the latest trunk in its place. The checkout must match that
commit and be reachable from the fetched `origin/main`. Annotated and lightweight
tags are supported. An older commit still in trunk history is a valid input, so
another push arriving during a run does not invalidate its immutable preview.

PRs run checks without publishing. Main and tag pushes run the same quality
checks and the release-ref policy. Manual source-check runs do not publish.
Checks must pass before a future publisher can run. Main/tag checks are not
cancelled by a newer push; only stale PR runs are cancelled.

When publishing is implemented, retain a commit-addressed preview for each
successful main input and a `preview` alias for the newest qualified trunk
commit. An older run finishing late must not move that alias backwards. Stable
tags publish versioned binaries/images and a GitHub Release, with `latest` only
for a stable version according to version ordering. Prerelease tags publish a
GitHub prerelease and versioned image, without changing stable aliases. An
existing published version must not be overwritten or rebuilt from a moved tag.
Deployment to the scanner and legacy retirement remain separate operations;
publishing an artifact must not automatically acquire a physical scanner lease.
Rollback selects a previous immutable version/digest after quiescing the worker;
it does not rebuild an old version or bypass storage/schema compatibility.

## Current implementation and remaining gates

`checks.yml` now routes pushes on `main` and `v*` tags to source checks. Its
release-ref job uses a full checkout with read-only permissions and no retained
credentials, then validates the exact commit, version syntax and trunk ancestry
using `scripts/check-release-ref.py`. Invalid tag versions fail rather than
being silently treated as previews. The ref checker does not publish anything.

`scripts/build-web.py --output … --ref "$GITHUB_REF" --sha "$GITHUB_SHA"` now embeds the validated channel version and exact peeled commit into the app. Identity validation runs before dependency installation. Main inputs report `preview-<12-character commit>`; tags report their exact `v…` name. Supplying only one identity argument fails. Without either argument, local/PR/manual builds retain `0.0.0-dev` and `unreleased`. The source CI uses identity arguments for push events, then compares the executable’s actual `version` response with the validated policy result.

`scripts/build-standalone.py` now supplies the packaging step for an already checked embedded app and a reviewed native manifest. It checks the static app target/build tag, compares the complete native inventory, builds the launcher in scratch, and refuses to overwrite an output directory. It neither supplies native provenance nor fetches the native input.

[Native source inputs](native-sources.md) distinguishes verified recipe/patch collection from upstream archives and source delivery. Both build SDKs now have complete APK locks and offline replay evidence; Tesseract and the SANE helper reproduce their current native file bytes. Stable native closure/provenance packaging and source delivery remain required. Source CI runs the collector’s offline safety tests only.

The source build job also runs `npm audit --audit-level=high` against the locked
frontend build dependencies and the pinned `govulncheck` v1.9.0 against `./...`
with the `webui` tag after generating embedded assets. Both are required steps:
advisory-service failures and findings at their respective thresholds fail the
job. Go source scanning uses Linux/amd64 with CGO disabled, matching the shipped API,
and the tool's default symbol-level analysis, rather
than treating a binary scan's JSON exit status as a clean result. These live
source/build checks do not cover native C libraries or the distroless OS, prove
license compliance, or authorize publication. Local snapshot-based diagnostics
remain separate evidence. Initial hosted run
[41780461741](https://github.com/otama-homelab/scanner-kiosk/actions/runs/41780461741)
passed all four jobs for commit `99990c12…`; this does not publish artifacts.

The private GitHub remote and initial hosted source checks are established.
Branch/tag protections, final native distribution/source offer,
SBOM/security/license acceptance and publishing jobs remain pending. GitHub Free
returned HTTP403 for branch protection on this private repository; changing
visibility or paying for another plan requires the operator's choice. There is
deliberately no successful-looking empty publishing job or API-only binary advertised as the plug-and-play preview. Native payload
and release gates must be implemented for the exact input before wiring registry
or GitHub Release writes. Until then, tag pushes verify a release input; they do
not produce a published release. That source check created no release tag,
registry upload or production deployment.


## Required source status

The workflow exposes `required-checks` as the aggregate status for future branch
protection. It runs even when a dependency fails: source build/tests and deployment
contracts must both succeed. On pushes, release-ref-policy must also succeed;
for PR/manual checks that inapplicable job must be skipped. Failure, cancellation,
a missing result or an unexpected skip cannot turn the aggregate green. Select
this exact job as required after the hosted repository has observed it.

Two local contract tests cover supported events and refusal of non-success
required results. Pinned actionlint validated the workflow; shellcheck/pyflakes
were unavailable in that fixture. This adds no publishing permissions or jobs,
and does not approve native artifacts, licensing, release or deployment.
The initial hosted aggregate succeeded. Actual branch protection remains
unconfigured under the private-repository plan restriction.
