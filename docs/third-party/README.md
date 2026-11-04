# Supplementary third-party source notices

These notices and provenance records supplement the dependency audit. They are not a complete distribution notice bundle or license approval. The current root Dockerfile copies the onefile executable; its embedded native notices are selected by the runtime manifest/provenance. The Debian SANE source directories below are historical audit inputs, and the npm source notices remain build/source audit material.

- sane-airscan-0.99.35: verbatim upstream LICENSE with linking exception and GPL-2 COPYING, from the authenticated Debian 0.99.35-1 source archive.
- sane-backends-1.3.1: verbatim LICENSE, GPL-2 COPYING and public-domain API header; `dll.c.copyright` is the explicitly marked original leading comment, including its linking exception, from the authenticated Debian 1.3.1-3 source archive. This does not extend the exception to every backend or to scanimage.

- boolbase-1.0.0: runtime entrypoint and README match the recorded licensed source; manifest test script differs.
- withastro--compiler-rs and bruits--satteri: notices from publisher-declared source commits, mapped to the current locked Linux x64 GNU/musl packages.
- rolldown--rolldown: notice from the source commit in previously verified registry provenance, mapped to the current locked Linux x64 GNU/musl packages.

Native binary dependencies and exact binary/source correspondence require separate review. In particular, sharp-libvips build repository terms must not replace the bundled native library terms. Missing notices for am-i-vibing, piccolore and process-ancestry remain unresolved. Review generated frontend assets and distribution contents before completing the license gate. Preserve copyright statements when assembling delivery notices.


## Current frontend distribution scope

An offline instrumented Astro build reproduced all five current embedded asset
hashes exactly. The emitted browser JavaScript chunk lists eight input modules,
all under this project's `web/src`; no npm module is listed in that chunk.
Seven prerender chunks contain build-time Astro/npm code but are absent from
the actual five-file distribution. Source records and the raw bundler metadata
are retained in the parent plan's
`current-frontend-artifact-origins-20261104/current-evidence.json`.

This narrows artifact attribution; it does not establish rights in generated
helpers/HTML or approve the entire license gate. Do not classify every npm
build dependency as browser-shipped code merely because it appears in the lock.
Likewise, the optional local npm-input archive would have its own redistribution
obligations if published; it is currently unpublished and is not automatically
a required product release asset. Review actual native/Go code, copied public
assets, generated output and any source archives that will really be delivered.
