// SPDX-License-Identifier: MPL-2.0
package releasetools

import "testing"

func TestReadinessFailsClosed(t *testing.T) {
	for _, scenario := range []string{"valid", "old_head", "pending", "dirty", "new_failed_rerun", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			runs := []WorkflowRun{{Head: "head", Path: ".github/workflows/test.yml", Event: "push", Status: "completed", Conclusion: "success", Number: 1, Attempt: 1}}
			dirty := false
			public, security := true, true
			secrets := []string{"GPG_PRIVATE_KEY", "PASSPHRASE"}
			switch scenario {
			case "old_head":
				runs[0].Head = "old"
			case "pending":
				runs[0].Status = "in_progress"
			case "dirty":
				dirty = true
			case "new_failed_rerun":
				r := runs[0]
				r.Attempt = 2
				r.Conclusion = "failure"
				runs = append(runs, r)
			case "missing":
				public, security = false, false
				secrets = nil
				runs = nil
				dirty = true
			}
			checks := EvaluateReadiness(public, "head", dirty, runs, secrets, security)
			passed := true
			for _, check := range checks {
				passed = passed && check.Passed
			}
			if passed != (scenario == "valid") {
				t.Fatal("stale, missing or failed evidence passed")
			}
			if scenario == "missing" {
				for _, check := range checks {
					if check.Passed {
						t.Fatal("missing evidence passed a gate")
					}
				}
			}
		})
	}
}
