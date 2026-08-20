// SPDX-License-Identifier: MPL-2.0
package releasetools

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func auditFixture() Audit {
	return Audit{
		SchemaVersion: 1, Reviewed: "2026-11-24", Scope: "Draft fixture; no live evidence.",
		Capabilities: []CapabilityAudit{{
			ID: "fixture", Priority: "P1", Review: "inventory_only", APIContract: "documented",
			Operations: map[string]string{"create": "implemented", "read": "implemented", "update": "implemented", "delete": "implemented", "import": "implemented"},
			Evidence:   "Offline fixture only.", Blockers: []string{"live_fixture", "live_verification"},
			NextAction: "Prepare a separate fixture.", Sources: []string{"coverage/capabilities.json"},
		}},
	}
}

func writeAuditFixture(t *testing.T, tool *Tools, audit Audit) {
	t.Helper()
	raw, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(tool.Root, "coverage", "audit.json"), raw)
}

func TestAuditRejectsIncompleteOrMisleadingEvidence(t *testing.T) {
	for _, scenario := range []string{"missing_capability", "unknown_capability", "duplicate_capability", "missing_operation", "unknown_operation", "invented_operation", "misplaced_reset", "misplaced_replacement", "incomplete_managed_lifecycle", "false_complete", "bad_date", "unknown_blocker", "duplicate_blocker", "missing_evidence", "missing_action", "missing_source", "outside_source", "bad_url", "missing_file"} {
		t.Run(scenario, func(t *testing.T) {
			tool, _ := fixtureTools(t)
			audit := auditFixture()
			entry := &audit.Capabilities[0]
			switch scenario {
			case "missing_capability":
				audit.Capabilities = nil
			case "unknown_capability":
				entry.ID = "unregistered"
			case "duplicate_capability":
				audit.Capabilities = append(audit.Capabilities, *entry)
			case "missing_operation":
				delete(entry.Operations, "read")
			case "unknown_operation":
				entry.Operations["read"] = "verified_by_guess"
			case "invented_operation":
				delete(entry.Operations, "import")
				entry.Operations["invoke"] = "implemented"
			case "misplaced_reset":
				entry.Operations["create"] = "reset_on_destroy"
			case "misplaced_replacement":
				entry.Operations["read"] = "replacement"
			case "incomplete_managed_lifecycle":
				entry.Operations["import"] = "not_implemented"
			case "false_complete":
				audit.InventoryComplete = true
			case "bad_date":
				audit.Reviewed = "2026-02-30"
			case "unknown_blocker":
				entry.Blockers = []string{"no_blocker"}
			case "duplicate_blocker":
				entry.Blockers = []string{"live_fixture", "live_fixture"}
			case "missing_evidence":
				entry.Evidence = " "
			case "missing_action":
				entry.NextAction = " "
			case "missing_source":
				entry.Sources = nil
			case "outside_source":
				entry.Sources = []string{"../outside.md"}
			case "bad_url":
				entry.Sources = []string{"https://user:secret@example.com/source"}
			case "missing_file":
				entry.Sources = []string{"missing.md"}
			}
			writeAuditFixture(t, tool, audit)
			if _, err := tool.Audit(); err == nil {
				t.Fatal("incomplete or misleading audit passed validation")
			}
		})
	}
}

func TestAuditDoesNotPromoteDiscoveryToManagement(t *testing.T) {
	for _, status := range []string{"discovery", "unimplemented"} {
		t.Run(status, func(t *testing.T) {
			tool, _ := fixtureTools(t)
			inventory := Inventory{Capabilities: []Capability{{ID: "fixture", Status: status}}}
			if status == "discovery" {
				inventory.Capabilities[0].DataSources = []string{"catalyst_fixtures"}
			}
			raw, err := json.Marshal(inventory)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(tool.Root, "coverage", "capabilities.json"), raw)
			audit := auditFixture()
			entry := &audit.Capabilities[0]
			entry.Blockers = []string{"api_contract", "implementation"}
			for _, operation := range auditOperations {
				entry.Operations[operation] = "not_implemented"
			}
			if status == "discovery" {
				entry.Operations["read"] = "implemented"
			}
			writeAuditFixture(t, tool, audit)
			if _, err = tool.Audit(); err != nil {
				t.Fatal(err)
			}
			entry.Operations["create"] = "implemented"
			writeAuditFixture(t, tool, audit)
			if _, err = tool.Audit(); err == nil {
				t.Fatal("a discovery/unknown contract was promoted to management")
			}
			entry.Operations["create"] = "not_implemented"
			entry.Blockers = []string{"live_fixture"}
			writeAuditFixture(t, tool, audit)
			if _, err = tool.Audit(); err == nil {
				t.Fatal("missing implementation/API work was hidden")
			}
		})
	}
}

type auditErrorWriter struct{}

func (auditErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAuditOutputIsOfflineAndPreservesLimits(t *testing.T) {
	tool, _ := fixtureTools(t)
	audit := auditFixture()
	writeAuditFixture(t, tool, audit)
	for _, asJSON := range []bool{false, true} {
		var output bytes.Buffer
		if err := tool.WriteAudit(&output, asJSON); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "fixture") || !strings.Contains(output.String(), "live_fixture") {
			t.Fatal("report omitted the backlog or its blockers")
		}
		if asJSON {
			var decoded Audit
			if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.InventoryComplete {
				t.Fatal("machine-readable report lost the incomplete-inventory limit")
			}
		} else if !strings.Contains(output.String(), "not live verification") {
			t.Fatal("human report conflated implemented operations and live evidence")
		}
		if err := tool.WriteAudit(auditErrorWriter{}, asJSON); err == nil {
			t.Fatal("report silently ignored an output failure")
		}
	}
}

func TestRepositoryAuditMatchesInventory(t *testing.T) {
	tool := New(filepath.Join("..", ".."))
	audit, err := tool.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.Capabilities) == 0 {
		t.Fatal("empty repository audit")
	}
	for i := 1; i < len(audit.Capabilities); i++ {
		previous, current := audit.Capabilities[i-1], audit.Capabilities[i]
		if previous.Priority > current.Priority || previous.Priority == current.Priority && previous.ID >= current.ID {
			t.Fatal("report does not sort the backlog by priority and capability ID")
		}
	}
}
