# Supplementary third-party source notices

These notices and provenance records supplement the dependency audit. They are not a complete distribution notice bundle or license approval. The Dockerfile copies only the two reviewed SANE directories into `/usr/share/scanner-kiosk/native-notices`; the npm source notices below remain build/source audit material.

- sane-airscan-0.99.35: verbatim upstream LICENSE with linking exception and GPL-2 COPYING, from the authenticated Debian 0.99.35-1 source archive.
- sane-backends-1.3.1: verbatim LICENSE, GPL-2 COPYING and public-domain API header; `dll.c.copyright` is the explicitly marked original leading comment, including its linking exception, from the authenticated Debian 1.3.1-3 source archive. This does not extend the exception to every backend or to scanimage.

- boolbase-1.0.0: runtime entrypoint and README match the recorded licensed source; manifest test script differs.
- withastro--compiler-rs and bruits--satteri: notices from publisher-declared source commits, mapped to the current locked Linux x64 GNU/musl packages.
- rolldown--rolldown: notice from the source commit in previously verified registry provenance, mapped to the current locked Linux x64 GNU/musl packages.

Native binary dependencies and exact binary/source correspondence require separate review. In particular, sharp-libvips build repository terms must not replace the bundled native library terms. Missing notices for am-i-vibing, piccolore and process-ancestry remain unresolved. Review generated frontend assets and distribution contents before completing the license gate. Preserve copyright statements when assembling delivery notices.
