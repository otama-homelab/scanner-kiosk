import hashlib
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class SANEHelperBuildTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.base = Path(temp.name)
        self.work, self.source, self.tools = [self.base/name for name in ['work', 'input', 'tools']]
        for path in [self.work, self.source, self.tools]:
            path.mkdir()
        (self.source/'main.go').write_bytes(b'package main\n')
        (self.source/'unlisted.go').write_bytes(b'must not enter compiler input')
        self.checksums = self.base/'source.SHA256SUMS'
        self.checksums.write_text(hashlib.sha256((self.source/'main.go').read_bytes()).hexdigest()+'  main.go\n')
        self.command('cp', 'if [ "$1" = /sdk-packages.txt ]; then printf fixture-sdk > "$2/sdk-packages.txt"; exit 0; fi\nexec /usr/bin/cp "$@"')
        self.command('go', '''test "$CGO_ENABLED" = 1
test "$GOTOOLCHAIN" = local
test "$GOENV" = off
test "$GOWORK" = off
test "$GOPROXY" = off
test "$GOSUMDB" = off
test -z "$GOFLAGS"
touch ../go-called
if [ "$1" = build ]; then
 while [ "$1" != -o ]; do shift; done
 printf fixture-helper > "$2"
else
 printf fixture-buildinfo
fi''')

    def command(self, name, body):
        path = self.tools/name
        path.write_text('#!/bin/sh\nset -eu\n'+body+'\n')
        path.chmod(0o755)

    def run_build(self, *arguments):
        return subprocess.run(['/bin/sh', str(ROOT/'native/build-sane-parameters.sh'),
                               *(arguments or [str(self.source), str(self.checksums)])],
                              cwd=self.work, env={'PATH':str(self.tools)+':/usr/bin:/bin',
                              'HOME':str(self.work), 'TMPDIR':str(self.work),
                              'GOTOOLCHAIN':'auto', 'CGO_ENABLED':'0',
                              'GOPROXY':'https://unused.invalid', 'GOFLAGS':'-invalid'},
                              capture_output=True, text=True, timeout=30)

    def test_success_selects_only_pinned_sources_and_exports_runtime_layout(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.work/'source/unlisted.go').exists())
        self.assertEqual((self.work/'source/main.go').read_bytes(), b'package main\n')
        self.assertIn('RESULT_READY', result.stdout)
        with tarfile.open(self.work/'native.tar') as archive:
            self.assertEqual({m.name for m in archive.getmembers() if not m.isdir()},
                             {'./native/usr/libexec/scanner/sane-parameters',
                              './build.log', './buildinfo.txt', './sdk-packages.txt'})

    def test_changed_source_stops_before_compiler(self):
        (self.source/'main.go').write_bytes(b'changed')
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'go-called').exists())
        self.assertNotIn('RESULT_READY', result.stdout)

    def test_failed_compiler_cannot_export_success(self):
        self.command('go', 'exit 23')
        result = self.run_build()
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertFalse((self.work/'native.tar').exists())
        self.assertNotIn('RESULT_READY', result.stdout)

    def test_escaping_source_selection_is_rejected(self):
        self.checksums.write_text('a'*64+'  ../outside.go\n')
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'outside.go').exists())
        self.assertFalse((self.work/'go-called').exists())

    def test_missing_input_argument_reports_usage(self):
        result = self.run_build('only-one-argument')
        self.assertEqual(result.returncode, 2)
        self.assertIn('usage:', result.stderr)

    def test_checksum_table_matches_firstparty_source_inventory(self):
        provenance = json.loads((ROOT/'native/runtime-provenance.json').read_text())
        expected = next(c['source_sha256'] for c in provenance['firstparty_components'] if c['name']=='sane-parameters')
        lines = (ROOT/'native/helper-source.SHA256SUMS').read_text().splitlines()
        actual = {name:digest for line in lines for digest,name in [line.split('  ')]}
        self.assertEqual(len(actual), len(lines))
        self.assertEqual(actual, expected)


if __name__ == '__main__':
    unittest.main()
