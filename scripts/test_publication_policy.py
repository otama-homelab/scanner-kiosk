import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import unittest
import test_release_ref as fixtures


class PublicationPolicyTest(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.ReleaseRefTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.repo, self.sha = self.fixture.repo, self.fixture.sha
        self.state = {'releases': [], 'image_tags': [], 'preview_source_sha': None}

    def plan(self, ref='refs/heads/main', event='push'):
        spec = importlib.util.spec_from_file_location(
            'publication_policy', Path(__file__).with_name('publication-policy.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.publication_plan(ref, self.sha, event, self.state, self.repo)

    def releases(self, *tags):
        self.state['releases'] = [{'tag_name': t, 'draft': False, 'prerelease': '-' in t} for t in tags]

    def next_commit(self):
        self.fixture.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                         'commit', '--allow-empty', '-m', 'next')
        result = self.fixture.git('rev-parse', 'HEAD')
        self.fixture.git('update-ref', 'refs/remotes/origin/main', result)
        return result

    def test_preview_keeps_full_commit_address_and_alias(self):
        plan = self.plan()
        self.assertEqual(plan['release_tag'], 'preview-'+self.sha)
        self.assertEqual(plan['image_tag'], 'sha-'+self.sha)
        self.assertEqual(plan['aliases'], ['preview'])
        self.assertTrue(plan['prerelease'])

    def test_cli_emits_a_plan_without_mutating_remote_state_or_git(self):
        state = self.repo/'remote-state.json'
        state.write_text(json.dumps(self.state))
        before = state.read_bytes()
        script = Path(__file__).with_name('publication-policy.py')
        result = subprocess.run([sys.executable, str(script), '--ref', 'refs/heads/main',
                                 '--sha', self.sha, '--event', 'push', '--remote-state', str(state)],
                                cwd=self.repo, check=True, capture_output=True, text=True)
        self.assertFalse(json.loads(result.stdout)['external_writes'])
        self.assertEqual(state.read_bytes(), before)
        self.assertEqual(self.fixture.git('tag'), '')

    def test_stable_versions_use_numeric_order(self):
        self.fixture.git('tag', 'v1.10.0')
        self.releases('v1.9.99', 'v1.11.0-rc.1')
        self.assertEqual(self.plan('refs/tags/v1.10.0')['aliases'], ['latest'])

    def test_late_old_stable_does_not_move_latest(self):
        self.fixture.git('tag', 'v1.9.0')
        self.releases('v1.10.0')
        self.assertEqual(self.plan('refs/tags/v1.9.0')['aliases'], [])

    def test_higher_draft_is_reserved_but_does_not_hold_back_latest(self):
        self.fixture.git('tag', 'v1.1.0')
        self.state['releases'] = [{'tag_name': 'v2.0.0', 'draft': True, 'prerelease': False}]
        self.assertEqual(self.plan('refs/tags/v1.1.0')['aliases'], ['latest'])

    def test_prerelease_never_moves_stable_alias(self):
        self.fixture.git('tag', 'v2.0.0-rc.1')
        plan = self.plan('refs/tags/v2.0.0-rc.1')
        self.assertEqual(plan['aliases'], [])
        self.assertTrue(plan['prerelease'])

    def test_late_old_preview_does_not_move_alias_backwards(self):
        newer = self.next_commit()
        self.fixture.git('checkout', '--detach', self.sha)
        self.state['preview_source_sha'] = newer
        self.assertEqual(self.plan()['aliases'], [])

    def test_descendant_preview_advances_alias(self):
        previous = self.sha
        self.sha = self.next_commit()
        self.state['preview_source_sha'] = previous
        self.assertEqual(self.plan()['aliases'], ['preview'])

    def test_pr_and_manual_source_checks_cannot_publish(self):
        for event in ['pull_request', 'workflow_dispatch', 'workflow_run', '']:
            with self.subTest(event=event), self.assertRaises(ValueError):
                self.plan(event=event)

    def test_existing_release_or_image_is_never_overwritten_on_rerun(self):
        for key, tag in [('releases', 'preview-'+self.sha),
                         ('image_tags', 'sha-'+self.sha)]:
            with self.subTest(key=key):
                self.state[key] = ([{'tag_name': tag, 'draft': False, 'prerelease': True}]
                                   if key == 'releases' else [tag])
                with self.assertRaises(ValueError):
                    self.plan()
                self.state[key] = []

    def test_existing_stable_version_is_immutable(self):
        self.fixture.git('tag', 'v1.2.3')
        self.releases('v1.2.3')
        with self.assertRaises(ValueError):
            self.plan('refs/tags/v1.2.3')

    def test_semver_exceeding_registry_tag_limit_is_rejected(self):
        tag = 'v'+'1'*128+'.0.0'
        self.fixture.git('tag', tag)
        with self.assertRaises(ValueError):
            self.plan('refs/tags/'+tag)

    def test_missing_remote_state_is_not_an_empty_registry(self):
        self.state.pop('image_tags')
        with self.assertRaises(ValueError):
            self.plan()

    def test_existing_alias_with_unknown_owner_is_not_reset(self):
        for alias in ['preview', 'latest']:
            with self.subTest(alias=alias), self.assertRaises(ValueError):
                self.state['image_tags'] = [alias]
                self.plan()

    def test_invalid_remote_values_are_rejected(self):
        for key, value in [('releases', [{'tag_name': 'v01.2.3', 'draft': False, 'prerelease': False}]), ('image_tags', 'not-a-list'),
                           ('preview_source_sha', '$(id)')]:
            original = self.state[key]
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.state[key] = value
                self.plan()
            self.state[key] = original

    def test_moved_tag_is_rejected_by_existing_ref_policy(self):
        self.fixture.git('tag', 'v1.2.3')
        self.sha = self.next_commit()
        with self.assertRaises(ValueError):
            self.plan('refs/tags/v1.2.3')


if __name__ == '__main__':
    unittest.main()
