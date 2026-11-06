import hashlib
import io
import json
import tarfile
import unittest
from unittest.mock import patch
import test_publication_preflight as fixtures


class PreviewPublicationTest(unittest.TestCase):
    def setUp(self):
        self.f = fixtures.PublicationPreflightTest()
        self.f.setUp()
        self.addCleanup(self.f.doCleanups)
        self.module = self.f.module
        self.review = self.f.candidate.root/'preview-review.json'
        self.policy = {'native_manifest_sha256': self.f.candidate.files['native-manifest']['sha256'],
                       'matches': {scope: [] for scope in ['image', 'native', 'native-cpe', 'onefile']}}
        self.review.write_text(json.dumps(self.policy))
        self.reports = {scope: [] for scope in self.policy['matches']}
        self.counter = 0

    def prepare(self, ref='refs/heads/main', event='push'):
        security = json.loads((self.f.candidate.inputs/'security').read_text())
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode='w') as archive:
            for scope, matches in self.reports.items():
                payload = json.dumps({'matches': matches}).encode()
                member = tarfile.TarInfo(scope+'/grype.json')
                member.size = len(payload)
                archive.addfile(member, io.BytesIO(payload))
                security['scans'][scope]['artifacts_sha256'] = {'grype.json': hashlib.sha256(payload).hexdigest()}
                security['scans'][scope]['match_count'] = len(matches)
        self.f.candidate.put('security-reports', raw.getvalue())
        self.f.candidate.put('security', security)
        self.counter += 1
        self.f.candidate.output = self.f.candidate.root/('preview-'+str(self.counter))
        self.f.candidate.run_stage()
        with patch.object(self.module, 'PREVIEW_REVIEW', self.review, create=True):
            return self.module.prepare_development_preview(self.f.candidate.output, ref,
                self.f.git.sha, event, self.f.git.repo)

    def test_preview_retains_sources_without_claiming_g5_or_shipping_stable_binaries(self):
        plan = self.prepare()
        self.assertIs(plan['development_preview'], True)
        self.assertEqual(plan['release_tag'], 'preview-source-'+self.f.git.sha)
        self.assertEqual(plan['image_tag'], 'sha-'+self.f.git.sha)
        self.assertEqual(plan['aliases'], ['preview'])
        self.assertNotIn('scanner-onefile-linux-amd64', plan['uploads'])
        self.assertNotIn('scanner-image.tar', plan['uploads'])
        self.assertIn('scanner-native-sources.tar', plan['uploads'])
        self.assertIn('THIRD-PARTY-NOTICES.md', plan['uploads'])
        self.assertNotIn('approval_sha256', plan)
        self.assertFalse(plan['external_writes'])

    def test_offline_preview_preflight_does_not_need_git_in_the_archive_tool(self):
        with patch.object(self.module.refs, 'release_ref', side_effect=FileNotFoundError('git')):
            plan = self.prepare()
        self.assertEqual(plan['source_sha'], self.f.git.sha)
        self.assertEqual(plan['version'], 'preview-'+self.f.git.sha[:12])

    def test_pr_manual_and_version_tags_cannot_use_development_preview(self):
        for ref, event in [('refs/tags/v1.0.0', 'push'), ('refs/heads/main', 'pull_request'),
                           ('refs/heads/main', 'workflow_dispatch'), ('refs/heads/other', 'push')]:
            with self.subTest(ref=ref, event=event), self.assertRaises(ValueError):
                self.prepare(ref, event)

    def test_native_bytes_require_the_exact_reviewed_manifest(self):
        self.policy['native_manifest_sha256'] = '0'*64
        self.review.write_text(json.dumps(self.policy))
        with self.assertRaises(ValueError):
            self.prepare()

    def test_unreviewed_findings_block_preview_and_reviewed_findings_remain_visible(self):
        row = {'vulnerability': {'id': 'CVE-fixture', 'severity': 'High'},
               'artifact': {'name': 'ocr-data', 'version': '1'}}
        self.reports['native-cpe'] = [row]
        with self.assertRaises(ValueError):
            self.prepare()
        self.policy['matches']['native-cpe'] = [{'id': 'CVE-fixture', 'severity': 'High',
                                               'package': 'ocr-data', 'version': '1'}]
        self.review.write_text(json.dumps(self.policy))
        plan = self.prepare()
        self.assertIn('security-reports.tar', plan['uploads'])
        self.reports['native-cpe'][0]['vulnerability']['severity'] = 'Critical'
        with self.assertRaises(ValueError):
            self.prepare()


if __name__ == '__main__':
    unittest.main()
