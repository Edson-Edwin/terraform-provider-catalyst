// SPDX-License-Identifier: MPL-2.0
package releasetools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

type Gate struct {
	Name   string `json:"gate"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}
type WorkflowRun struct {
	Head       string `json:"head_sha"`
	Path       string `json:"path"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Number     int    `json:"run_number"`
	Attempt    int    `json:"run_attempt"`
}

func EvaluateReadiness(public bool, head string, dirty bool, runs []WorkflowRun, secrets []string, security bool) []Gate {
	var latest *WorkflowRun
	for i := range runs {
		r := &runs[i]
		if r.Head != head || r.Path != ".github/workflows/test.yml" || r.Event != "push" {
			continue
		}
		if latest == nil || r.Number > latest.Number || (r.Number == latest.Number && r.Attempt > latest.Attempt) {
			latest = r
		}
	}
	names := map[string]bool{}
	for _, name := range secrets {
		names[name] = true
	}
	cleanDetail := "Working tree is clean."
	if dirty {
		cleanDetail = "Commit the reviewed changes before tagging."
	}
	return []Gate{{"clean_source", !dirty, cleanDetail}, {"public_repository", public, "Terraform Registry requires a public source repository."}, {"ci_for_head", latest != nil && latest.Status == "completed" && latest.Conclusion == "success", "Require successful push checks for the exact local HEAD, including native OS tests."}, {"signing_secret_names", names["GPG_PRIVATE_KEY"] && names["PASSPHRASE"], "Checks repository secret names only; contents and signing identity require separate verification."}, {"private_security_reporting", security, "Enable private vulnerability reporting or verify an alternative private contact."}}
}

type ReadinessReport struct {
	CheckedAt  string   `json:"checked_at"`
	Repository string   `json:"repository"`
	Head       string   `json:"head"`
	Passed     bool     `json:"preflight_passed"`
	Checks     []Gate   `json:"checks"`
	Manual     []string `json:"remaining_manual_evidence"`
}

// Readiness makes explicit read-only hosted checks. Do not run while hosted CI is deferred.
func (t *Tools) Readiness(ctx context.Context, repo string) (*ReadinessReport, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) {
		return nil, errors.New("repository must be OWNER/NAME")
	}
	command := func(name string, args ...string) ([]byte, error) {
		return t.command(ctx, time.Minute, t.Root, os.Environ(), name, args...)
	}
	head, err := command("git", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	dirty, err := command("git", "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	raw, err := command("gh", "api", "repos/"+repo)
	if err != nil {
		return nil, err
	}
	var remote struct {
		Visibility string `json:"visibility"`
	}
	if json.Unmarshal(raw, &remote) != nil {
		return nil, errors.New("invalid GitHub repository response")
	}
	headString := strings.TrimSpace(string(head))
	raw, err = command("gh", "api", "repos/"+repo+"/actions/runs?head_sha="+headString+"&per_page=100")
	if err != nil {
		return nil, err
	}
	var runs struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	if json.Unmarshal(raw, &runs) != nil {
		return nil, errors.New("invalid workflow response")
	}
	unknown := map[string]bool{}
	secretNames := []string{}
	raw, err = command("gh", "secret", "list", "--repo", repo, "--json", "name")
	var secrets []struct {
		Name string `json:"name"`
	}
	if err != nil || json.Unmarshal(raw, &secrets) != nil {
		unknown["signing_secret_names"] = true
	} else {
		for _, s := range secrets {
			secretNames = append(secretNames, s.Name)
		}
	}
	raw, err = command("gh", "api", "repos/"+repo+"/private-vulnerability-reporting")
	var security struct {
		Enabled bool `json:"enabled"`
	}
	if err != nil || json.Unmarshal(raw, &security) != nil {
		unknown["private_security_reporting"] = true
	}
	checks := EvaluateReadiness(remote.Visibility == "public", headString, strings.TrimSpace(string(dirty)) != "", runs.Runs, secretNames, security.Enabled)
	passed := true
	for i := range checks {
		if unknown[checks[i].Name] {
			checks[i].Passed = false
			checks[i].Detail = "Unverified: API read unavailable; do not interpret this as disabled or absent."
		}
		passed = passed && checks[i].Passed
	}
	return &ReadinessReport{CheckedAt: time.Now().UTC().Format(time.RFC3339), Repository: repo, Head: headString, Passed: passed, Checks: checks, Manual: []string{"Review source and history for credentials before changing visibility.", "Verify signed draft downloads using the trusted maintainer fingerprint.", "Complete Registry registration and run the verify-registry command for the published version.", "Publish service and regional limits; preflight is not a coverage score."}}, nil
}
