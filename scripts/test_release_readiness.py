"""Release gates must fail closed for stale, failed or missing evidence."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('readiness', Path(__file__).with_name('release-readiness.py'))
readiness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(readiness)


class ReadinessTests(unittest.TestCase):
    def checks(self, *, sha='head', dirty=False, status='completed', conclusion='success', runs=None):
        if runs is None:
            runs = [{'head_sha': sha, 'path': '.github/workflows/test.yml', 'event': 'push', 'status': status, 'conclusion': conclusion}]
        return {item['gate']: item['passed'] for item in readiness.evaluate(
            {'visibility': 'public'}, 'head', dirty, runs,
            [{'name': 'GPG_PRIVATE_KEY'}, {'name': 'PASSPHRASE'}], {'enabled': True})}

    def test_preflight_is_not_publication(self):
        self.assertTrue(all(self.checks().values()))

    def test_old_sha_does_not_pass(self):
        self.assertFalse(self.checks(sha='old')['ci_for_head'])

    def test_pending_run_does_not_pass(self):
        self.assertFalse(self.checks(status='in_progress')['ci_for_head'])

    def test_dirty_tree_does_not_pass(self):
        self.assertFalse(self.checks(dirty=True)['clean_source'])

    def test_new_failed_rerun_wins(self):
        runs = [{'head_sha': 'head', 'path': '.github/workflows/test.yml', 'event': 'push',
                 'status': 'completed', 'conclusion': conclusion, 'run_number': 1, 'run_attempt': attempt}
                for attempt, conclusion in [(1, 'success'), (2, 'failure')]]
        self.assertFalse(self.checks(runs=runs)['ci_for_head'])

    def test_missing_evidence_fails(self):
        checks = readiness.evaluate({}, 'head', True, [], [], {})
        self.assertFalse(any(c['passed'] for c in checks))
