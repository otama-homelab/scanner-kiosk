#!/bin/sh
set -eu
umask 077
cd /work
mkdir source build home tmp output
echo '445603ab1d0a63df552e193a600d9d5c4021aff36e144f3091af149e5efb381a  /inputs/source.tar.gz' | sha256sum -c -
tar -xzf /inputs/source.tar.gz -C source --strip-components=1
echo PHASE_CONFIGURE
cmake -S source -B build -DCMAKE_SKIP_RPATH=ON -DCMAKE_BUILD_TYPE=Release -DENABLE_NATIVE=OFF -DOPENMP_BUILD=ON -DBUILD_SHARED_LIBS=ON -DBUILD_TRAINING_TOOLS=ON -DBUILD_TESTS=ON -DUSE_SYSTEM_GTEST=ON -DCMAKE_INSTALL_PREFIX=/usr > output/configure.log 2>&1
echo PHASE_COMPILE
cmake --build build --parallel 2 --target tesseract bitvector_test indexmapbidi_test > output/build.log 2>&1
echo PHASE_TEST
test_status=0
(cd build && LD_LIBRARY_PATH=/work/build:/work/build/src/training ctest --output-on-failure -R '^(bitvector_test|indexmapbidi_test)$') > output/tests.log 2>&1 || test_status=$?
echo "TEST_EXIT=$test_status" >> output/tests.log
LD_LIBRARY_PATH=/work/build build/bin/tesseract --version > output/version.log 2>&1
cp build/bin/tesseract output/
cp -P build/libtesseract.so* output/
cp build/CMakeCache.txt output/
cp /sdk-packages.txt output/
tar -cf native.tar -C output .
sha256sum native.tar
echo RESULT_READY
exec sleep 180
