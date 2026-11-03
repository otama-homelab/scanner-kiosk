import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('build_standalone', Path(__file__).with_name('build-standalone.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def app_info():
    return {'Path': 'scanner-kiosk/cmd/scanner', 'GoVersion': 'go1.27.3',
            'Settings': [{'Key': key, 'Value': value} for key, value in
                         [('GOOS', 'linux'), ('GOARCH', 'amd64'), ('CGO_ENABLED', '0'), ('-tags', 'webui')]]}


class BuildStandaloneTests(unittest.TestCase):
    def test_app_contract(self):
        module.validate_app(app_info())
        for key, value in [('GOOS', 'windows'), ('GOARCH', 'arm64'), ('CGO_ENABLED', '1'), ('-tags', '')]:
            info = app_info()
            for item in info['Settings']:
                if item['Key'] == key:
                    item['Value'] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                module.validate_app(info)
        info = app_info()
        info['Path'] = 'another/app'
        with self.assertRaises(ValueError):
            module.validate_app(info)

    def test_existing_output_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'output'
            output.mkdir()
            (output / 'keep').write_text('owned by caller')
            with patch.object(module.subprocess, 'run') as run:
                with self.assertRaises(FileExistsError):
                    module.build(Path('missing'), Path('missing'), Path('missing'), output)
                run.assert_not_called()
            self.assertEqual((output / 'keep').read_text(), 'owned by caller')

    def test_symlink_app_rejected_before_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'app').write_bytes(b'fixture')
            (root / 'link').symlink_to('app')
            with patch.object(module.subprocess, 'run') as run:
                with self.assertRaises(ValueError):
                    module.build(root / 'link', root, root / 'missing', root / 'output')
                run.assert_not_called()

    def test_changed_native_blocks_compilation_and_publication(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app = root / 'app'
            app.write_bytes(b'synthetic app; not native acceptance')
            native = root / 'native'
            for name in ['lib/ld-musl-x86_64.so.1', 'usr/bin/scanimage', 'usr/bin/tesseract',
                         'usr/libexec/scanner/sane-parameters', 'usr/share/tessdata/eng.traineddata',
                         'usr/share/tessdata/jpn.traineddata', 'usr/share/tessdata/osd.traineddata',
                         'usr/share/scanner-kiosk/ca-certificates.crt']:
                path = native / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'synthetic native fixture')
            with contextlib.redirect_stdout(io.StringIO()):
                module.packager.pack(app, native, root / 'pinned')
            (native / 'usr/share/tessdata/jpn.traineddata').write_bytes(b'changed')
            with patch.object(module.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps(app_info()))) as run:
                with contextlib.redirect_stdout(io.StringIO()), self.assertRaisesRegex(ValueError, 'pinned manifest'):
                    module.build(app, native, root / 'pinned/manifest.json', root / 'output')
                self.assertEqual(run.call_count, 1)
                self.assertEqual(run.call_args.args[0][:4], ['go', 'version', '-m', '-json'])
            self.assertFalse((root / 'output').exists())


if __name__ == '__main__':
    unittest.main()
