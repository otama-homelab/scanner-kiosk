"""Render-only Kubernetes contracts; never contacts a cluster or scanner."""
from pathlib import Path
import subprocess
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]
BASE = ['helm', 'template', 'scanner-kiosk', 'deploy/helm',
        '--set-string', 'image.repository=registry.example.test/scanner-kiosk',
        '--set-string', 'image.digest=sha256:'+'1'*64,
        '--set-string', 'state.existingClaim=scanner-state',
        '--set-string', 'documents.existingClaim=scanner-documents']


def render(*extra):
    result = subprocess.run([*BASE, *extra], cwd=ROOT, check=True,
                            capture_output=True, text=True, timeout=30)
    resources = list(yaml.safe_load_all(result.stdout))
    deployment = next(r for r in resources if r['kind']=='Deployment')
    return deployment['spec']['template']['spec']


class KubernetesNativeTests(unittest.TestCase):
    def test_default_core_disables_ocr(self):
        for container in render()['containers']:
            env={e['name']:e.get('value') for e in container['env']}
            self.assertEqual(env['SCANNER_OCR_PLUGINS'],'')

    def test_optional_tesseract_has_no_application_storage_or_api_secret(self):
        result=subprocess.run([*BASE,'--set','tesseract.enabled=true','--set-string',
            'tesseract.image.digest=sha256:'+'2'*64,'--set-string','tesseract.secretName=ocr-only'],
            cwd=ROOT,capture_output=True,text=True,check=True,timeout=30)
        resources=list(yaml.safe_load_all(result.stdout))
        deployment=next(r for r in resources if r['kind']=='Deployment' and r['metadata']['name']=='scanner-kiosk-tesseract')
        pod=deployment['spec']['template']['spec'];container=pod['containers'][0]
        self.assertFalse(pod['automountServiceAccountToken'])
        self.assertTrue(container['securityContext']['readOnlyRootFilesystem'])
        self.assertEqual(container['securityContext']['capabilities']['drop'],['ALL'])
        self.assertNotIn('envFrom',container)
        self.assertEqual([m['mountPath'] for m in container['volumeMounts']],['/tmp'])
        self.assertEqual(pod['volumes'][0]['emptyDir']['medium'],'Memory')
        key=next(e for e in container['env'] if e['name']=='SCANNER_OCR_TESSERACT_API_KEY')
        self.assertEqual(key['valueFrom']['secretKeyRef'],{'name':'ocr-only','key':'SCANNER_OCR_TESSERACT_API_KEY'})
        policy=next(r for r in resources if r['kind']=='NetworkPolicy')
        self.assertEqual(policy['spec']['egress'],[])
        self.assertEqual(policy['spec']['ingress'][0]['from'][0]['podSelector']['matchLabels'],{'app.kubernetes.io/instance':'scanner-kiosk'})
        core=next(r for r in resources if r['kind']=='Deployment' and r['metadata']['name']=='scanner-kiosk')
        for c in core['spec']['template']['spec']['containers']:
            env={e['name']:e for e in c['env']}
            self.assertEqual(env['SCANNER_OCR_PLUGINS']['value'],'tesseract')
            self.assertEqual('SCANNER_OCR_TESSERACT_API_KEY' in env,c['name']=='worker')

    def test_optional_tesseract_requires_immutable_image_and_its_own_secret(self):
        for extra in [[],['--set-string','tesseract.image.digest=sha256:'+'2'*64],
                ['--set-string','tesseract.image.digest=latest','--set-string','tesseract.secretName=ocr-only']]:
            result=subprocess.run([*BASE,'--set','tesseract.enabled=true',*extra],cwd=ROOT,capture_output=True,text=True,timeout=30)
            self.assertNotEqual(result.returncode,0)
    def test_chart_lints(self):
        result = subprocess.run(['helm', 'lint', 'deploy/helm', *BASE[4:]], cwd=ROOT,
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_chart_rejects_invalid_deployment_inputs(self):
        cases = [
            ('missing inputs', BASE[:4], 'image.repository is required'),
            ('mutable image', [*BASE, '--set-string', 'image.digest=latest'], 'immutable sha256 digest'),
            ('shared storage', [*BASE, '--set-string', 'documents.existingClaim=scanner-state'], 'different claims'),
        ]
        for name, command, message in cases:
            with self.subTest(name=name):
                result = subprocess.run(command, cwd=ROOT, capture_output=True,
                                        text=True, timeout=30)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)

    def test_runtime_identity_defaults_and_numeric_overrides(self):
        for uid, gid, extra in [(10001, 10001, []),
                                (1000, 100, ['--set', 'runtimeIdentity.uid=1000', '--set', 'runtimeIdentity.gid=100']),
                                (4294967294, 4294967294, ['--set', 'runtimeIdentity.uid=4294967294', '--set', 'runtimeIdentity.gid=4294967294'])]:
            with self.subTest(uid=uid, gid=gid):
                pod = render(*extra)
                security = pod['securityContext']
                self.assertEqual(security['runAsUser'], uid)
                self.assertEqual(security['runAsGroup'], gid)
                self.assertIs(type(security['runAsUser']), int)
                self.assertIs(type(security['runAsGroup']), int)
                self.assertTrue(security['runAsNonRoot'])
                self.assertNotIn('fsGroup', security)
                for container in pod['containers']:
                    self.assertNotIn('runAsUser', container['securityContext'])
                    self.assertNotIn('runAsGroup', container['securityContext'])

    def test_chart_rejects_invalid_runtime_identity(self):
        for key, value, flag in [('uid', '0', '--set'), ('gid', '0', '--set'),
                                 ('uid', '-1', '--set'), ('gid', '4294967295', '--set'),
                                 ('uid', '1.5', '--set'), ('gid', 'root', '--set-string'),
                                 ('uid', '1000', '--set-string')]:
            with self.subTest(key=key, value=value, flag=flag):
                result = subprocess.run([*BASE, flag, f'runtimeIdentity.{key}={value}'], cwd=ROOT,
                                        capture_output=True, text=True, timeout=30)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('runtimeIdentity', result.stderr)

    def test_image_entrypoint_owns_executable(self):
        pod = render()
        for container in pod['containers']:
            self.assertNotIn('command', container)
            self.assertEqual(container['args'], ['serve' if container['name']=='api' else 'worker'])

    def test_private_runtime_directories_use_separate_ephemeral_volumes(self):
        pod = render()
        volumes = {v['name']:v for v in pod['volumes']}
        mounted = []
        for container in pod['containers']:
            env = {e['name']:e['value'] for e in container['env']}
            self.assertEqual(env['XDG_CACHE_HOME'], '/tmp')
            temp = next(m for m in container['volumeMounts'] if m['mountPath']=='/tmp')
            self.assertEqual(volumes[temp['name']]['emptyDir']['sizeLimit'], '512Mi')
            mounted.append(temp['name'])
            self.assertTrue(container['securityContext']['readOnlyRootFilesystem'])
        self.assertEqual(len(set(mounted)), 2)
        self.assertNotIn('fsGroup', pod['securityContext'])
        self.assertFalse(pod['automountServiceAccountToken'])

    def test_startup_probe_protects_self_extraction(self):
        api = next(c for c in render()['containers'] if c['name']=='api')
        startup = api['startupProbe']
        self.assertEqual(startup['httpGet'], {'path':'/health/live','port':'http'})
        self.assertGreaterEqual(startup['failureThreshold']*startup['periodSeconds'], 300)
        for name, path in [('livenessProbe','/health/live'), ('readinessProbe','/health/ready')]:
            self.assertEqual(api[name]['httpGet'], {'path':path,'port':'http'})
            self.assertGreater(api[name]['timeoutSeconds'], 0)

    def test_bundled_native_paths_are_not_overridden_by_chart_defaults(self):
        pod = render()
        names = {'SCANNER_SCANIMAGE_PATH','SCANNER_SANE_PARAMETERS_PATH','SCANNER_SANE_LIBRARY_DIRS_JSON',
                 'SCANNER_OCR_TESSERACT_PATH','SCANNER_OCR_TESSERACT_DATA_DIR'}
        for container in pod['containers']:
            self.assertFalse(names.intersection(e['name'] for e in container['env']))

    def test_explicit_native_path_override_remains_worker_only(self):
        pod = render('--set-string', 'workerEnv.SCANNER_SCANIMAGE_PATH=/opt/scanimage')
        for container in pod['containers']:
            env = {e['name']:e['value'] for e in container['env']}
            if container['name']=='worker':
                self.assertEqual(env['SCANNER_SCANIMAGE_PATH'], '/opt/scanimage')
            else:
                self.assertNotIn('SCANNER_SCANIMAGE_PATH', env)


if __name__=='__main__':
    unittest.main()
