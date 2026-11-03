import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('release_ref', Path(__file__).with_name('check-release-ref.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseRefTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        self.git('init', '-b', 'main')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                 'commit', '--allow-empty', '-m', 'initial')
        self.sha = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', self.sha)

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.repo, check=True,
                              capture_output=True, text=True).stdout.strip()

    def check(self, ref, sha=None):
        return module.release_ref(ref, sha or self.sha, self.repo)

    def test_trunk_is_preview(self):
        result = self.check('refs/heads/main')
        self.assertEqual(result['channel'], 'preview')
        self.assertEqual(result['source_sha'], self.sha)
        self.assertTrue(result['prerelease'])

    def test_stable_and_prerelease_tags(self):
        for tag, prerelease in [('v1.2.3', False), ('v1.2.3-rc.1', True)]:
            with self.subTest(tag=tag):
                self.git('tag', tag)
                result = self.check('refs/tags/' + tag)
                self.assertEqual(result['channel'], 'release')
                self.assertEqual(result['version'], tag)
                self.assertEqual(result['prerelease'], prerelease)

    def test_annotated_tag_resolves_to_commit(self):
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                 'tag', '-a', 'v1.2.4', '-m', 'release')
        result = self.check('refs/tags/v1.2.4', self.git('rev-parse', 'v1.2.4'))
        self.assertEqual(result['source_sha'], self.sha)

    def test_invalid_refs_and_versions(self):
        for ref in ['refs/heads/feature', 'refs/tags/preview', 'refs/tags/v1.2',
                    'refs/tags/v01.2.3', 'refs/tags/v1.2.3-rc.01',
                    'refs/tags/v1.2.3+build', 'refs/tags/v1.2.3;echo x']:
            with self.subTest(ref=ref), self.assertRaises(ValueError):
                self.check(ref)
        with self.assertRaises(ValueError):
            self.check('refs/heads/main', '$(id)')

    def test_tag_outside_trunk_rejected(self):
        self.git('checkout', '-b', 'feature')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                 'commit', '--allow-empty', '-m', 'outside trunk')
        sha = self.git('rev-parse', 'HEAD')
        self.git('tag', 'v2.0.0')
        with self.assertRaises(subprocess.CalledProcessError):
            self.check('refs/tags/v2.0.0', sha)
        with self.assertRaises(subprocess.CalledProcessError):
            self.check('refs/heads/main', sha)

    def test_wrong_checkout_or_tag_rejected(self):
        self.git('tag', 'v1.0.0')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                 'commit', '--allow-empty', '-m', 'next')
        with self.assertRaises(ValueError):
            self.check('refs/tags/v1.0.0')
        with self.assertRaises(ValueError):
            self.check('refs/tags/v1.0.0', self.git('rev-parse', 'HEAD'))
        self.git('update-ref', 'refs/remotes/origin/main', self.git('rev-parse', 'HEAD'))
        self.git('checkout', '--detach', self.sha)
        self.assertEqual(self.check('refs/heads/main')['source_sha'], self.sha)
        self.assertEqual(self.check('refs/tags/v1.0.0')['source_sha'], self.sha)


if __name__ == '__main__':
    unittest.main()
