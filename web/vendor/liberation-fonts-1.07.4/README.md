# Liberation Sans corresponding source

PDF.js 6.5.438 contains four unmodified Liberation Sans 1.07.4 fonts. These are GPL v2 with the Liberation font exception, rather than the SIL-licensed Liberation 2.x fonts. The exception does not replace the obligation to provide the font source.

`source.tar.gz` is the unmodified upstream 1.07.4 release, recovered from the Cygwin source package identified in SOURCE.json. It includes the preferred SFD files, Makefile, FontForge export scripts, AUTHORS, License.txt and COPYING. The four runtime font SHA256 hashes and their SFD paths are recorded in SOURCE.json; the build verifies both the source archive and the npm font bytes. The complete project source distribution retains this directory.

To modify/build the fonts, extract source.tar.gz into a private directory, install FontForge and GNU Make, and follow the upstream README/Makefile (`make`, or `make ttf`). The upstream source contains other font families as well. No fonts or glyph ordering are modified here; replacing the runtime fonts also requires following PDF.js's standard-font width/scale-table instructions. A byte-identical font rebuild is not claimed.

This font's licenses apply to the font and its source, not the project's separate Apache-2.0 source. The unchanged font software and both license texts remain available for modification and rebuilding.
