import hashlib
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class TesseractBuildTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.work = Path(temp.name)
        self.tools = self.work/'tools'
        self.tools.mkdir()
        self.command('sha256sum', 'exit 0')
        self.command('tar', '[ "$1" != -xzf ] || exit 0\nexec /usr/bin/tar "$@"')
        self.command('ctest', 'echo fixture-tests-passed\nexit 0')
        self.command('cp', 'if [ "$1" = /sdk-packages.txt ]; then printf fixture-sdk > "$2/sdk-packages.txt"; exit 0; fi\nexec /usr/bin/cp "$@"')
        self.command('cmake', '''mkdir -p build/bin
printf fixture-cache > build/CMakeCache.txt
if [ "$1" = --build ]; then
 printf '#!/bin/sh\nprintf "tesseract 5.5.4\\n"\n' > build/bin/tesseract
 chmod 755 build/bin/tesseract
 printf fixture-library > build/libtesseract.so.5.5.4
 ln -sf libtesseract.so.5.5.4 build/libtesseract.so.5.5
 ln -sf libtesseract.so.5.5 build/libtesseract.so
fi''')

    def command(self, name, body):
        path = self.tools/name
        path.write_text('#!/bin/sh\nset -eu\n'+body+'\n')
        path.chmod(0o755)

    def run_build(self):
        return subprocess.run(['/bin/sh', str(ROOT/'native/build-tesseract.sh')],
                              cwd=self.work, env={'PATH': str(self.tools)+':/usr/bin:/bin',
                              'HOME': str(self.work), 'TMPDIR': str(self.work)},
                              capture_output=True, text=True, timeout=30)

    def test_source_hash_failure_stops_before_build(self):
        self.command('sha256sum', 'exit 17')
        result = self.run_build()
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertFalse((self.work/'build/bin/tesseract').exists())
        self.assertNotIn('RESULT_READY', result.stdout)

    def test_upstream_test_failure_is_fatal(self):
        self.command('ctest', 'echo fixture-test-failure\nexit 23')
        result = self.run_build()
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertIn('fixture-test-failure', (self.work/'output/tests.log').read_text())
        self.assertFalse((self.work/'native.tar').exists())
        self.assertNotIn('RESULT_READY', result.stdout)

    def test_success_exports_assembly_layout_without_source_or_tools(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('RESULT_READY', result.stdout)
        with tarfile.open(self.work/'native.tar') as archive:
            files = {m.name for m in archive.getmembers() if not m.isdir()}
            self.assertEqual(files, {'./native/usr/bin/tesseract',
                './native/usr/lib/libtesseract.so', './native/usr/lib/libtesseract.so.5.5',
                './native/usr/lib/libtesseract.so.5.5.4', './configure.log', './build.log',
                './tests.log', './version.log', './CMakeCache.txt', './sdk-packages.txt'})
            self.assertEqual(archive.getmember('./native/usr/lib/libtesseract.so').linkname,
                             'libtesseract.so.5.5')

    def test_source_lock_matches_runtime_origin_and_sdk(self):
        lock = json.loads((ROOT/'native/tesseract-source.lock.json').read_text())
        origins = json.loads((ROOT/'native/runtime-provenance.json').read_text())
        origin = next(p for p in origins['upstream_source_components'] if p['name']=='tesseract')
        self.assertEqual(lock['version'], origin['version'])
        self.assertEqual(lock['commit'], origin['commit'])
        self.assertEqual(lock['url'], 'https://codeload.github.com/tesseract-ocr/tesseract/tar.gz/'+lock['commit'])
        self.assertEqual(lock['sdk_lock_sha256'], hashlib.sha256((ROOT/'native/tesseract-sdk.lock.json').read_bytes()).hexdigest())
        self.assertEqual((ROOT/'native/tesseract-source.SHA256SUMS').read_text(),
                         lock['sha256']+'  /inputs/source.tar.gz\n')


if __name__ == '__main__':
    unittest.main()
