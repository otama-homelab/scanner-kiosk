#!/bin/sh
set -eu
umask 077

root=${1:?usage: build_candidates.sh PRIVATE_ROOT}
mkdir -p "$root/bin" "$root/build" "$root/src" "$root/install"

archive="$root/leptonica-1.87.0.tar.gz"
if [ ! -f "$archive" ]; then
  curl -fsSL https://github.com/DanBloomberg/leptonica/archive/refs/tags/1.87.0.tar.gz -o "$archive"
fi
echo "fa2b40c5caea96d1bb93a97486262aed8731b69ce25a84a6bf5d25323e33f631  $archive" | sha256sum -c -

if [ ! -f "$root/install/lib/libleptonica.a" ]; then
  tar -xzf "$archive" -C "$root/src" --strip-components=1
  cmake -S "$root/src" -B "$root/build/leptonica" \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_PROG=OFF -DSW_BUILD=OFF \
    -DENABLE_TIFF=OFF -DENABLE_GIF=OFF -DENABLE_WEBP=OFF -DENABLE_OPENJPEG=OFF \
    -DENABLE_ZLIB=ON -DENABLE_PNG=ON -DENABLE_JPEG=ON \
    -DCMAKE_INSTALL_PREFIX="$root/install"
  cmake --build "$root/build/leptonica" --parallel 2
  cmake --install "$root/build/leptonica"
fi

cc -O2 -std=c11 -I"$root/install/include" \
  scripts/eval/correction/leptonica_candidate.c \
  "$root/install/lib/libleptonica.a" -lpng -ljpeg -lz -lm \
  -o "$root/bin/leptonica_candidate"

g++ -O2 -std=c++17 scripts/eval/correction/opencv_candidate.cpp \
  $(pkg-config --cflags --libs opencv4) -o "$root/bin/opencv_candidate"

chmod 700 "$root/bin/leptonica_candidate" "$root/bin/opencv_candidate"
sha256sum "$root/bin/leptonica_candidate" "$root/bin/opencv_candidate"
