# Third-party notices

Project-owned code is licensed under [Apache-2.0](LICENSE). Third-party material
keeps its own terms. This index does not relicense bundled software or approve
an unreleased candidate for distribution.

## Delivered boundaries

The standalone launcher and API are Go executables. They invoke separate native
programs; the CGO sane-parameters helper links SANE. The packed native payload
contains shared libraries, OCR models, a CA bundle and the notices indexed below.
The container adds the pinned distroless base specified in [Dockerfile](Dockerfile).
Node/npm and the build SDK are build inputs, not automatically browser/runtime
contents. The native payload includes Tesseract’s glyphless `pdf.ttf`, separate
from optional administrator-supplied fonts. See the [font source association](docs/design/license-distribution.md#bundled-pdf-font-identity). Optional administrator fonts are not bundled.

[Go notice index](native/notice-files/retained/go/index.json) preserves module
versions, checksums and source notice paths, including third-party subcomponents.
Its historical `ProjectLicense` field predates the Apache-2.0 decision; it is not
the current first-party license. The module set is a source inventory, not proof
that every module is linked into every executable.

This software is based in part on the work of the Independent JPEG Group.

## Native source associations

Each association below records the pinned source archive, recipe identity,
retained notices and corresponding runtime files. License texts and exceptions
must be read for the particular delivered file; a root COPYING can include terms
for tools absent from the runtime. Publication approval remains pending.

| Component | Source and notice association |
| --- | --- |
| avahi-0.9-rc4 | [source record](native/notice-files/avahi-0.9-rc4/source.json) |
| brotli-1.2.1 | [source record](native/notice-files/brotli-1.2.1/source.json) |
| dbus-1.16.2 | [source record](native/notice-files/dbus-1.16.2/source.json) |
| gcc-15.2.0 | [source record](native/notice-files/gcc-15.2.0/source.json) |
| gettext-1.0 | [source record](native/notice-files/gettext-1.0/source.json) |
| giflib-5.2.2 | [source record](native/notice-files/giflib-5.2.2/source.json) |
| gmp-6.3.0 | [source record](native/notice-files/gmp-6.3.0/source.json) |
| gnutls-3.8.13 | [source record](native/notice-files/gnutls-3.8.13/source.json) |
| leptonica-1.87.0 | [source record](native/notice-files/leptonica-1.87.0/source.json) |
| libffi-3.5.2 | [source record](native/notice-files/libffi-3.5.2/source.json) |
| libidn2-2.3.8 | [source record](native/notice-files/libidn2-2.3.8/source.json) |
| libjpeg-turbo-3.1.3 | [source record](native/notice-files/libjpeg-turbo-3.1.3/source.json) |
| libpng-1.6.59 | [source record](native/notice-files/libpng-1.6.59/source.json) |
| libtasn1-4.21.0 | [source record](native/notice-files/libtasn1-4.21.0/source.json) |
| libunistring-1.4.2 | [source record](native/notice-files/libunistring-1.4.2/source.json) |
| libusb-1.0.30 | [source record](native/notice-files/libusb-1.0.30/source.json) |
| libwebp-1.6.0 | [source record](native/notice-files/libwebp-1.6.0/source.json) |
| libxml2-2.13.9 | [source record](native/notice-files/libxml2-2.13.9/source.json) |
| musl-1.2.6 | [source record](native/notice-files/musl-1.2.6/source.json) |
| nettle-3.10.2 | [source record](native/notice-files/nettle-3.10.2/source.json) |
| p11-kit-0.26.2 | [source record](native/notice-files/p11-kit-0.26.2/source.json) |
| tiff-4.7.2 | [source record](native/notice-files/tiff-4.7.2/source.json) |
| xz-5.8.4 | [source record](native/notice-files/xz-5.8.4/source.json) |
| zlib-1.3.2 | [source record](native/notice-files/zlib-1.3.2/source.json) |
| zstd-1.5.7 | [source record](native/notice-files/zstd-1.5.7/source.json) |

SANE, the modified airscan fork, Tesseract and OCR model notices are also present
in the complete payload index below. Their current binary/source identities are
recorded in [native provenance](native/runtime-provenance.json), rather than
in historical Debian package inventories.

## Complete selected notice index

The selected notice inputs now contain188 entries; the retained unreleased
8e8c75f4… candidate still contains187 until the planned batch rebuild.
These paths and SHA-256 values are taken from the committed runtime manifest
and provenance. Source bytes were checked against every value when this index
was created. Runtime paths are relative to the unpacked native root. This is
an inventory of selected notices, not a claim that all component obligations
have been discharged. Preserve the actual texts when redistributing.

| Runtime notice path | Source text | SHA-256 |
| --- | --- | --- |
| `usr/share/scanner-kiosk/notices/README.txt` | [retained/README.txt](native/notice-files/retained/README.txt) | `1fbb15515169d61f2c5c88d004750009fae25b6e181e6d1b4c0a77564c4e0aff` |
| `usr/share/scanner-kiosk/notices/apkbuild/sane-airscan.APKBUILD` | [retained/apkbuild/sane-airscan.APKBUILD](native/notice-files/retained/apkbuild/sane-airscan.APKBUILD) | `2fe49677adb9e576e03fc1383233640fcbed5245351ab5d6e93175c5320ac54c` |
| `usr/share/scanner-kiosk/notices/apkbuild/sane.APKBUILD` | [retained/apkbuild/sane.APKBUILD](native/notice-files/retained/apkbuild/sane.APKBUILD) | `6177156ccd65ffeb5c7ebdb61dc9dc4596a48c1d2098b7c01373e60623cf43c4` |
| `usr/share/scanner-kiosk/notices/apkbuild/tesseract-ocr.APKBUILD` | [retained/apkbuild/tesseract-ocr.APKBUILD](native/notice-files/retained/apkbuild/tesseract-ocr.APKBUILD) | `60f136bf6e62a470c4c302346d57e7f8e11e831f221bf7834f393af880b61ab6` |
| `usr/share/scanner-kiosk/notices/delivery-selection.json` | [retained/delivery-selection.json](native/notice-files/retained/delivery-selection.json) | `6a822eadaa1a98c7e7cd027ca7c7d753c37029c0c71a98efef5cb821a7837ee8` |
| `usr/share/scanner-kiosk/notices/go/index.json` | [retained/go/index.json](native/notice-files/retained/go/index.json) | `d9d4e43412e6153dbffa083c1aea49ba784f8e3e2807937ffe79791968962e5e` |
| `usr/share/scanner-kiosk/notices/go/modules/090a936f6b69a25a/AUTHORS` | [retained/go/modules/090a936f6b69a25a/AUTHORS](native/notice-files/retained/go/modules/090a936f6b69a25a/AUTHORS) | `86555660d038cc73180fbc221e83ce43ac6624c5b4ce629172e0011df06b321d` |
| `usr/share/scanner-kiosk/notices/go/modules/090a936f6b69a25a/LICENSE` | [retained/go/modules/090a936f6b69a25a/LICENSE](native/notice-files/retained/go/modules/090a936f6b69a25a/LICENSE) | `c6fe05491a60ae13bcd223088d2705e36dede24e5587226231d2459ada5c4822` |
| `usr/share/scanner-kiosk/notices/go/modules/090a936f6b69a25a/LICENSE-3RD-PARTY.md` | [retained/go/modules/090a936f6b69a25a/LICENSE-3RD-PARTY.md](native/notice-files/retained/go/modules/090a936f6b69a25a/LICENSE-3RD-PARTY.md) | `4194f8e7119be72babd499db4f6f1f7547e36e8a7bffcb2b9c8df241b8f1576a` |
| `usr/share/scanner-kiosk/notices/go/modules/090a936f6b69a25a/LICENSE-SQLITE` | [retained/go/modules/090a936f6b69a25a/LICENSE-SQLITE](native/notice-files/retained/go/modules/090a936f6b69a25a/LICENSE-SQLITE) | `8438c9c89b849131ead81d5435cb97fcf052df5b0b286dda8a2d4c29e6cb3fd0` |
| `usr/share/scanner-kiosk/notices/go/modules/090a936f6b69a25a/LICENSE-SQLITE_VEC` | [retained/go/modules/090a936f6b69a25a/LICENSE-SQLITE_VEC](native/notice-files/retained/go/modules/090a936f6b69a25a/LICENSE-SQLITE_VEC) | `6ce72bbe12d975bd5286e5ab0a064c069693300c47bccbc57bec18485f1621ea` |
| `usr/share/scanner-kiosk/notices/go/modules/0d1e2d67ae64f2b5/LICENSE` | [retained/go/modules/0d1e2d67ae64f2b5/LICENSE](native/notice-files/retained/go/modules/0d1e2d67ae64f2b5/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/0d1e2d67ae64f2b5/PATENTS` | [retained/go/modules/0d1e2d67ae64f2b5/PATENTS](native/notice-files/retained/go/modules/0d1e2d67ae64f2b5/PATENTS) | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc` |
| `usr/share/scanner-kiosk/notices/go/modules/108dfdc5a4ad9af3/LICENSE` | [retained/go/modules/108dfdc5a4ad9af3/LICENSE](native/notice-files/retained/go/modules/108dfdc5a4ad9af3/LICENSE) | `09e8a9bcec8067104652c168685ab0931e7868f9c8284b66f5ae6edae5f1130b` |
| `usr/share/scanner-kiosk/notices/go/modules/126ca532490193ba/LICENSE` | [retained/go/modules/126ca532490193ba/LICENSE](native/notice-files/retained/go/modules/126ca532490193ba/LICENSE) | `2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067` |
| `usr/share/scanner-kiosk/notices/go/modules/126ca532490193ba/internal/lzw/LICENSE` | [retained/go/modules/126ca532490193ba/internal/lzw/LICENSE](native/notice-files/retained/go/modules/126ca532490193ba/internal/lzw/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/12802620d9f724f9/LICENSE` | [retained/go/modules/12802620d9f724f9/LICENSE](native/notice-files/retained/go/modules/12802620d9f724f9/LICENSE) | `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30` |
| `usr/share/scanner-kiosk/notices/go/modules/12802620d9f724f9/json/LICENSE` | [retained/go/modules/12802620d9f724f9/json/LICENSE](native/notice-files/retained/go/modules/12802620d9f724f9/json/LICENSE) | `dd26a7abddd02e2d0aba97805b31f248ef7835d9e10da289b22e3b8ab78b324d` |
| `usr/share/scanner-kiosk/notices/go/modules/1e7b1aaf85f9116b/LICENSE` | [retained/go/modules/1e7b1aaf85f9116b/LICENSE](native/notice-files/retained/go/modules/1e7b1aaf85f9116b/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/1e7b1aaf85f9116b/PATENTS` | [retained/go/modules/1e7b1aaf85f9116b/PATENTS](native/notice-files/retained/go/modules/1e7b1aaf85f9116b/PATENTS) | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc` |
| `usr/share/scanner-kiosk/notices/go/modules/37a1867ef0a7f064/LICENSE` | [retained/go/modules/37a1867ef0a7f064/LICENSE](native/notice-files/retained/go/modules/37a1867ef0a7f064/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/37a1867ef0a7f064/PATENTS` | [retained/go/modules/37a1867ef0a7f064/PATENTS](native/notice-files/retained/go/modules/37a1867ef0a7f064/PATENTS) | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc` |
| `usr/share/scanner-kiosk/notices/go/modules/4182e4eea1eec26c/LICENSE` | [retained/go/modules/4182e4eea1eec26c/LICENSE](native/notice-files/retained/go/modules/4182e4eea1eec26c/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/4182e4eea1eec26c/PATENTS` | [retained/go/modules/4182e4eea1eec26c/PATENTS](native/notice-files/retained/go/modules/4182e4eea1eec26c/PATENTS) | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc` |
| `usr/share/scanner-kiosk/notices/go/modules/4fd601d1d188212e/LICENSE` | [retained/go/modules/4fd601d1d188212e/LICENSE](native/notice-files/retained/go/modules/4fd601d1d188212e/LICENSE) | `07e9e3daaccc707c9b7d535ed1405a65d37919fdd42197fe55e68820e974688c` |
| `usr/share/scanner-kiosk/notices/go/modules/51bf2a17bdc4c906/LICENSE` | [retained/go/modules/51bf2a17bdc4c906/LICENSE](native/notice-files/retained/go/modules/51bf2a17bdc4c906/LICENSE) | `a973b4498c13eb74baa2a8e5c351426a6826f2fcdd909916dbe53ee2e755fd71` |
| `usr/share/scanner-kiosk/notices/go/modules/636809519be14c75/LICENSE` | [retained/go/modules/636809519be14c75/LICENSE](native/notice-files/retained/go/modules/636809519be14c75/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/6cdec685f8b76c93/LICENSE.txt` | [retained/go/modules/6cdec685f8b76c93/LICENSE.txt](native/notice-files/retained/go/modules/6cdec685f8b76c93/LICENSE.txt) | `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30` |
| `usr/share/scanner-kiosk/notices/go/modules/6cdec685f8b76c93/internal/filter/lzw/LICENSE` | [retained/go/modules/6cdec685f8b76c93/internal/filter/lzw/LICENSE](native/notice-files/retained/go/modules/6cdec685f8b76c93/internal/filter/lzw/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/modules/6cdec685f8b76c93/pkg/pdfcpu/pkcs7/LICENSE` | [retained/go/modules/6cdec685f8b76c93/pkg/pdfcpu/pkcs7/LICENSE](native/notice-files/retained/go/modules/6cdec685f8b76c93/pkg/pdfcpu/pkcs7/LICENSE) | `0e8bec4750de7c8b2fdef97083c4fe1f9f5b7a0a71ca1ccf781fc2b135284ce0` |
| `usr/share/scanner-kiosk/notices/go/modules/6e4ce618136452ca/LICENSE` | [retained/go/modules/6e4ce618136452ca/LICENSE](native/notice-files/retained/go/modules/6e4ce618136452ca/LICENSE) | `d18f6323b71b0b768bb5e9616e36da390fbd39369a81807cca352de4e4e6aa0b` |
| `usr/share/scanner-kiosk/notices/go/modules/6e4ce618136452ca/NOTICE` | [retained/go/modules/6e4ce618136452ca/NOTICE](native/notice-files/retained/go/modules/6e4ce618136452ca/NOTICE) | `f6c2dd3a67b576eafb89b80200b8b1627230bf3821a0c14cb99a22ac19107d00` |
| `usr/share/scanner-kiosk/notices/go/modules/790f498af6e56cbc/LICENSE` | [retained/go/modules/790f498af6e56cbc/LICENSE](native/notice-files/retained/go/modules/790f498af6e56cbc/LICENSE) | `60aa83c124df5a317fe85b1ad9c4984ad056e0221d572d195b00d5b45b8f3bb1` |
| `usr/share/scanner-kiosk/notices/go/modules/7cd0fd73658571a4/LICENSE` | [retained/go/modules/7cd0fd73658571a4/LICENSE](native/notice-files/retained/go/modules/7cd0fd73658571a4/LICENSE) | `cb5e8e7e5f4a3988e1063c142c60dc2df75605f4c46515e776e3aca6df976e14` |
| `usr/share/scanner-kiosk/notices/go/modules/7cd0fd73658571a4/NOTICE` | [retained/go/modules/7cd0fd73658571a4/NOTICE](native/notice-files/retained/go/modules/7cd0fd73658571a4/NOTICE) | `dccd26c6fd9c296daf44d0bc56bb4efc566edd4880381b3331c9a63e6e471338` |
| `usr/share/scanner-kiosk/notices/go/modules/baa659b49de75956/AUTHORS` | [retained/go/modules/baa659b49de75956/AUTHORS](native/notice-files/retained/go/modules/baa659b49de75956/AUTHORS) | `df1f424436f279309de85b82e494d548297a2570e9b5794630d7e72c9eb50b6a` |
| `usr/share/scanner-kiosk/notices/go/modules/baa659b49de75956/LICENSE` | [retained/go/modules/baa659b49de75956/LICENSE](native/notice-files/retained/go/modules/baa659b49de75956/LICENSE) | `59895e669f48f168b6b858358f6005779cdf40a265f7828813061b56af67b496` |
| `usr/share/scanner-kiosk/notices/go/modules/baa659b49de75956/LICENSE-GO` | [retained/go/modules/baa659b49de75956/LICENSE-GO](native/notice-files/retained/go/modules/baa659b49de75956/LICENSE-GO) | `2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067` |
| `usr/share/scanner-kiosk/notices/go/modules/baa659b49de75956/LICENSE-LOGO` | [retained/go/modules/baa659b49de75956/LICENSE-LOGO](native/notice-files/retained/go/modules/baa659b49de75956/LICENSE-LOGO) | `5ae5bee3072a841376451b48d8cfcec7188e10543926d5870828d36c8a750dc5` |
| `usr/share/scanner-kiosk/notices/go/modules/baa659b49de75956/LICENSE-MMAP-GO` | [retained/go/modules/baa659b49de75956/LICENSE-MMAP-GO](native/notice-files/retained/go/modules/baa659b49de75956/LICENSE-MMAP-GO) | `c2eba69f20d05414538c3a5df7694dde392e065ff70882e1625e90f5d6659fff` |
| `usr/share/scanner-kiosk/notices/go/modules/bc866d47782e62af/AUTHORS` | [retained/go/modules/bc866d47782e62af/AUTHORS](native/notice-files/retained/go/modules/bc866d47782e62af/AUTHORS) | `51fcdd68bb605f080283bb6eabb72452591289650599d3d67603381834e536af` |
| `usr/share/scanner-kiosk/notices/go/modules/bc866d47782e62af/LICENSE` | [retained/go/modules/bc866d47782e62af/LICENSE](native/notice-files/retained/go/modules/bc866d47782e62af/LICENSE) | `bfa9bf72a72ca009fd62a8f84fca3dca67e51d93af96352723646599898b6cf5` |
| `usr/share/scanner-kiosk/notices/go/modules/bc866d47782e62af/mersenne/AUTHORS` | [retained/go/modules/bc866d47782e62af/mersenne/AUTHORS](native/notice-files/retained/go/modules/bc866d47782e62af/mersenne/AUTHORS) | `f79ff8b413938c3ce7c60db12242a663d8e840249bd838fcf58f4aaccfec5454` |
| `usr/share/scanner-kiosk/notices/go/modules/bc866d47782e62af/mersenne/LICENSE` | [retained/go/modules/bc866d47782e62af/mersenne/LICENSE](native/notice-files/retained/go/modules/bc866d47782e62af/mersenne/LICENSE) | `c05630f9ca899ea094620d37daea6ba66f99adbf9268ea5fea9db607e1102bd9` |
| `usr/share/scanner-kiosk/notices/go/modules/ccde9cb2b3b92254/LICENSE` | [retained/go/modules/ccde9cb2b3b92254/LICENSE](native/notice-files/retained/go/modules/ccde9cb2b3b92254/LICENSE) | `dd26a7abddd02e2d0aba97805b31f248ef7835d9e10da289b22e3b8ab78b324d` |
| `usr/share/scanner-kiosk/notices/go/modules/d0d666767bf9063c/LICENSE` | [retained/go/modules/d0d666767bf9063c/LICENSE](native/notice-files/retained/go/modules/d0d666767bf9063c/LICENSE) | `0a8d61ed3cbfd5312326e8126c31ce9c627a283adc99131b56896d29ada04b2d` |
| `usr/share/scanner-kiosk/notices/go/modules/d5394666bff0dd4c/LICENSE` | [retained/go/modules/d5394666bff0dd4c/LICENSE](native/notice-files/retained/go/modules/d5394666bff0dd4c/LICENSE) | `88a2379b3ca34bf5c57127aff9dcb802bbb60ece0805cdbda65b3bd115f971d9` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/AUTHORS` | [retained/go/modules/e7e9a74be99acf27/AUTHORS](native/notice-files/retained/go/modules/e7e9a74be99acf27/AUTHORS) | `40224528f3c7363534f59521d803a4eb627966b334d732bfc68a788655cb2545` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/LICENSE` | [retained/go/modules/e7e9a74be99acf27/LICENSE](native/notice-files/retained/go/modules/e7e9a74be99acf27/LICENSE) | `95ff867eb55a56935fa7492406cfa953fb7c13ca73f4c0a86ae05756b4605600` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/LICENSE-3RD-PARTY.md` | [retained/go/modules/e7e9a74be99acf27/LICENSE-3RD-PARTY.md](native/notice-files/retained/go/modules/e7e9a74be99acf27/LICENSE-3RD-PARTY.md) | `f597097efe3d97021f89170746bd3a0fb9a8b6fb26b82043ed68a4e0283bee6c` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/AUTHORS` | [retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/AUTHORS](native/notice-files/retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/AUTHORS) | `d04faa7fbed5d7e30d5f55c014c0f5e702245ca5ba62a39c7bc4df8724853a6c` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/COPYRIGHT` | [retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/COPYRIGHT](native/notice-files/retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/COPYRIGHT) | `41b8f2e2706012eb534f89e72ff547308cb81dcb9acf077c40a9058a7c06088d` |
| `usr/share/scanner-kiosk/notices/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/src/math/crlibm/COPYING` | [retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/src/math/crlibm/COPYING](native/notice-files/retained/go/modules/e7e9a74be99acf27/testdata/nsz.repo.hu/libc-test/src/math/crlibm/COPYING) | `dcc100d4161cc0b7177545ab6e47216f84857cda3843847c792a25289852dcaa` |
| `usr/share/scanner-kiosk/notices/go/stdlib/LICENSE` | [retained/go/stdlib/LICENSE](native/notice-files/retained/go/stdlib/LICENSE) | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `usr/share/scanner-kiosk/notices/go/stdlib/PATENTS` | [retained/go/stdlib/PATENTS](native/notice-files/retained/go/stdlib/PATENTS) | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc` |
| `usr/share/scanner-kiosk/notices/image-notices-receipt.json` | [retained/image-notices-receipt.json](native/notice-files/retained/image-notices-receipt.json) | `2779eb3d546435656e67e913a27c696f7b8218fd658c7ae70760e66c119957f3` |
| `usr/share/scanner-kiosk/notices/native/avahi-0.9-rc4/LICENSE` | [avahi-0.9-rc4/LICENSE](native/notice-files/avahi-0.9-rc4/LICENSE) | `a9bdde5616ecdd1e980b44f360600ee8783b1f99b8cc83a2beb163a0a390e861` |
| `usr/share/scanner-kiosk/notices/native/avahi-0.9-rc4/source.json` | [avahi-0.9-rc4/source.json](native/notice-files/avahi-0.9-rc4/source.json) | `f689e8614078e9b832d1bf1a00a4c9dea1c9e0fa876a28d509eb85962d6e4fea` |
| `usr/share/scanner-kiosk/notices/native/brotli-1.2.1/LICENSE` | [brotli-1.2.1/LICENSE](native/notice-files/brotli-1.2.1/LICENSE) | `3d180008e36922a4e8daec11c34c7af264fed5962d07924aea928c38e8663c94` |
| `usr/share/scanner-kiosk/notices/native/brotli-1.2.1/source.json` | [brotli-1.2.1/source.json](native/notice-files/brotli-1.2.1/source.json) | `6cc4a729db12e7c4ea046c43c506fec9a32c5961434f571d9b4ef2042b0691b8` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/AUTHORS` | [dbus-1.16.2/AUTHORS](native/notice-files/dbus-1.16.2/AUTHORS) | `a56f6628c407e46ec72eb5512591b4c6204e7c0854c15ed0af4daad807edb84f` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/COPYING` | [dbus-1.16.2/COPYING](native/notice-files/dbus-1.16.2/COPYING) | `150e5d08e1cb5e62b65967d7931acad2c42d98ec1bbb584bebb01921cbc853c2` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--AFL-2.0.txt` | [dbus-1.16.2/LICENSES--AFL-2.0.txt](native/notice-files/dbus-1.16.2/LICENSES--AFL-2.0.txt) | `7ecc7db84298adb05d2cf10339c63a5315a1c5c270a68d97d7d40a53b508ccde` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--AFL-2.1.txt` | [dbus-1.16.2/LICENSES--AFL-2.1.txt](native/notice-files/dbus-1.16.2/LICENSES--AFL-2.1.txt) | `93bb4f7417aa775bab9026cc3d0af28aeb64451fc5a8ec651876785edd8eaf9b` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--Apache-2.0.txt` | [dbus-1.16.2/LICENSES--Apache-2.0.txt](native/notice-files/dbus-1.16.2/LICENSES--Apache-2.0.txt) | `074e6e32c86a4c0ef8b3ed25b721ca23aca83df277cd88106ef7177c354615ff` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--BSD-3-Clause.txt` | [dbus-1.16.2/LICENSES--BSD-3-Clause.txt](native/notice-files/dbus-1.16.2/LICENSES--BSD-3-Clause.txt) | `5a93d5831e1297ab10fe643e1a631e83be392896da14ee2951285a79012df69d` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--GPL-2.0-or-later.txt` | [dbus-1.16.2/LICENSES--GPL-2.0-or-later.txt](native/notice-files/dbus-1.16.2/LICENSES--GPL-2.0-or-later.txt) | `aaf135472f81c5b4a0dca9367e5bb5e9750032b5bebe5442b36e4c0a47430df3` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--LGPL-2.1-or-later.txt` | [dbus-1.16.2/LICENSES--LGPL-2.1-or-later.txt](native/notice-files/dbus-1.16.2/LICENSES--LGPL-2.1-or-later.txt) | `fa6f36630bb1e0c571d34b2bbdf188d08495c9dbf58f28cac112f303fc1f58fb` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--LicenseRef-CMakeScripts.txt` | [dbus-1.16.2/LICENSES--LicenseRef-CMakeScripts.txt](native/notice-files/dbus-1.16.2/LICENSES--LicenseRef-CMakeScripts.txt) | `a4c1387064fda4c22bfca364b606eb4e0421efa37cad27c040a3d7ca7186d522` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--LicenseRef-GAP.txt` | [dbus-1.16.2/LICENSES--LicenseRef-GAP.txt](native/notice-files/dbus-1.16.2/LICENSES--LicenseRef-GAP.txt) | `cf4bc4c56dd0837f3d53b8c1467e0d7dbb95223de8bcfe83a4981c318a738ae3` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--LicenseRef-pycrypto-orig.txt` | [dbus-1.16.2/LICENSES--LicenseRef-pycrypto-orig.txt](native/notice-files/dbus-1.16.2/LICENSES--LicenseRef-pycrypto-orig.txt) | `2cb8f4f410f0ffc5dcfbadd188e20c859fdbb161e616cf2f3d34dfbf8c740547` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--MIT.txt` | [dbus-1.16.2/LICENSES--MIT.txt](native/notice-files/dbus-1.16.2/LICENSES--MIT.txt) | `b85dcd3e453d05982552c52b5fc9e0bdd6d23c6f8e844b984a88af32570b0cc0` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/LICENSES--TCL.txt` | [dbus-1.16.2/LICENSES--TCL.txt](native/notice-files/dbus-1.16.2/LICENSES--TCL.txt) | `36fc2d8a56a2a014ded51e298a7e63635eb4ec62e8bedb99c82b892542ac5e7d` |
| `usr/share/scanner-kiosk/notices/native/dbus-1.16.2/source.json` | [dbus-1.16.2/source.json](native/notice-files/dbus-1.16.2/source.json) | `5bf3b41dd17a90b1eb3150f69a656713de5ed8fb36afd11a8042156da68f14bc` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/COPYING` | [gcc-15.2.0/COPYING](native/notice-files/gcc-15.2.0/COPYING) | `231f7edcc7352d7734a96eef0b8030f77982678c516876fcb81e25b32d68564c` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/COPYING.LIB` | [gcc-15.2.0/COPYING.LIB](native/notice-files/gcc-15.2.0/COPYING.LIB) | `a9bdde5616ecdd1e980b44f360600ee8783b1f99b8cc83a2beb163a0a390e861` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/COPYING.RUNTIME` | [gcc-15.2.0/COPYING.RUNTIME](native/notice-files/gcc-15.2.0/COPYING.RUNTIME) | `9d6b43ce4d8de0c878bf16b54d8e7a10d9bd42b75178153e3af6a815bdc90f74` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/COPYING3` | [gcc-15.2.0/COPYING3](native/notice-files/gcc-15.2.0/COPYING3) | `8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/COPYING3.LIB` | [gcc-15.2.0/COPYING3.LIB](native/notice-files/gcc-15.2.0/COPYING3.LIB) | `a853c2ffec17057872340eee242ae4d96cbf2b520ae27d903e1b2fef1a5f9d1c` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/libgcc--libgcc2.c.notice.txt` | [gcc-15.2.0/libgcc--libgcc2.c.notice.txt](native/notice-files/gcc-15.2.0/libgcc--libgcc2.c.notice.txt) | `339909457c9ffd6328dcad49cc03155a72d6f27f0ae37e3c32d835c3493745a9` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/libgomp--libgomp.h.notice.txt` | [gcc-15.2.0/libgomp--libgomp.h.notice.txt](native/notice-files/gcc-15.2.0/libgomp--libgomp.h.notice.txt) | `046c131874b7a208e212af311bb4f7da51effe3012ccf764dea27abca79299fa` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/libstdc++-v3--include--bits--stl_vector.h.notice.txt` | [gcc-15.2.0/libstdc++-v3--include--bits--stl_vector.h.notice.txt](native/notice-files/gcc-15.2.0/libstdc++-v3--include--bits--stl_vector.h.notice.txt) | `b8ef9d594258e73babd585aa3c4965b1c6e274712a6153188fd8cc9102779191` |
| `usr/share/scanner-kiosk/notices/native/gcc-15.2.0/source.json` | [gcc-15.2.0/source.json](native/notice-files/gcc-15.2.0/source.json) | `53f5d40cf8d60ee57e373fc833cad5fa3e4cce2b7ca964bf5a682505045d81f7` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/AUTHORS` | [gettext-1.0/AUTHORS](native/notice-files/gettext-1.0/AUTHORS) | `167b04ed012e59c2d2cfc87d420d8c8a8f5c528ef1fd068598b36d57f39db203` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/COPYING` | [gettext-1.0/COPYING](native/notice-files/gettext-1.0/COPYING) | `e79e9c8a0c85d735ff98185918ec94ed7d175efc377012787aebcf3b80f0d90b` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/gettext-runtime--intl--AUTHORS` | [gettext-1.0/gettext-runtime--intl--AUTHORS](native/notice-files/gettext-1.0/gettext-runtime--intl--AUTHORS) | `4bd9e2cd15ab6d68b30965512b63a1d4aae95f4c1dd040657a4d1300252a695f` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/gettext-runtime--intl--COPYING.LIB` | [gettext-1.0/gettext-runtime--intl--COPYING.LIB](native/notice-files/gettext-1.0/gettext-runtime--intl--COPYING.LIB) | `20e50fe7aae3e56378ebf0417d9de904f55a0e61e4df315333e632a4d3555d95` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/gettext-runtime--intl--dcigettext.c.notice.txt` | [gettext-1.0/gettext-runtime--intl--dcigettext.c.notice.txt](native/notice-files/gettext-1.0/gettext-runtime--intl--dcigettext.c.notice.txt) | `c301874fcfdb27a49237157c7831e7c645e0a6dc44dcee6592e598e858410e3e` |
| `usr/share/scanner-kiosk/notices/native/gettext-1.0/source.json` | [gettext-1.0/source.json](native/notice-files/gettext-1.0/source.json) | `5bc30063af3137284cdfd83c39e751c7b79dba538bb121cf7b4a8be7613a9a7d` |
| `usr/share/scanner-kiosk/notices/native/giflib-5.2.2/COPYING` | [giflib-5.2.2/COPYING](native/notice-files/giflib-5.2.2/COPYING) | `0c9b7990ecdca88b676db232c226548ac408b279f550d424d996f0d83591dd8e` |
| `usr/share/scanner-kiosk/notices/native/giflib-5.2.2/source.json` | [giflib-5.2.2/source.json](native/notice-files/giflib-5.2.2/source.json) | `19b2a23f5467bb8d83d69df4ada367937c6f9fc10feceb44757502bdf7ccf9a2` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/AUTHORS` | [gmp-6.3.0/AUTHORS](native/notice-files/gmp-6.3.0/AUTHORS) | `da5870e2f0482c5dc15b4cfed8bbfb51ce1279c8f8fc52bdb8c13c7cee949c32` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/COPYING` | [gmp-6.3.0/COPYING](native/notice-files/gmp-6.3.0/COPYING) | `8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/COPYING.LESSERv3` | [gmp-6.3.0/COPYING.LESSERv3](native/notice-files/gmp-6.3.0/COPYING.LESSERv3) | `a853c2ffec17057872340eee242ae4d96cbf2b520ae27d903e1b2fef1a5f9d1c` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/COPYINGv2` | [gmp-6.3.0/COPYINGv2](native/notice-files/gmp-6.3.0/COPYINGv2) | `8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/COPYINGv3` | [gmp-6.3.0/COPYINGv3](native/notice-files/gmp-6.3.0/COPYINGv3) | `e6037104443f9a7829b2aa7c5370d0789a7bda3ca65a0b904cdc0c2e285d9195` |
| `usr/share/scanner-kiosk/notices/native/gmp-6.3.0/source.json` | [gmp-6.3.0/source.json](native/notice-files/gmp-6.3.0/source.json) | `990c19438e578dd2e06c9c8f96d66338eee4c3210c1b93e5fa40c10b10b4d90f` |
| `usr/share/scanner-kiosk/notices/native/gnutls-3.8.13/AUTHORS` | [gnutls-3.8.13/AUTHORS](native/notice-files/gnutls-3.8.13/AUTHORS) | `79bca1136dc515b930f2f85975c0c4c687e7d430be27a9b59113637c33b8e7a3` |
| `usr/share/scanner-kiosk/notices/native/gnutls-3.8.13/COPYING` | [gnutls-3.8.13/COPYING](native/notice-files/gnutls-3.8.13/COPYING) | `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` |
| `usr/share/scanner-kiosk/notices/native/gnutls-3.8.13/COPYING.LESSERv2` | [gnutls-3.8.13/COPYING.LESSERv2](native/notice-files/gnutls-3.8.13/COPYING.LESSERv2) | `20e50fe7aae3e56378ebf0417d9de904f55a0e61e4df315333e632a4d3555d95` |
| `usr/share/scanner-kiosk/notices/native/gnutls-3.8.13/source.json` | [gnutls-3.8.13/source.json](native/notice-files/gnutls-3.8.13/source.json) | `2e47fda97c88361aac4f85a57561e4823a07b4f053132836357821e0dd214819` |
| `usr/share/scanner-kiosk/notices/native/leptonica-1.87.0/LICENSE` | [leptonica-1.87.0/LICENSE](native/notice-files/leptonica-1.87.0/LICENSE) | `87829abb5bbb00b55a107365da89e9a33f86c4250169e5a1e5588505be7d5806` |
| `usr/share/scanner-kiosk/notices/native/leptonica-1.87.0/source.json` | [leptonica-1.87.0/source.json](native/notice-files/leptonica-1.87.0/source.json) | `f2f9e3f987d541209d296bccd74b8cf3ad22d9cf22e95aafb0c5835177dd76de` |
| `usr/share/scanner-kiosk/notices/native/libffi-3.5.2/LICENSE` | [libffi-3.5.2/LICENSE](native/notice-files/libffi-3.5.2/LICENSE) | `d5699fa516968e3a1550e6c902b7441c78856f2603d61bedd0b0662a26655366` |
| `usr/share/scanner-kiosk/notices/native/libffi-3.5.2/linux-x86_64-source-notices.txt` | [libffi-3.5.2/linux-x86_64-source-notices.txt](native/notice-files/libffi-3.5.2/linux-x86_64-source-notices.txt) | `85b3ab9b4bbe82811cf6f97542717e4913802fdf9d50407e4c8017bf4df1b679` |
| `usr/share/scanner-kiosk/notices/native/libffi-3.5.2/source.json` | [libffi-3.5.2/source.json](native/notice-files/libffi-3.5.2/source.json) | `5042a8403b6d49175950eeb75bd893582f89e88cc68a96c551d192ad2d4ae59d` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/AUTHORS` | [libidn2-2.3.8/AUTHORS](native/notice-files/libidn2-2.3.8/AUTHORS) | `7719304df806ca5cd68c1f1c7e0139e0c11ed14fede560ae192d3c4d1989e839` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/COPYING` | [libidn2-2.3.8/COPYING](native/notice-files/libidn2-2.3.8/COPYING) | `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/COPYING.LESSERv3` | [libidn2-2.3.8/COPYING.LESSERv3](native/notice-files/libidn2-2.3.8/COPYING.LESSERv3) | `e3a994d82e644b03a792a930f574002658412f62407f5fee083f2555c5f23118` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/COPYING.unicode` | [libidn2-2.3.8/COPYING.unicode](native/notice-files/libidn2-2.3.8/COPYING.unicode) | `01d621eef165cf4d3d3dbb737aa0699178d94c6f18cf87e9dde6db3ca7790f46` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/COPYINGv2` | [libidn2-2.3.8/COPYINGv2](native/notice-files/libidn2-2.3.8/COPYINGv2) | `edaef632cbb643e4e7a221717a6c441a4c1a7c918e6e4d56debc3d8739b233f6` |
| `usr/share/scanner-kiosk/notices/native/libidn2-2.3.8/source.json` | [libidn2-2.3.8/source.json](native/notice-files/libidn2-2.3.8/source.json) | `5810d6fb040e74e850c09916a973034122fa29fe36f9b80159085c7407a0ef61` |
| `usr/share/scanner-kiosk/notices/native/libjpeg-turbo-3.1.3/ATTRIBUTION.txt` | [libjpeg-turbo-3.1.3/ATTRIBUTION.txt](native/notice-files/libjpeg-turbo-3.1.3/ATTRIBUTION.txt) | `d903a69ab39425283b44e2ea355cbfaf79bcd738b69c2b1c3c700ce80f2d89f9` |
| `usr/share/scanner-kiosk/notices/native/libjpeg-turbo-3.1.3/LICENSE.md` | [libjpeg-turbo-3.1.3/LICENSE.md](native/notice-files/libjpeg-turbo-3.1.3/LICENSE.md) | `2189dc45a8fe96204069f8124caa53a148dfdc193f50f584c6ca7849a6072872` |
| `usr/share/scanner-kiosk/notices/native/libjpeg-turbo-3.1.3/README.ijg` | [libjpeg-turbo-3.1.3/README.ijg](native/notice-files/libjpeg-turbo-3.1.3/README.ijg) | `75815e3bf6484201a3c3d17a1bbf10f2e8e3237f84df10a2357ea896db2a81d6` |
| `usr/share/scanner-kiosk/notices/native/libjpeg-turbo-3.1.3/source.json` | [libjpeg-turbo-3.1.3/source.json](native/notice-files/libjpeg-turbo-3.1.3/source.json) | `99cccf8c4397639b8652fc1a701b98fa730dc4fbaa0207debd7adbfce990d55f` |
| `usr/share/scanner-kiosk/notices/native/libpng-1.6.59/LICENSE` | [libpng-1.6.59/LICENSE](native/notice-files/libpng-1.6.59/LICENSE) | `bdb0a645ea18c60507d0368379b1ac5474b92255fcc2d115e07486a7672ba526` |
| `usr/share/scanner-kiosk/notices/native/libpng-1.6.59/source.json` | [libpng-1.6.59/source.json](native/notice-files/libpng-1.6.59/source.json) | `dacbf32cab9e3be27200dc33349ad7d14633dd7ab0250319edcee1ae1367dd69` |
| `usr/share/scanner-kiosk/notices/native/libtasn1-4.21.0/AUTHORS` | [libtasn1-4.21.0/AUTHORS](native/notice-files/libtasn1-4.21.0/AUTHORS) | `34384e8860c713e31e9339a5d4eb0727a9a93d2a5bb74b36053d9a66920bff9e` |
| `usr/share/scanner-kiosk/notices/native/libtasn1-4.21.0/COPYING` | [libtasn1-4.21.0/COPYING](native/notice-files/libtasn1-4.21.0/COPYING) | `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` |
| `usr/share/scanner-kiosk/notices/native/libtasn1-4.21.0/COPYING.LESSERv2` | [libtasn1-4.21.0/COPYING.LESSERv2](native/notice-files/libtasn1-4.21.0/COPYING.LESSERv2) | `20e50fe7aae3e56378ebf0417d9de904f55a0e61e4df315333e632a4d3555d95` |
| `usr/share/scanner-kiosk/notices/native/libtasn1-4.21.0/source.json` | [libtasn1-4.21.0/source.json](native/notice-files/libtasn1-4.21.0/source.json) | `02d3504426f59c07c8e528b233378fa3a7a4a44cf9e8febcc8b24757a453195a` |
| `usr/share/scanner-kiosk/notices/native/libunistring-1.4.2/AUTHORS` | [libunistring-1.4.2/AUTHORS](native/notice-files/libunistring-1.4.2/AUTHORS) | `9877ee5d24b5fa059fcc3ff2f4af36918f7c5676de000b9cb5ec11efa6aac149` |
| `usr/share/scanner-kiosk/notices/native/libunistring-1.4.2/COPYING` | [libunistring-1.4.2/COPYING](native/notice-files/libunistring-1.4.2/COPYING) | `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` |
| `usr/share/scanner-kiosk/notices/native/libunistring-1.4.2/COPYING.LIB` | [libunistring-1.4.2/COPYING.LIB](native/notice-files/libunistring-1.4.2/COPYING.LIB) | `a853c2ffec17057872340eee242ae4d96cbf2b520ae27d903e1b2fef1a5f9d1c` |
| `usr/share/scanner-kiosk/notices/native/libunistring-1.4.2/source.json` | [libunistring-1.4.2/source.json](native/notice-files/libunistring-1.4.2/source.json) | `bf6fa2085c7ccb8a52c693ac170fcee6da2e135a51feb972c1f55ff7f24f2b65` |
| `usr/share/scanner-kiosk/notices/native/libusb-1.0.30/AUTHORS` | [libusb-1.0.30/AUTHORS](native/notice-files/libusb-1.0.30/AUTHORS) | `27fd82b016838197d5276ba98d2187ed5a1ad0e171767322f356f0a9f6395941` |
| `usr/share/scanner-kiosk/notices/native/libusb-1.0.30/COPYING` | [libusb-1.0.30/COPYING](native/notice-files/libusb-1.0.30/COPYING) | `5df07007198989c622f5d41de8d703e7bef3d0e79d62e24332ee739a452af62a` |
| `usr/share/scanner-kiosk/notices/native/libusb-1.0.30/source.json` | [libusb-1.0.30/source.json](native/notice-files/libusb-1.0.30/source.json) | `f4f619f7701a51e973f741540696e7d560ee44037e6f27dbf329727eb7608845` |
| `usr/share/scanner-kiosk/notices/native/libwebp-1.6.0/AUTHORS` | [libwebp-1.6.0/AUTHORS](native/notice-files/libwebp-1.6.0/AUTHORS) | `f251be89fd442bd9d701360730aea163cc8e2076ff03528f59267dc2c4f8b60a` |
| `usr/share/scanner-kiosk/notices/native/libwebp-1.6.0/COPYING` | [libwebp-1.6.0/COPYING](native/notice-files/libwebp-1.6.0/COPYING) | `5aec868f669e384a22372a4e8a1a6cd7d44c64cd451f960ca69cc170d1e13acf` |
| `usr/share/scanner-kiosk/notices/native/libwebp-1.6.0/PATENTS` | [libwebp-1.6.0/PATENTS](native/notice-files/libwebp-1.6.0/PATENTS) | `cc3273e0694ea5896145e0677699b53471b03ea43021ddc50e7923fbb9f5023c` |
| `usr/share/scanner-kiosk/notices/native/libwebp-1.6.0/source.json` | [libwebp-1.6.0/source.json](native/notice-files/libwebp-1.6.0/source.json) | `1bc8601d66117758438a63d5505f69a95c7fe6e5d5e44663871ec538a8dade3b` |
| `usr/share/scanner-kiosk/notices/native/libxml2-2.13.9/Copyright` | [libxml2-2.13.9/Copyright](native/notice-files/libxml2-2.13.9/Copyright) | `c99aae1afe013e50b8b3701e089222b351258043c3025b64053a233fd25b4be7` |
| `usr/share/scanner-kiosk/notices/native/libxml2-2.13.9/dict.c.additional-license.txt` | [libxml2-2.13.9/dict.c.additional-license.txt](native/notice-files/libxml2-2.13.9/dict.c.additional-license.txt) | `8d10f68393d4beba204211ade832f1d92fa5b1226f78481dec7e7ec03b89630e` |
| `usr/share/scanner-kiosk/notices/native/libxml2-2.13.9/dict.c.notice.txt` | [libxml2-2.13.9/dict.c.notice.txt](native/notice-files/libxml2-2.13.9/dict.c.notice.txt) | `a3b2725bfa5d14eaade702d2023c97a6e744a3a8e5f0e8eb0da29d77a11f96c8` |
| `usr/share/scanner-kiosk/notices/native/libxml2-2.13.9/list.c.notice.txt` | [libxml2-2.13.9/list.c.notice.txt](native/notice-files/libxml2-2.13.9/list.c.notice.txt) | `b3b090ffa88200a97a60feda84ebeec4894e139edc4fc9749eb1efeb2686c786` |
| `usr/share/scanner-kiosk/notices/native/libxml2-2.13.9/source.json` | [libxml2-2.13.9/source.json](native/notice-files/libxml2-2.13.9/source.json) | `95112e43ee94e634b9787475b533404a0dd1c1320cdeb24c5d2e6d9321a8ae62` |
| `usr/share/scanner-kiosk/notices/native/musl-1.2.6/COPYRIGHT` | [musl-1.2.6/COPYRIGHT](native/notice-files/musl-1.2.6/COPYRIGHT) | `b870108ec5e7790e9f9919064f1b9421d62d5f9b0e6c230c6adf7ea2da62e97b` |
| `usr/share/scanner-kiosk/notices/native/musl-1.2.6/source.json` | [musl-1.2.6/source.json](native/notice-files/musl-1.2.6/source.json) | `47aca0aebdcd8db267d91d05e88ad5db8c617ad95ec19d33ce8706161f2c88f3` |
| `usr/share/scanner-kiosk/notices/native/musl-1.2.6/third-party-source-notices.txt` | [musl-1.2.6/third-party-source-notices.txt](native/notice-files/musl-1.2.6/third-party-source-notices.txt) | `2f9e533355db39de1b44de8fd63e3b180c8266f8ee5ce9dcbab2a6aa4bd4e5c4` |
| `usr/share/scanner-kiosk/notices/native/nettle-3.10.2/AUTHORS` | [nettle-3.10.2/AUTHORS](native/notice-files/nettle-3.10.2/AUTHORS) | `6fd91e60caa0c33dd22888b710fcfa36532bbbef00d85c78de385dcd97f7b9ee` |
| `usr/share/scanner-kiosk/notices/native/nettle-3.10.2/COPYING.LESSERv3` | [nettle-3.10.2/COPYING.LESSERv3](native/notice-files/nettle-3.10.2/COPYING.LESSERv3) | `a853c2ffec17057872340eee242ae4d96cbf2b520ae27d903e1b2fef1a5f9d1c` |
| `usr/share/scanner-kiosk/notices/native/nettle-3.10.2/COPYINGv2` | [nettle-3.10.2/COPYINGv2](native/notice-files/nettle-3.10.2/COPYINGv2) | `8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643` |
| `usr/share/scanner-kiosk/notices/native/nettle-3.10.2/COPYINGv3` | [nettle-3.10.2/COPYINGv3](native/notice-files/nettle-3.10.2/COPYINGv3) | `e6037104443f9a7829b2aa7c5370d0789a7bda3ca65a0b904cdc0c2e285d9195` |
| `usr/share/scanner-kiosk/notices/native/nettle-3.10.2/source.json` | [nettle-3.10.2/source.json](native/notice-files/nettle-3.10.2/source.json) | `3aa4facacc545b9553bc31e1c2aa965fa4fc5430b8f171cebdd1f326ae03e638` |
| `usr/share/scanner-kiosk/notices/native/p11-kit-0.26.2/COPYING` | [p11-kit-0.26.2/COPYING](native/notice-files/p11-kit-0.26.2/COPYING) | `2e1ba993904df807a10c3eda1e5c272338edc35674b679773a8b3ad460731054` |
| `usr/share/scanner-kiosk/notices/native/p11-kit-0.26.2/library-source-notices.txt` | [p11-kit-0.26.2/library-source-notices.txt](native/notice-files/p11-kit-0.26.2/library-source-notices.txt) | `8601671b1ef73fbe168085298391c7536e15ebaffa495d91e945eeefca810b1d` |
| `usr/share/scanner-kiosk/notices/native/p11-kit-0.26.2/source.json` | [p11-kit-0.26.2/source.json](native/notice-files/p11-kit-0.26.2/source.json) | `d904385ded968b1600142610d31ad19efc0dd5d7f71886267312ab476db8880d` |
| `usr/share/scanner-kiosk/notices/native/provenance.json` | [retained/native/provenance.json](native/notice-files/retained/native/provenance.json) | `2e15a03b26573a3ebedc0e732528e6a7e0546b426be85c94cfb8ec2b38f20eef` |
| `usr/share/scanner-kiosk/notices/native/sane-airscan/COPYING` | [retained/native/sane-airscan/COPYING](native/notice-files/retained/native/sane-airscan/COPYING) | `8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643` |
| `usr/share/scanner-kiosk/notices/native/sane-airscan/LICENSE` | [retained/native/sane-airscan/LICENSE](native/notice-files/retained/native/sane-airscan/LICENSE) | `a2631c1b81992bd6f66db6be8b009ed7e3e998d1e2db4de497fa82d65011d373` |
| `usr/share/scanner-kiosk/notices/native/sane/COPYING` | [retained/native/sane/COPYING](native/notice-files/retained/native/sane/COPYING) | `8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643` |
| `usr/share/scanner-kiosk/notices/native/sane/LICENSE` | [retained/native/sane/LICENSE](native/notice-files/retained/native/sane/LICENSE) | `704fb487721423f16eea1f8f9da208434a0f93bca4a98ce543a0fb2038fb2b38` |
| `usr/share/scanner-kiosk/notices/native/sane/dll.c` | [retained/native/sane/dll.c](native/notice-files/retained/native/sane/dll.c) | `662d68482dc637e728b0db3a0e4c65391b0cedd45a192223c36e7bb06996e36f` |
| `usr/share/scanner-kiosk/notices/native/sane/sane.h` | [retained/native/sane/sane.h](native/notice-files/retained/native/sane/sane.h) | `288334864eb01b4773892a2b696cb12c87bb05fada46cb9d588cb42a732d935d` |
| `usr/share/scanner-kiosk/notices/native/tesseract-ocr/LICENSE` | [retained/native/tesseract-ocr/LICENSE](native/notice-files/retained/native/tesseract-ocr/LICENSE) | `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30` |
| `usr/share/scanner-kiosk/notices/native/tiff-4.7.2/ATTRIBUTION.txt` | [tiff-4.7.2/ATTRIBUTION.txt](native/notice-files/tiff-4.7.2/ATTRIBUTION.txt) | `f211568c9111a65a21e25cb0e257d421e999e1a672c91d6183bcad2134fe898d` |
| `usr/share/scanner-kiosk/notices/native/tiff-4.7.2/LICENSE.md` | [tiff-4.7.2/LICENSE.md](native/notice-files/tiff-4.7.2/LICENSE.md) | `0e27c2382d7b8147972bbb746e04059a1152c8d0fda9d03ef1399d1a433c4ade` |
| `usr/share/scanner-kiosk/notices/native/tiff-4.7.2/source.json` | [tiff-4.7.2/source.json](native/notice-files/tiff-4.7.2/source.json) | `1418cc99c3b58462326cf236424a54049dff6a3dc9d8fe239363f1cfdb5443c7` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/COPYING` | [xz-5.8.4/COPYING](native/notice-files/xz-5.8.4/COPYING) | `616a3ad264ce29b8f1cb97e53037b139d406899ca8d1f799651e17bfa09830b8` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/COPYING.0BSD` | [xz-5.8.4/COPYING.0BSD](native/notice-files/xz-5.8.4/COPYING.0BSD) | `0b01625d853911cd0e2e088dcfb743261034a091bb379246cb25a14cc4c74bf1` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/COPYING.GPLv2` | [xz-5.8.4/COPYING.GPLv2](native/notice-files/xz-5.8.4/COPYING.GPLv2) | `edaef632cbb643e4e7a221717a6c441a4c1a7c918e6e4d56debc3d8739b233f6` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/COPYING.GPLv3` | [xz-5.8.4/COPYING.GPLv3](native/notice-files/xz-5.8.4/COPYING.GPLv3) | `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/COPYING.LGPLv2.1` | [xz-5.8.4/COPYING.LGPLv2.1](native/notice-files/xz-5.8.4/COPYING.LGPLv2.1) | `20e50fe7aae3e56378ebf0417d9de904f55a0e61e4df315333e632a4d3555d95` |
| `usr/share/scanner-kiosk/notices/native/xz-5.8.4/source.json` | [xz-5.8.4/source.json](native/notice-files/xz-5.8.4/source.json) | `33069c67c189eff290db139d55edd01f0230c99ea08f2294280855b106415d34` |
| `usr/share/scanner-kiosk/notices/native/zlib-1.3.2/LICENSE` | [zlib-1.3.2/LICENSE](native/notice-files/zlib-1.3.2/LICENSE) | `e32ff4e00d9d94930537635291da39e7e612703334bf6fde8c7f1686fe8a45a2` |
| `usr/share/scanner-kiosk/notices/native/zlib-1.3.2/source.json` | [zlib-1.3.2/source.json](native/notice-files/zlib-1.3.2/source.json) | `edd27324d774eb0685f02f80e1c1406762741969bc3ce46d687a02355c55a7ba` |
| `usr/share/scanner-kiosk/notices/native/zstd-1.5.7/COPYING` | [zstd-1.5.7/COPYING](native/notice-files/zstd-1.5.7/COPYING) | `f9c375a1be4a41f7b70301dd83c91cb89e41567478859b77eef375a52d782505` |
| `usr/share/scanner-kiosk/notices/native/zstd-1.5.7/LICENSE` | [zstd-1.5.7/LICENSE](native/notice-files/zstd-1.5.7/LICENSE) | `7055266497633c9025b777c78eb7235af13922117480ed5c674677adc381c9d8` |
| `usr/share/scanner-kiosk/notices/native/zstd-1.5.7/source.json` | [zstd-1.5.7/source.json](native/notice-files/zstd-1.5.7/source.json) | `cd1ae99019c15361e06d8d3858fc75ba74cae24454261746d280513b7575259d` |
| `usr/share/scanner-kiosk/notices/original-delivery-inventory.json` | [retained/original-delivery-inventory.json](native/notice-files/retained/original-delivery-inventory.json) | `49357229497ab31123ada3aff0c41844ade769298cd9e76bd9b44e4cca1e0132` |
| `usr/share/scanner-kiosk/notices/p11-kit-0.26.2/library-header-notices.txt` | [p11-kit-0.26.2/library-header-notices.txt](native/notice-files/p11-kit-0.26.2/library-header-notices.txt) | `378b30431117cc2a94c4bfe80664f35a6bf322e61a3a4035b1cbd2c0ae2e6ab9` |
| `usr/share/scanner-kiosk/notices/source-build/ca-certificates/APKBUILD` | [retained/source-build/ca-certificates/APKBUILD](native/notice-files/retained/source-build/ca-certificates/APKBUILD) | `7b35fb8e11790105578f47d57a3b22831227b0d8ad7d064e575bad146023f3da` |
| `usr/share/scanner-kiosk/notices/source-build/ca-certificates/MPL-2.0.txt` | [retained/source-build/ca-certificates/MPL-2.0.txt](native/notice-files/retained/source-build/ca-certificates/MPL-2.0.txt) | `fab3dd6bdab226f1c08630b1dd917e11fcb4ec5e1e020e2c16f83a0a13863e85` |
| `usr/share/scanner-kiosk/notices/source-build/ca-certificates/certdata-license-header.txt` | [retained/source-build/ca-certificates/certdata-license-header.txt](native/notice-files/retained/source-build/ca-certificates/certdata-license-header.txt) | `f63a93ed6525d62c9cb979f5dff7a9a7e71bab35cdc3ef3c435e00233e315d2c` |
| `usr/share/scanner-kiosk/notices/source-build/ca-certificates/source.json` | [retained/source-build/ca-certificates/source.json](native/notice-files/retained/source-build/ca-certificates/source.json) | `b98310fb60f1a8874c5e2674e94796afce7d9a18ed615e93ac2463b457cda61d` |
| `usr/share/scanner-kiosk/notices/source-build/sane-airscan-homelab/COPYING` | [retained/source-build/sane-airscan-homelab/COPYING](native/notice-files/retained/source-build/sane-airscan-homelab/COPYING) | `8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643` |
| `usr/share/scanner-kiosk/notices/source-build/sane-airscan-homelab/LICENSE` | [retained/source-build/sane-airscan-homelab/LICENSE](native/notice-files/retained/source-build/sane-airscan-homelab/LICENSE) | `a2631c1b81992bd6f66db6be8b009ed7e3e998d1e2db4de497fa82d65011d373` |
| `usr/share/scanner-kiosk/notices/source-build/sane-airscan-homelab/build.json` | [retained/source-build/sane-airscan-homelab/build.json](native/notice-files/retained/source-build/sane-airscan-homelab/build.json) | `7a7f480c7735fed782a4d0337ecfea6744faa5aecad97dff00b79ae4b6b6dc83` |
| `usr/share/scanner-kiosk/notices/source-build/sane-airscan-homelab/patch.diff` | [retained/source-build/sane-airscan-homelab/patch.diff](native/notice-files/retained/source-build/sane-airscan-homelab/patch.diff) | `ee7a58daeedc8d2efefae615b1eb93c295ab69a7f34c3b1a1448ca966049bb4c` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/Dockerfile.build` | [retained/source-build/tesseract-5.5.4/Dockerfile.build](native/notice-files/retained/source-build/tesseract-5.5.4/Dockerfile.build) | `a7bf7052afb843beb57135c8127170da381b329d410f224ba4ff366dc094ca55` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/Dockerfile.sdk` | [retained/source-build/tesseract-5.5.4/Dockerfile.sdk](native/notice-files/retained/source-build/tesseract-5.5.4/Dockerfile.sdk) | `36c3fd8c00771203fdad70b50a6b77d2c84d33c87b85ab605955494a8787b9a1` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/LICENSE` | [retained/source-build/tesseract-5.5.4/LICENSE](native/notice-files/retained/source-build/tesseract-5.5.4/LICENSE) | `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/build-receipt.json` | [retained/source-build/tesseract-5.5.4/build-receipt.json](native/notice-files/retained/source-build/tesseract-5.5.4/build-receipt.json) | `96ca6012ff8c0aac655552e221b5c56d0f8ca8ae0d3ca9ecbb618f6b0d186d42` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/build.sh` | [retained/source-build/tesseract-5.5.4/build.sh](native/notice-files/retained/source-build/tesseract-5.5.4/build.sh) | `0cc15604416f8f89d308c516443b0e888d69c8b28f171e7156f1e606cf2f3396` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/replacement.json` | [retained/source-build/tesseract-5.5.4/replacement.json](native/notice-files/retained/source-build/tesseract-5.5.4/replacement.json) | `d604cbb4b99eff6a57bbefcb1cfc2983a2e6771e848709e573d6f4945bf64b49` |
| `usr/share/scanner-kiosk/notices/source-build/tesseract-5.5.4/source-receipt.json` | [retained/source-build/tesseract-5.5.4/source-receipt.json](native/notice-files/retained/source-build/tesseract-5.5.4/source-receipt.json) | `7827fa1ac4d0787866c01b308d5fd418fa25b7171c9df11378bafe1f2892e74b` |

## Corresponding source and replacement

See [source distribution](docs/design/source-distribution.md) for the selected
release delivery method and remaining gates, and [personal repackaging](docs/design/native-replacement.md)
for the technical replacement path. Neither an unpublished local source archive
nor an upstream homepage is a completed recipient-facing source delivery.

The [distroless inventory and source subset](docs/design/license-distribution.md#current-distroless-only-delivery-scope-2026-11-04) identify six container-only package records. Retain its exact digest,
package notices and source obligations with the image release. The standalone
notice index alone does not close that scope.
