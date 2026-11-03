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

The GitHub remote, hosted runs, branch/tag protections, complete reproducible
native build/source offer, SBOM/security/license acceptance and publishing jobs
are still pending. There is deliberately no successful-looking empty publishing
job or API-only binary advertised as the plug-and-play preview. Native payload
and release gates must be implemented for the exact input before wiring registry
or GitHub Release writes. Until then, tag pushes verify a release input; they do
not produce a published release. No release tag, remote push, registry upload,
production deployment or external workflow execution was performed here.
