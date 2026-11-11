import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import tarfile
import unittest


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


class StageCandidateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.inputs = self.root/'inputs'
        self.inputs.mkdir()
        self.output = self.root/'staged'
        self.source = 'a'*40
        self.version = {'version': '0.0.0-dev', 'revision': 'unreleased',
                        'os': 'linux', 'architecture': 'amd64', 'go_version': 'go1.27.4'}
        self.files = {}
        for name in ['binary', 'image', 'source', 'native-source', 'base-source',
                     'license', 'notices', 'deployment', 'changelog', 'security-reports']:
            self.put(name, name.encode())
        native = {'files': {'native/file': {'sha256': digest(b'native'), 'size': 6, 'mode': 365}}}
        self.put('native-manifest', native)
        self.put('native-provenance', {'runtime_manifest_sha256': self.files['native-manifest']['sha256']})
        self.put('build', {'binary_sha256': self.files['binary']['sha256'], 'binary_bytes': 6,
                          'app_sha256': digest(b'api'), 'native_entries': 1,
                          'project_license_sha256': self.files['license']['sha256'],
                          'native_manifest_sha256': self.files['native-manifest']['sha256']})
        self.put('packed-manifest', {'files': {**native['files'], 'app/scanner': {'sha256': digest(b'api'), 'size': 3, 'mode': 365}}})
        self.put('application', {'passed': True, 'source_revision': self.source, 'version': self.version,
                                 'assets': {'index.html': digest(b'html')}})
        self.image = 'sha256:'+'b'*64
        self.put('image-config', [{'Id': self.image, 'Config': {'Entrypoint': ['/usr/local/bin/scanner'],
                                  'Cmd': ['serve'], 'User': '10001:10001'}}])
        self.put('onefile-sbom', {'artifacts': [{'name': 'fixture', 'type': 'go-module'}]})
        self.put('native-sbom', {'bomFormat': 'CycloneDX', 'components': [{'name': 'native'}]})
        self.put('image-sbom', {'artifacts': [{'name': 'base', 'type': 'deb'}],
                                'source': {'type': 'image', 'metadata': {'imageID': self.image}}})
        self.put('onefile-inventory', {'binary_sha256': self.files['binary']['sha256'], 'binary_bytes': 6,
            'build_receipt_sha256': self.files['build']['sha256'], 'packed_manifest_sha256': self.files['packed-manifest']['sha256'],
            'version': self.version, 'native': {'native_manifest_sha256': self.files['native-manifest']['sha256'],
            'provenance_sha256': self.files['native-provenance']['sha256']},
            'artifacts_sha256': {'onefile.syft.json': self.files['onefile-sbom']['sha256'],
                                 'native/native-origin.cdx.json': self.files['native-sbom']['sha256']}})
        self.put('image-inventory', {'passed': True, 'image_id': self.image, 'all_layer_diff_ids_verified': True,
                                     'image_archive_sha256': self.files['image']['sha256'],
                                     'artifacts_sha256':{'image.syft.json':self.files['image-sbom']['sha256']}})
        scans = {key: {'scan_completed': True, 'exit_code': 0, 'input_sha256': self.files[role]['sha256']}
                 for key, role in [('image', 'image-sbom'), ('onefile', 'onefile-sbom'),
                                   ('native', 'native-sbom'), ('native-cpe', 'native-sbom')]}
        self.put('security', {'scans': scans})
        self.put('source-bindings', {'application_source_commit': self.source, 'candidate_sha256': self.files['binary']['sha256'],
            'api_sha256': digest(b'api'), 'native_manifest_sha256': self.files['native-manifest']['sha256'],
            'native_provenance_sha256': self.files['native-provenance']['sha256'], 'version': self.version,
            'image_id': self.image, 'assets': {'index.html': digest(b'html')},
            'archives': {k: {'sha256': self.files[role]['sha256'], 'bytes': self.files[role]['bytes']}
                         for k, role in [('native', 'native-source'), ('base', 'base-source')]}})
        self.put('native-source-index', {'application_source_commit': self.source,
            'candidate_sha256': self.files['binary']['sha256'],
            'native_manifest_sha256': self.files['native-manifest']['sha256'],
            'files': [{'archive': 'build-controls/scanner-source.tar', 'sha256': self.files['source']['sha256'],
                       'bytes': self.files['source']['bytes']}]})
        self.put('base-inventory', {'passed': True, 'image_id': self.image, 'archive_sha256': self.files['base-source']['sha256']})
        self.put('deployment-inventory', {'passed': True, 'source_archive_sha256': self.files['source']['sha256'],
                                         'archive_sha256': self.files['deployment']['sha256']})
        self.runtime_materials()
        scans['tesseract-image']={'scan_completed':True,'exit_code':0,
                                  'input_sha256':self.files['tesseract-image-sbom']['sha256']}
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode='w') as archive:
            for key in scans:
                payload = b'{}'
                member = tarfile.TarInfo(key+'/grype.json')
                member.size = len(payload)
                archive.addfile(member, io.BytesIO(payload))
                scans[key]['artifacts_sha256'] = {'grype.json': digest(payload)}
        self.put('security-reports', raw.getvalue())
        self.put('security', {'scans': scans})

    def runtime_materials(self):
        parent=self.files['native-manifest']['sha256'];roles={};images={}
        recipes={'core':digest(b'core recipe'),'tesseract':digest(b'tesseract recipe')}
        for role,prefix,binary,image_id in [('core','image','scanner',self.image),
                ('tesseract','tesseract-image','scanner-tesseract-api','sha256:'+'c'*64)]:
            if role=='tesseract':
                self.put(prefix,b'tesseract image')
                self.put(prefix+'-config',[{'Id':image_id,'Config':{'Entrypoint':['/usr/local/bin/'+binary],
                         'Cmd':['serve'],'User':'10001:10001'}}])
                self.put(prefix+'-inventory',{'passed':True,'all_layer_diff_ids_verified':True,
                    'image_id':image_id,'image_archive_sha256':self.files[prefix]['sha256']})
                self.put(prefix+'-sbom',{'artifacts':[{'name':'service','type':'go-module'}],
                    'source':{'type':'image','metadata':{'imageID':image_id}}})
                saved=json.loads((self.inputs/(prefix+'-inventory')).read_text())
                saved['artifacts_sha256']={'image.syft.json':self.files[prefix+'-sbom']['sha256']}
                self.put(prefix+'-inventory',saved)
            native=json.loads((self.inputs/'native-manifest').read_text())
            self.put(role+'-native-manifest',native)
            manifest=self.files[role+'-native-manifest']['sha256']
            self.put(role+'-native-provenance',{'runtime_manifest_sha256':manifest,
                        'partition':{'role':role,'parent_manifest_sha256':parent}})
            provenance=self.files[role+'-native-provenance']['sha256']
            self.put(role+'-native-inventory',{'native_manifest_sha256':manifest,
                    'provenance_sha256':provenance,'native_entries':len(native['files'])})
            self.put(role+'-native-sbom',{'bomFormat':'CycloneDX','components':[{'name':'native'}],
                'metadata':{'properties':[{'name':'scanner:native_manifest_sha256','value':manifest},
                    {'name':'scanner:provenance_sha256','value':provenance}]}})
            binary_sha=digest(b'api' if role=='core' else b'service')
            roles[role]={'binary':{'path':binary,'sha256':binary_sha,'bytes':3 if role=='core' else 7},
                'native_manifest_sha256':manifest,'native_provenance_sha256':provenance,
                'license_sha256':self.files['license']['sha256'],'dockerfile_sha256':recipes[role],
                'dockerignore_sha256':digest(b'ignore'),'native_entries':len(native['files']),
                'native_bytes':sum(f['size'] for f in native['files'].values())}
            self.put(prefix+'-version',{**self.version,**({'native_runtime_sha256':manifest} if role=='tesseract' else {})})
            self.put(prefix+'-container',[{'Id':('d' if role=='core' else 'e')*64,'Image':image_id,
                'State':{'Running':False,'OOMKilled':False,'ExitCode':0}}])
            self.put(prefix+'-payload',{'passed':True,'image_id':image_id,'role':role,
                'container_id':('d' if role=='core' else 'e')*64,'native_manifest_sha256':manifest,
                'binary_sha256':binary_sha,'native_paths':len(native['files'])})
            images[role]={'image_id':image_id,'binary_sha256':binary_sha,'native_manifest_sha256':manifest,
                          'native_provenance_sha256':provenance}
        self.put('runtime-inputs',{'source_revision':self.source,'parent_manifest_sha256':parent,'roles':roles})
        app=json.loads((self.inputs/'application').read_text());app['runtime_inputs_sha256']=self.files['runtime-inputs']['sha256'];self.put('application',app)
        sources=json.loads((self.inputs/'source-bindings').read_text())
        sources.update(runtime_images=images,runtime_recipes=recipes,dockerignore_sha256=digest(b'ignore'))
        self.put('source-bindings',sources)

    def put(self, role, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value)+'\n').encode()
        (self.inputs/role).write_bytes(raw)
        self.files[role] = {'path': role, 'bytes': len(raw), 'sha256': digest(raw)}

    def run_stage(self):
        spec = importlib.util.spec_from_file_location('stage_candidate', Path(__file__).with_name('stage-candidate.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        inventory = self.root/'inventory.json'
        inventory.write_text(json.dumps({'format': 1, 'source_sha': self.source, 'files': self.files}))
        self.module = module
        return module.stage(self.inputs, inventory, self.output)

    def test_read_only_verification_reuses_candidate_bindings(self):
        result = self.run_stage()
        before = {p.name: p.read_bytes() for p in self.output.iterdir()}
        manifest = self.module.verify_staged(self.output)
        self.assertEqual(manifest['binary_sha256'], self.files['binary']['sha256'])
        self.assertEqual(result['candidate_id'], manifest['candidate_id'])
        self.assertEqual(before, {p.name: p.read_bytes() for p in self.output.iterdir()})

    def test_read_only_verification_rejects_post_stage_changes(self):
        self.run_stage()
        target = self.output/'scanner-onefile-linux-amd64'
        target.chmod(0o644)
        target.write_bytes(b'poison')
        with self.assertRaises(ValueError):
            self.module.verify_staged(self.output)

    def test_read_only_verification_requires_final_receipt(self):
        self.run_stage()
        (self.output/'receipt.json').unlink()
        with self.assertRaises((ValueError, OSError)):
            self.module.verify_staged(self.output)

    def test_read_only_verification_rejects_changed_manifest_and_checksums(self):
        for name in ['manifest.json', 'SHA256SUMS']:
            with self.subTest(name=name):
                self.run_stage()
                path = self.output/name
                path.chmod(0o644)
                path.write_text('{}\n')
                with self.assertRaises((ValueError, OSError, KeyError)):
                    self.module.verify_staged(self.output)
                import shutil
                shutil.rmtree(self.output)

    def test_stage_rehashes_and_copies_materials_without_publication_approval(self):
        result = self.run_stage()
        manifest = json.loads((self.output/'manifest.json').read_text())
        self.assertFalse(manifest['publication_approved'])
        self.assertEqual(manifest['version'], self.version)
        self.assertEqual(result['manifest_sha256'], digest((self.output/'manifest.json').read_bytes()))
        for record in manifest['files'].values():
            self.assertEqual(digest((self.output/record['file']).read_bytes()), record['sha256'])
        self.assertNotIn(str(self.inputs), (self.output/'manifest.json').read_text())

    def test_missing_material_is_rejected_without_final_output(self):
        self.files.pop('native-source')
        with self.assertRaises(ValueError):
            self.run_stage()
        self.assertFalse(self.output.exists())

    def test_hash_mismatch_is_rejected_without_partial_final_output(self):
        (self.inputs/'binary').write_bytes(b'poison')
        with self.assertRaises(ValueError):
            self.run_stage()
        self.assertFalse(self.output.exists())

    def test_symlink_input_is_rejected(self):
        p = self.inputs/'binary'
        p.unlink()
        p.symlink_to('image')
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_parent_symlink_and_traversal_are_rejected(self):
        (self.inputs/'linked').symlink_to(self.inputs, target_is_directory=True)
        for path in ['linked/binary', '../inputs/binary', '/etc/passwd']:
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.files['binary']['path'] = path
                self.run_stage()

    def test_existing_output_is_never_overwritten(self):
        self.output.mkdir()
        (self.output/'keep').write_text('original')
        with self.assertRaises(FileExistsError):
            self.run_stage()
        self.assertEqual((self.output/'keep').read_text(), 'original')

    def test_source_candidate_mismatch_is_rejected(self):
        data = json.loads((self.inputs/'source-bindings').read_text())
        data['candidate_sha256'] = '0'*64
        self.put('source-bindings', data)
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_image_archive_mismatch_is_rejected(self):
        data = json.loads((self.inputs/'image-inventory').read_text())
        data['image_archive_sha256'] = '0'*64
        self.put('image-inventory', data)
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_terminal_image_and_runtime_controls_are_bound_to_the_same_source(self):
        self.run_stage()
        manifest=self.module.verify_staged(self.output)
        self.assertEqual(set(manifest['runtime_images']),{'core','tesseract'})
        self.assertEqual(manifest['runtime_images']['tesseract']['image_id'],'sha256:'+'c'*64)

    def test_changed_terminal_runtime_version_payload_or_source_cannot_be_sealed(self):
        for role,key,value in [('tesseract-image-version','native_runtime_sha256','0'*64),
                ('tesseract-image-payload','binary_sha256','0'*64),
                ('tesseract-image-payload','container_id','0'*64),
                ('runtime-inputs','source_revision','0'*40),
                ('tesseract-image-inventory','image_archive_sha256','0'*64)]:
            with self.subTest(role=role,key=key):
                original=(self.inputs/role).read_bytes();data=json.loads(original);data[key]=value;self.put(role,data)
                with self.assertRaises(ValueError):self.run_stage()
                self.assertFalse(self.output.exists());self.put(role,original)

    def test_project_license_mismatch_is_rejected(self):
        self.put('license', b'another license')
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_deployment_from_another_source_is_rejected(self):
        self.put('deployment-inventory', {'passed': True, 'source_archive_sha256': '0'*64,
                                         'archive_sha256': self.files['deployment']['sha256']})
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_raw_reports_must_match_recorded_scan_hashes(self):
        data = json.loads((self.inputs/'security').read_text())
        data['scans']['image']['artifacts_sha256']['grype.json'] = '0'*64
        self.put('security', data)
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_malformed_raw_report_archive_is_rejected(self):
        self.put('security-reports', b'not a tar')
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_image_sbom_identity_is_checked_even_if_scan_hash_agrees(self):
        self.put('image-sbom', {'artifacts': [{'name': 'other', 'type': 'deb'}],
                                'source': {'type': 'image', 'metadata': {'imageID': 'sha256:'+'0'*64}}})
        data = json.loads((self.inputs/'security').read_text())
        data['scans']['image']['input_sha256'] = self.files['image-sbom']['sha256']
        self.put('security', data)
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_scan_for_another_sbom_is_rejected(self):
        data = json.loads((self.inputs/'security').read_text())
        data['scans']['image']['input_sha256'] = '0'*64
        self.put('security', data)
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_failed_and_incomplete_scans_are_preserved_as_unpassed(self):
        data = json.loads((self.inputs/'security').read_text())
        data['scans']['native-cpe']['exit_code'] = 2
        data['scans']['image']['scan_completed'] = False
        self.put('security', data)
        self.run_stage()
        manifest = json.loads((self.output/'manifest.json').read_text())
        self.assertFalse(manifest['advisory_threshold_passed'])
        self.assertFalse(manifest['scans_completed'])
        self.assertFalse(manifest['publication_approved'])


if __name__ == '__main__':
    unittest.main()
