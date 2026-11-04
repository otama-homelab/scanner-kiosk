"""Required status cannot hide a failed, cancelled or unexpectedly skipped job."""
from pathlib import Path
import subprocess
import unittest

SCRIPT = Path(__file__).with_name('check-required-jobs.sh')


class RequiredJobsTests(unittest.TestCase):
    def invoke(self, **changes):
        env = dict(EVENT_NAME='push', REF_RESULT='success', TEST_RESULT='success',
                   DEPLOY_RESULT='success', NATIVE_RESULT='success', SECURITY_RESULT='success')
        env.update(changes)
        return subprocess.run(['/bin/sh', str(SCRIPT)], env=env,
                              capture_output=True, timeout=5).returncode

    def test_supported_events(self):
        self.assertEqual(self.invoke(), 0)
        for event in ['pull_request', 'workflow_dispatch']:
            with self.subTest(event=event):
                self.assertEqual(self.invoke(EVENT_NAME=event, REF_RESULT='skipped'), 0)
                self.assertNotEqual(self.invoke(EVENT_NAME=event), 0)
        self.assertNotEqual(self.invoke(EVENT_NAME='pull_request_target'), 0)

    def test_any_required_job_must_succeed(self):
        for key in ['TEST_RESULT', 'DEPLOY_RESULT', 'REF_RESULT', 'NATIVE_RESULT', 'SECURITY_RESULT']:
            for result in ['', 'failure', 'cancelled', 'skipped', 'timed_out']:
                with self.subTest(job=key, result=result):
                    self.assertNotEqual(self.invoke(**{key: result}), 0)


if __name__ == '__main__':
    unittest.main()
