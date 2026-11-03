"""Render Compose contracts without contacting a daemon or scanner."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCANNER = {
    'SCANNER_SCANIMAGE_PATH': '/opt/native/scanimage',
    'SCANNER_SANE_PARAMETERS_PATH': '/opt/native/sane-parameters',
    'SCANNER_SANE_LIBRARY_DIRS_JSON': '["/opt/native/lib"]',
}
OCR = {
    'SCANNER_OCR_TESSERACT_PATH': '/opt/native/tesseract',
    'SCANNER_OCR_TESSERACT_DATA_DIR': '/opt/native/tessdata',
}


def render(**overrides):
    compose = shutil.which('docker-compose')
    command = [compose] if compose else ['docker', 'compose']
    with tempfile.TemporaryDirectory() as scratch:
        env = {
            'PATH': os.environ['PATH'], 'HOME': scratch,
            'DOCKER_CONFIG': scratch, 'DOCKER_HOST': 'unix:///nonexistent.sock',
            'SCANNER_IMAGE': 'registry.example.test/scanner@sha256:' + '1'*64,
            'SCANNER_PUBLIC_URL': 'https://scanner.example.test',
            'SCANNER_SCANNERS_JSON': '[]',
            'SCANNER_STATE_HOST_PATH': '/fixture/state',
            'SCANNER_DOCUMENT_HOST_PATH': '/fixture/documents',
            'SCANNER_OIDC_CLIENT_SECRET': 'dummy-api-secret',
            'UNRELATED_SECRET': 'dummy-unrelated-secret',
            **overrides,
        }
        result = subprocess.run(
            [*command, '--project-name', 'scanner-contract', '--env-file', '/dev/null',
             '-f', str(ROOT/'deploy/compose.yaml'), 'config', '--format', 'json'],
            env=env, capture_output=True, text=True, check=True, timeout=30)
        return json.loads(result.stdout)['services']


class ComposeNativeTests(unittest.TestCase):
    def test_defaults_preserve_bundled_paths(self):
        for service in render().values():
            for name in SCANNER | OCR:
                self.assertIsNone(service['environment'].get(name), name)

    def test_explicit_overrides_and_role_isolation(self):
        services = render(**SCANNER, **OCR)
        for role, service in services.items():
            env = service['environment']
            for name, value in OCR.items():
                self.assertEqual(env[name], value)
            for name, value in SCANNER.items():
                if role == 'worker':
                    self.assertEqual(env[name], value)
                else:
                    self.assertNotIn(name, env)
            self.assertNotIn('UNRELATED_SECRET', env)
        self.assertEqual(services['api']['environment']['SCANNER_OIDC_CLIENT_SECRET'], 'dummy-api-secret')
        self.assertNotIn('SCANNER_OIDC_CLIENT_SECRET', services['worker']['environment'])

    def test_empty_override_is_not_replaced_with_default(self):
        for role, service in render(**{name: '' for name in SCANNER | OCR}).items():
            for name in (SCANNER | OCR if role == 'worker' else OCR):
                self.assertEqual(service['environment'][name], '')

    def test_entrypoint_and_private_extraction_storage(self):
        for role, service in render().items():
            self.assertIsNone(service.get('entrypoint'))
            self.assertEqual(service['command'], ['serve' if role == 'api' else 'worker'])
            self.assertTrue(service['read_only'])
            self.assertEqual(service['user'], '10001:10001')
            self.assertEqual(service['environment']['XDG_CACHE_HOME'], '/tmp')
            self.assertIn('/tmp:rw,exec,nosuid,nodev,size=512m,mode=1777', service['tmpfs'])

    def test_env_example_does_not_override_bundled_paths(self):
        active = {line.split('=', 1)[0] for line in (ROOT/'.env.example').read_text().splitlines()
                  if line and not line.startswith('#')}
        self.assertFalse(active.intersection(SCANNER | OCR))


if __name__ == '__main__':
    unittest.main()
