import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
import test_release_ref as git_fixtures
import test_stage_candidate as candidate_fixtures


class PublicationPreflightTest(unittest.TestCase):
    def setUp(self):
        self.git = git_fixtures.ReleaseRefTest()
        self.git.setUp()
        self.addCleanup(self.git.doCleanups)
        self.candidate = candidate_fixtures.StageCandidateTest()
        self.candidate.setUp()
        self.addCleanup(self.candidate.doCleanups)
        self.candidate.source = self.git.sha
        version = {**self.candidate.version, 'version': 'preview-'+self.git.sha[:12], 'revision': self.git.sha}
        self.set_identity(version)
        receipt = self.candidate.run_stage()
        self.approval_path = self.candidate.root/'approval.json'
        self.approval = {'format': 1, 'manifest_sha256': receipt['manifest_sha256'],
            'source_sha': self.git.sha, 'repository': 'otama-homelab/scanner-kiosk', 'visibility': 'private',
            'gates': {k: {'passed': True, 'evidence_sha256': 'f'*64}
                      for k in ['required_checks', 'security', 'license', 'g5', 'recipient_access', 'c1']}}
        spec = importlib.util.spec_from_file_location('publication_policy', Path(__file__).with_name('publication-policy.py'))
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)

    def set_identity(self, version):
        for role in ['application', 'onefile-inventory', 'source-bindings', 'native-source-index']:
            data = json.loads((self.candidate.inputs/role).read_text())
            if 'source_revision' in data:
                data['source_revision'] = self.git.sha
            if 'application_source_commit' in data:
                data['application_source_commit'] = self.git.sha
            if 'version' in data:
                data['version'] = version
            self.candidate.put(role, data)

    def prepare(self, event='push', approval_hash=None):
        raw = (json.dumps(self.approval)+'\n').encode()
        self.approval_path.write_bytes(raw)
        return self.module.prepare_publication(self.candidate.output, self.approval_path,
            approval_hash or hashlib.sha256(raw).hexdigest(), 'refs/heads/main', self.git.sha,
            event, {'releases': [], 'image_tags': [], 'preview_source_sha': None}, self.git.repo)

    def test_preflight_returns_exact_upload_inventory_without_writes(self):
        before = (self.candidate.output/'manifest.json').read_bytes()
        result = self.prepare()
        self.assertFalse(result['external_writes'])
        self.assertEqual(result['source_sha'], self.git.sha)
        self.assertEqual(result['repository'], 'otama-homelab/scanner-kiosk')
        self.assertIn('scanner-native-sources.tar', result['uploads'])
        self.assertIn('manifest.json', result['uploads'])
        self.assertEqual(before, (self.candidate.output/'manifest.json').read_bytes())

    def test_every_existing_acceptance_gate_is_required(self):
        for gate in self.approval['gates']:
            with self.subTest(gate=gate), self.assertRaises(ValueError):
                self.approval['gates'][gate]['passed'] = False
                self.prepare()
            self.approval['gates'][gate]['passed'] = True

    def test_approval_must_match_trusted_out_of_band_hash(self):
        with self.assertRaises(ValueError):
            self.prepare(approval_hash='0'*64)

    def test_approval_for_another_manifest_is_rejected(self):
        self.approval['manifest_sha256'] = '0'*64
        with self.assertRaises(ValueError):
            self.prepare()

    def test_approval_cannot_redirect_repository(self):
        self.approval['repository'] = 'another/repository'
        with self.assertRaises(ValueError):
            self.prepare()

    def test_gate_without_evidence_identity_is_rejected(self):
        self.approval['gates']['security'].pop('evidence_sha256')
        with self.assertRaises(ValueError):
            self.prepare()

    def test_local_development_identity_is_rejected_even_with_fixture_approvals(self):
        self.set_identity({**self.candidate.version, 'version': '0.0.0-dev', 'revision': 'unreleased'})
        self.candidate.output = self.candidate.root/'dev-candidate'
        receipt = self.candidate.run_stage()
        self.approval['manifest_sha256'] = receipt['manifest_sha256']
        with self.assertRaises(ValueError):
            self.prepare()

    def test_pr_and_manual_checks_cannot_use_fixture_approvals_to_publish(self):
        for event in ['pull_request', 'workflow_dispatch']:
            with self.subTest(event=event), self.assertRaises(ValueError):
                self.prepare(event=event)

    def test_missing_or_changed_staged_material_is_rejected(self):
        (self.candidate.output/'scanner-native-sources.tar').unlink()
        with self.assertRaises((ValueError, OSError)):
            self.prepare()


if __name__ == '__main__':
    unittest.main()
