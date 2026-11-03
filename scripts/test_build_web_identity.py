import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('build_web', Path(__file__).with_name('build-web.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class BuildWebIdentityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        self.git('init', '-b', 'main')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.test',
                 'commit', '--allow-empty', '-m', 'fixture')
        self.sha = self.git('rev-parse', 'HEAD')
        self.git('update-ref', 'refs/remotes/origin/main', self.sha)

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.repo, check=True,
                              capture_output=True, text=True).stdout.strip()

    def test_development_keeps_default_identity(self):
        self.assertEqual(module.identity_flags(None, None, self.repo), [])

    def test_trunk_embeds_preview_and_commit(self):
        flags = module.identity_flags('refs/heads/main', self.sha, self.repo)
        self.assertIn('Version=preview-' + self.sha[:12], flags[-1])
        self.assertIn('Revision=' + self.sha, flags[-1])
        self.assertIn('-buildvcs=false', flags)

    def test_tag_embeds_tagged_commit(self):
        for tag in ['v1.2.3', 'v1.2.3-rc.1']:
            with self.subTest(tag=tag):
                self.git('tag', tag)
                flags = module.identity_flags('refs/tags/' + tag, self.sha, self.repo)
                self.assertIn('Version=' + tag + ' ', flags[-1])
                self.assertIn('Revision=' + self.sha, flags[-1])

    def test_partial_identity_fails_before_dependency_install(self):
        for args in [['--ref', 'refs/heads/main'], ['--sha', self.sha]]:
            with self.subTest(args=args), patch.object(module.subprocess, 'run') as run:
                with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                    module.main(['--output', str(self.repo / 'scanner'), *args])
                run.assert_not_called()

    def test_wrong_ref_or_commit_fails(self):
        for ref, sha in [('refs/tags/v1.2.3 -X other=value', self.sha),
                         ('refs/heads/main', '0' * 40)]:
            with self.subTest(ref=ref), self.assertRaises(subprocess.CalledProcessError):
                module.identity_flags(ref, sha, self.repo)


if __name__ == '__main__':
    unittest.main()
