#!/bin/sh
set -eu
umask 077
# The pinned input image runs this in /work to preserve compiler path identity.
mkdir source build home tmp output
sha256sum -c /inputs/tesseract-source.SHA256SUMS
tar -xzf /inputs/source.tar.gz -C source --strip-components=1
echo PHASE_CONFIGURE
cmake -S source -B build -DCMAKE_SKIP_RPATH=ON -DCMAKE_BUILD_TYPE=Release -DENABLE_NATIVE=OFF -DOPENMP_BUILD=ON -DBUILD_SHARED_LIBS=ON -DBUILD_TRAINING_TOOLS=ON -DBUILD_TESTS=ON -DUSE_SYSTEM_GTEST=ON -DCMAKE_INSTALL_PREFIX=/usr > output/configure.log 2>&1
echo PHASE_COMPILE
cmake --build build --parallel 2 --target tesseract bitvector_test indexmapbidi_test > output/build.log 2>&1
echo PHASE_TEST
(cd build && LD_LIBRARY_PATH=/work/build:/work/build/src/training ctest --output-on-failure -R '^(bitvector_test|indexmapbidi_test)$') > output/tests.log 2>&1
echo TEST_EXIT=0 >> output/tests.log
LD_LIBRARY_PATH=/work/build build/bin/tesseract --version > output/version.log 2>&1
mkdir -p output/native/usr/bin output/native/usr/lib
cp build/bin/tesseract output/native/usr/bin/
cp -P build/libtesseract.so* output/native/usr/lib/
cp build/CMakeCache.txt output/
cp /sdk-packages.txt output/
tar -cf native.tar -C output .
sha256sum native.tar
echo RESULT_READY
