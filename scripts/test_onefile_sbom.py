import hashlib
import importlib.util
import json
import os
import subprocess
from pathlib import Path
import unittest
from unittest.mock import patch
import test_assemble_native as fixtures


def load():
    spec = importlib.util.spec_from_file_location('onefile_sbom', Path(__file__).with_name('onefile-sbom.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class OnefileSBOMTest(unittest.TestCase):
    def setUp(self):
        self.f = fixtures.AssembleNativeTest()
        self.f.setUp()
        self.addCleanup(self.f.doCleanups)
        f = self.f
        f.provenance['apk_source_components']['fixture'].update(
            version='1.0-r0', purl='pkg:apk/alpine/fixture@1.0-r0')
        f.provenance['firstparty_components'][0]['name'] = 'helper'
        f.assemble()
        self.packed = f.work/'packed.json'
        app = {'size': 3, 'mode': 0o555, 'sha256': hashlib.sha256(b'app').hexdigest()}
        files = {**f.files, 'app/scanner': app}
        self.packed.write_text(json.dumps({'files': files, 'payload_sha256': '0'*64}))
        self.binary = f.work/'scanner'
        data = {n: ('app' if n == 'app/scanner' else (f.output/n).read_text())
                for n, e in files.items() if not e.get('link')}
        links = {n: e['link'] for n, e in files.items() if e.get('link')}
        code = ('#!/usr/bin/python3\nfrom pathlib import Path\nimport os,json\n'
                'assert "UNRELATED_SECRET" not in os.environ\n'
                "root=Path(os.environ['XDG_CACHE_HOME'])/'scanner-kiosk/native/fixture';root.mkdir(parents=True)\n"
                f'files=json.loads({json.dumps(json.dumps(files))})\n'
                f'data=json.loads({json.dumps(json.dumps(data))})\n'
                'for name,text in data.items():\n p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text);p.chmod(files[name]["mode"])\n'
                f'links=json.loads({json.dumps(json.dumps(links))})\n'
                'for name,target in links.items():(root/name).symlink_to(target)\n'
                "(Path(os.environ['HOME'])/'child-env.json').write_text(json.dumps(dict(os.environ)))\n"
                'print(json.dumps({"version":"fixture","revision":"fixture"}))\n')
        self.binary.write_text(code)
        self.binary.chmod(0o755)
        self.build = f.work/'build.json'
        self.build.write_text(json.dumps({'binary_sha256': hashlib.sha256(self.binary.read_bytes()).hexdigest(),
                                        'binary_bytes': self.binary.stat().st_size,
                                        'app_sha256': app['sha256'], 'native_entries': len(f.files),
                                        'native_manifest_sha256': f.provenance['runtime_manifest_sha256']}))
        self.syft = f.work/'syft'
        self.syft.write_text('''#!/usr/bin/python3
import json,sys
from pathlib import Path
if sys.argv[1]=='version': print(json.dumps({'application':'syft','version':'fixture'}))
else:
 for argument in sys.argv:
  if argument.startswith('syft-json='):Path(argument.split('=',1)[1]).write_text(json.dumps({'artifacts':[{'type':'go-module','name':'fixture','version':'v1'}]}))
  if argument.startswith('cyclonedx-json='):Path(argument.split('=',1)[1]).write_text(json.dumps({'bomFormat':'CycloneDX','components':[{'type':'library','name':'fixture'}]}))
''')
        self.syft.chmod(0o755)
        self.output = f.work/'inventory'

    def run_catalog(self):
        return load().catalog(self.binary, self.build, self.packed, self.f.manifest,
                              self.f.work/'provenance.json', self.syft, self.output)

    def test_binds_actual_files_and_reuses_native_catalog(self):
        with patch.dict(os.environ, {'UNRELATED_SECRET': 'dummy-must-not-forward'}):
            receipt = self.run_catalog()
        self.assertEqual(receipt['binary_sha256'], hashlib.sha256(self.binary.read_bytes()).hexdigest())
        self.assertEqual(receipt['native']['native_entries'], 5)
        self.assertEqual(receipt['go_package_count'], 1)
        self.assertFalse(receipt['publication_approved'])
        self.assertFalse(receipt['container_base_included'])
        self.assertIn('native/native-origin.cdx.json', receipt['artifacts_sha256'])
        self.assertTrue((self.output/'onefile.syft.json').is_file())
        self.assertFalse(any(p.name == 'child-env.json' for p in self.output.rglob('*')))
        self.assertFalse(any(p.name.startswith('scanner-onefile-sbom-') for p in self.f.work.iterdir()))

    def test_wrong_binary_or_build_binding_rejected_before_execution(self):
        self.binary.write_text(self.binary.read_text()+'# changed\n')
        with self.assertRaises(ValueError): self.run_catalog()
        self.assertFalse(self.output.exists())
        self.assertFalse(list(self.f.work.glob('scanner-onefile-sbom-*')))

    def test_wrong_native_or_packed_manifest_rejected(self):
        packed = json.loads(self.packed.read_text())
        packed['files']['../unregistered'] = packed['files']['app/scanner']
        self.packed.write_text(json.dumps(packed))
        with self.assertRaises(ValueError): self.run_catalog()
        self.assertFalse(self.output.exists())

    def test_existing_output_never_overwritten(self):
        self.output.mkdir()
        sentinel = self.output/'keep'
        sentinel.write_text('keep')
        with self.assertRaises(FileExistsError): self.run_catalog()
        self.assertEqual(sentinel.read_text(), 'keep')

    def test_output_created_during_scan_is_not_removed(self):
        self.syft.write_text(self.syft.read_text() +
            "\nif sys.argv[1]!='version':\n p=Path(" + repr(str(self.output)) +
            ");p.mkdir();(p/'receipt.json').write_text('other writer')\n")
        self.syft.chmod(0o755)
        with self.assertRaises(FileExistsError): self.run_catalog()
        self.assertEqual((self.output/'receipt.json').read_text(), 'other writer')

    def test_partial_output_copy_failure_is_removed(self):
        module = load()
        def fail_copy(source, target, **kwargs):
            Path(target).mkdir(exist_ok=True)
            (Path(target)/'partial').write_text('partial')
            raise OSError('fixture copy failure')
        with patch.object(module.shutil, 'copytree', side_effect=fail_copy):
            with self.assertRaises(OSError):
                module.catalog(self.binary, self.build, self.packed, self.f.manifest,
                               self.f.work/'provenance.json', self.syft, self.output)
        self.assertFalse(self.output.exists())

    def test_syft_failure_leaves_no_output_or_cache(self):
        self.syft.write_text('#!/usr/bin/python3\nimport sys\nsys.exit(2)\n')
        self.syft.chmod(0o755)
        with self.assertRaises(subprocess.CalledProcessError): self.run_catalog()
        self.assertFalse(self.output.exists())
        self.assertFalse(list(self.f.work.glob('scanner-onefile-sbom-*')))


if __name__ == '__main__':
    unittest.main()
