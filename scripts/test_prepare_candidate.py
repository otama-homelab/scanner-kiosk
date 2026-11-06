import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('prepare_candidate',Path(__file__).with_name('prepare-candidate.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class PrepareCandidateTests(unittest.TestCase):
    def test_generated_materials_reach_the_existing_sealed_stage(self):
        fixture_spec=importlib.util.spec_from_file_location('stage_fixture',Path(__file__).with_name('test_stage_candidate.py'))
        fixture_module=importlib.util.module_from_spec(fixture_spec);fixture_spec.loader.exec_module(fixture_module)
        fixture=fixture_module.StageCandidateTest();fixture.setUp();self.addCleanup(fixture.doCleanups)
        inputs=fixture.root/'generated-inputs';candidate=inputs/'candidate';native=inputs/'native'
        candidate.mkdir(parents=True);native.mkdir()
        paths={'binary':'packaged/scanner-onefile-linux-amd64','image':'image.tar','build':'packaged/build.json',
            'packed-manifest':'packaged/manifest.json','application':'application.json','image-config':'image.json',
            'onefile-sbom':'onefile-sbom/onefile.syft.json','onefile-inventory':'onefile-sbom/receipt.json',
            'native-sbom':'native-sbom/native-origin.cdx.json','image-sbom':'image-sbom/image.syft.json',
            'image-inventory':'image-sbom/receipt.json','base-source':'base-sources.tar','base-inventory':'base-inventory.json'}
        paths.update(module.stage.RUNTIME_FILES)
        for role,name in paths.items():
            path=candidate/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes((fixture.inputs/role).read_bytes())
        (candidate/'application-sources.tar').write_bytes(b'application sources')
        source={'path':'upstream/apk/fixture.tar.gz','bytes':4,'sha256':hashlib.sha256(b'data').hexdigest()}
        lock={'files':[source]};tesseract={'bytes':6,'sha256':hashlib.sha256(b'source').hexdigest()}
        controls={'LICENSE':'license','THIRD-PARTY-NOTICES.md':'notices','CHANGELOG.md':'changelog',
                  'native/runtime-manifest.json':'native-manifest','native/runtime-provenance.json':'native-provenance'}
        with tarfile.open(native/'source.tar','w') as archive:
            for name,payload in [('Dockerfile',b'core recipe'),('Dockerfile.tesseract',b'tesseract recipe'),('.dockerignore',b'ignore')]:
                module.tar_bytes(archive,'source/'+name,payload)
            for name,role in controls.items():module.tar_bytes(archive,'source/'+name,(fixture.inputs/role).read_bytes())
            module.tar_bytes(archive,'source/native/source-inputs.lock.json',json.dumps(lock).encode())
            module.tar_bytes(archive,'source/native/tesseract-source.lock.json',json.dumps(tesseract).encode())
            module.tar_bytes(archive,'source/deploy/compose.yaml',b'fixture deployment')
        (candidate/'native-build.json').write_text(json.dumps({'passed':True,'source_revision':fixture.source,
            'source_archive_sha256':module.record(native/'source.tar')['sha256']}))
        with tarfile.open(native/'external-sources.tar','w') as archive:
            module.tar_bytes(archive,'source-inputs.lock.json',json.dumps(lock).encode())
            module.tar_bytes(archive,source['path'],b'data')
        (native/'tesseract-source.tar.gz').write_bytes(b'source')
        reports=inputs/'security/reports';reports.mkdir(parents=True)
        security=json.loads((fixture.inputs/'security').read_text());security['scans_completed']=True
        for scope,scan in security['scans'].items():
            scan['artifacts_sha256']['scan.log']=hashlib.sha256(b'').hexdigest()
            (reports/scope).mkdir()
            (reports/scope/'scan.log').write_bytes(b'')
        (reports/'receipt.json').write_text(json.dumps(security))
        with tarfile.open(fixture.inputs/'security-reports') as archive:
            for member in archive:
                path=reports/member.name;path.parent.mkdir(exist_ok=True);path.write_bytes(archive.extractfile(member).read())
        output=fixture.root/'assembled'
        result=module.prepare(inputs,output)
        manifest=module.stage.verify_staged(output/'candidate')
        self.assertTrue(result['passed']);self.assertFalse(manifest['publication_approved'])
        self.assertEqual(manifest['source_sha'],fixture.source)
        (candidate/'base-inventory.json').unlink()
        with self.assertRaises(FileNotFoundError):module.prepare(inputs,fixture.root/'incomplete')
        self.assertFalse((fixture.root/'incomplete/candidate').exists())

    def test_empty_assets_are_rejected_unless_explicitly_allowed_for_logs(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'empty';path.write_bytes(b'')
            with self.assertRaises(ValueError):module.record(path)
            self.assertEqual(module.record(path,allow_empty=True)['bytes'],0)

    def test_source_archive_revalidates_inputs_and_preserves_project_correspondence(self):
        for changed in [False,True]:
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);project=root/'project.tar';project.write_bytes(b'project')
                tesseract=root/'tesseract.tar.gz';tesseract.write_bytes(b'source')
                source={'path':'upstream/apk/fixture.tar.gz','bytes':4,'sha256':hashlib.sha256(b'data').hexdigest()}
                lock={'files':[source]}
                with tarfile.open(root/'external.tar','w') as archive:
                    module.tar_bytes(archive,'source-inputs.lock.json',json.dumps(lock).encode())
                    module.tar_bytes(archive,source['path'],b'evil' if changed else b'data')
                index={};args=[root/'external.tar',project,tesseract,lock,
                    {'bytes':6,'sha256':hashlib.sha256(b'source').hexdigest()},index,root/'native.tar']
                if changed:
                    with self.assertRaises(ValueError):module.native_sources(*args)
                else:
                    module.native_sources(*args)
                    self.assertIn('build-controls/scanner-source.tar',[r['archive'] for r in index['files']])
                    with tarfile.open(root/'native.tar') as archive:
                        self.assertEqual(archive.extractfile(source['path']).read(),b'data')
                        self.assertEqual(archive.extractfile('build-controls/scanner-source.tar').read(),b'project')


if __name__=='__main__':unittest.main()
