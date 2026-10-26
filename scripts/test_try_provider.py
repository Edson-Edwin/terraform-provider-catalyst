"""Safety checks for the local evaluation launcher; no cloud calls."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("trial", Path(__file__).with_name("try-provider.py"))
trial = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(trial)


class TrialTests(unittest.TestCase):
    def account(self, **extra):
        return dict(CATALYST_DATA_CENTER="in", CATALYST_ORGANIZATION_ID="123",
                    CATALYST_ACCESS_TOKEN="test-credential", **extra)

    def test_inherited_logging_flags_and_workspace_are_not_used(self):
        env = trial.child_environment({"PATH": "/bin", "TF_LOG": "TRACE", "TF_LOG_PATH": "secret.log",
                                       "TF_CLI_ARGS_apply": "-auto-approve", "TF_WORKSPACE": "production",
                                       "TF_VAR_project_id": "999", "TF_REATTACH_PROVIDERS": "{}"})
        self.assertEqual(env["PATH"], "/bin")
        self.assertFalse(any(key.startswith(("TF_LOG", "TF_CLI_ARGS", "TF_VAR_")) for key in env))
        self.assertNotIn("TF_WORKSPACE", env)
        self.assertNotIn("TF_REATTACH_PROVIDERS", env)

    def test_production_and_ambiguous_credentials_are_rejected(self):
        for env in (self.account(CATALYST_ENVIRONMENT="Production"),
                    self.account(CATALYST_CLIENT_SECRET="another-test-credential")):
            with self.assertRaises(ValueError):
                trial.account_environment(env)

    def test_incomplete_refresh_credentials_do_not_prompt_for_token(self):
        env = {"CATALYST_DATA_CENTER": "in", "CATALYST_ORGANIZATION_ID": "123", "CATALYST_CLIENT_ID": "example"}
        with patch.object(trial.getpass, "getpass") as prompt, self.assertRaises(ValueError):
            trial.account_environment(env)
        prompt.assert_not_called()

    def test_deployment_identity_is_stable_and_credentials_are_not_saved(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(trial, "TRIAL", Path(directory)):
            (Path(directory) / "appsail").mkdir()
            env = self.account()
            trial.deployment_inputs(env, "456", "plan")
            first = json.loads((Path(directory) / "deployment.json").read_text())
            trial.deployment_inputs(env, None, "apply")
            self.assertEqual(first, json.loads((Path(directory) / "deployment.json").read_text()))
            self.assertRegex(first["app_name"], "^tfpreview[a-f0-9]{16}$")
            for path in Path(directory).rglob("*.json"):
                self.assertNotIn(env["CATALYST_ACCESS_TOKEN"], path.read_text())
            with self.assertRaises(ValueError):
                trial.deployment_inputs(env, "789", "apply")
            env["CATALYST_ORGANIZATION_ID"] = "789"
            with self.assertRaises(ValueError):
                trial.deployment_inputs(env, None, "destroy")

    def test_destroy_without_saved_identity_is_refused(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(trial, "TRIAL", Path(directory)):
            with self.assertRaises(ValueError):
                trial.deployment_inputs(self.account(), "456", "destroy")

    def test_default_command_only_plans_read_only_example(self):
        with patch.object(trial.sys, "argv", ["try-provider.py"]), patch.object(trial, "prepare"), \
                patch.object(trial, "account_environment", return_value=self.account()), \
                patch.object(trial, "run") as run, patch.object(trial, "deployment_inputs") as deployment:
            trial.main()
            command = run.call_args.args[0]
            self.assertIn("plan", command)
            self.assertIn("read-only", command[1])
            self.assertNotIn("apply", command)
            deployment.assert_not_called()


if __name__ == "__main__":
    unittest.main()
