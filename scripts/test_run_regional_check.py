"""Offline checks for secret isolation in the regional CI launcher."""

import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("regional", Path(__file__).with_name("run-regional-check.py"))
REGIONAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(REGIONAL)


class RegionalRunnerTests(unittest.TestCase):
    def test_secret_isolation_and_explicit_modes(self):
        source = {
            "PATH": "/tools", "CATALYST_ACCESS_TOKEN": "wrong-global",
            "CATALYST_TEST_US_PRODUCTION_ACCESS_TOKEN": "wrong-region",
            "CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES": "1",
            "CATALYST_FIXTURE_ACCESS_TOKEN": "selected-secret", "TF_LOG": "TRACE",
            "TF_LOG_PATH": "/tmp/unsafe.log", "TF_ACC": "0",
        }
        env = REGIONAL.fixture_environment(source, "in", "Development", "read-only")
        self.assertEqual(env["CATALYST_TEST_IN_DEVELOPMENT_ACCESS_TOKEN"], "selected-secret")
        self.assertEqual(env["CATALYST_TEST_REQUIRE_BOTH_AUTH_MODES"], "1")
        self.assertEqual(env["PATH"], "/tools")
        for key in source.keys() - {"PATH", "TF_ACC"}:
            self.assertNotIn(key, env)
        self.assertNotIn("CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES", env)

    def test_write_mode_does_not_authorize_fixture(self):
        env = REGIONAL.fixture_environment({}, "eu", "Production", "stratus-lifecycle")
        self.assertEqual(env["CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES"], "1")
        self.assertEqual(env["CATALYST_TEST_EU_PRODUCTION_ALLOW_STRATUS_WRITES"], "")

    def test_injection_diagnostics_do_not_disclose_values(self):
        for control in ("\n", "\r", "\x00"):
            with self.assertRaises(ValueError) as caught:
                REGIONAL.fixture_environment({"CATALYST_FIXTURE_ACCESS_TOKEN": "secret" + control}, "in", "Development", "read-only")
            self.assertNotIn("secret", str(caught.exception))

    def test_launcher_targets_both_auth_and_read_before_writes(self):
        for mode in ("read-only", "stratus-lifecycle"):
            with patch("sys.argv", ["runner", "--region", "jp", "--environment", "Production", "--mode", mode]), patch.object(REGIONAL.subprocess, "run") as run:
                run.return_value.returncode = 7
                with self.assertRaises(SystemExit) as caught:
                    REGIONAL.main()
                self.assertEqual(caught.exception.code, 7)
                args, kwargs = run.call_args
                self.assertIn("Authentication|StratusRead", " ".join(args[0]))
                self.assertEqual("StratusLifecycle" in " ".join(args[0]), mode == "stratus-lifecycle")
                self.assertEqual(kwargs["env"]["CATALYST_TEST_REGIONS"], "jp")


if __name__ == "__main__":
    unittest.main()
