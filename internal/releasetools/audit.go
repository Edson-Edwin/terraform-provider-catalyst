// SPDX-License-Identifier: MPL-2.0
package releasetools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CapabilityAudit separates implemented operations from contract and evidence gaps.
// Entries are a reviewed backlog, not proof of live compatibility or full coverage.
type CapabilityAudit struct {
	ID            string            `json:"id"`
	Priority      string            `json:"priority"`
	Review        string            `json:"review"`
	APIContract   string            `json:"api_contract"`
	Operations    map[string]string `json:"operations"`
	Evidence      string            `json:"evidence"`
	Blockers      []string          `json:"blockers"`
	NextAction    string            `json:"next_action"`
	Sources       []string          `json:"sources"`
	RemainingGaps []string          `json:"remaining_gaps"`
	ReadFields    []AuditReadField  `json:"read_fields,omitempty"`
}

// AuditReadField connects a reviewed response field to its public Terraform attribute.
type AuditReadField struct {
	APIField   string `json:"api_field"`
	DataSource string `json:"data_source"`
	Attribute  string `json:"attribute"`
}

type Audit struct {
	SchemaVersion     int               `json:"schema_version"`
	Reviewed          string            `json:"reviewed"`
	InventoryComplete bool              `json:"inventory_complete"`
	Scope             string            `json:"scope"`
	Capabilities      []CapabilityAudit `json:"capabilities"`
}

var auditOperations = []string{"create", "read", "update", "delete", "import"}
var auditStates = []string{"implemented", "replacement", "reset_on_destroy", "not_implemented"}
var auditBlockers = []string{"api_contract", "account_eligibility", "implementation", "live_fixture", "live_verification", "deprecation"}

// Audit validates exact inventory membership before returning the offline backlog.
// It does not use credentials, run commands, or make network requests.
func (t *Tools) Audit() (*Audit, error) {
	inventory, err := t.inventory()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(t.Root, "coverage", "audit.json"))
	if err != nil {
		return nil, err
	}
	var audit Audit
	if err = json.Unmarshal(raw, &audit); err != nil {
		return nil, err
	}
	if audit.SchemaVersion != 1 || strings.TrimSpace(audit.Scope) == "" {
		return nil, errors.New("unsupported or incomplete capability audit")
	}
	if _, err = time.Parse("2006-01-02", audit.Reviewed); err != nil {
		return nil, errors.New("audit requires a valid reviewed date")
	}
	if audit.InventoryComplete != inventory.Complete {
		return nil, errors.New("audit completeness differs from the capability inventory")
	}
	capabilities := make(map[string]Capability, len(inventory.Capabilities))
	for _, capability := range inventory.Capabilities {
		if capability.ID == "" {
			return nil, errors.New("capability inventory contains an empty identity")
		}
		if _, exists := capabilities[capability.ID]; exists {
			return nil, errors.New("capability inventory contains duplicate identities")
		}
		if !slices.Contains([]string{"managed", "discovery", "unimplemented"}, capability.Status) {
			return nil, errors.New("capability inventory contains an unknown status")
		}
		capabilities[capability.ID] = capability
	}
	seen := make(map[string]bool, len(audit.Capabilities))
	for _, entry := range audit.Capabilities {
		capability, exists := capabilities[entry.ID]
		if !exists || seen[entry.ID] {
			return nil, fmt.Errorf("unknown or duplicate audit identity: %s", entry.ID)
		}
		seen[entry.ID] = true
		if err = t.validateAuditEntry(entry, capability); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.ID, err)
		}
	}
	if len(seen) != len(capabilities) {
		return nil, errors.New("audit omits capability inventory entries")
	}
	slices.SortFunc(audit.Capabilities, func(a, b CapabilityAudit) int {
		if priority := strings.Compare(a.Priority, b.Priority); priority != 0 {
			return priority
		}
		return strings.Compare(a.ID, b.ID)
	})
	return &audit, nil
}

func (t *Tools) validateAuditEntry(entry CapabilityAudit, capability Capability) error {
	if !slices.Contains([]string{"P0", "P1", "P2", "P3", "P4"}, entry.Priority) ||
		!slices.Contains([]string{"source_reviewed", "inventory_only"}, entry.Review) ||
		!slices.Contains([]string{"documented", "official_sdk", "official_cli_candidate", "console_only_documented", "unknown"}, entry.APIContract) {
		return errors.New("invalid priority, review state or API contract classification")
	}
	if strings.TrimSpace(entry.Evidence) == "" || strings.TrimSpace(entry.NextAction) == "" || len(entry.Sources) == 0 {
		return errors.New("evidence, next action and source references are required")
	}
	if len(entry.Operations) != len(auditOperations) {
		return errors.New("all five lifecycle operations must be audited")
	}
	for _, operation := range auditOperations {
		state := entry.Operations[operation]
		if !slices.Contains(auditStates, state) {
			return fmt.Errorf("invalid or missing %s implementation status", operation)
		}
		if state == "replacement" && operation != "update" || state == "reset_on_destroy" && operation != "delete" {
			return errors.New("replacement/reset status assigned to the wrong operation")
		}
		isDiscoveryRead := capability.Status == "discovery" && operation == "read" && state == "implemented"
		if capability.Status != "managed" && state != "not_implemented" && !isDiscoveryRead {
			return errors.New("audit claims management not present in the inventory")
		}
	}
	if (capability.Status == "managed" || capability.Status == "discovery") && entry.Operations["read"] != "implemented" {
		return errors.New("audit omits an implemented read operation")
	}
	if capability.Status == "managed" && (entry.Operations["create"] != "implemented" || entry.Operations["import"] != "implemented" || entry.Operations["update"] == "not_implemented" || entry.Operations["delete"] == "not_implemented") {
		return errors.New("managed capability omits a lifecycle operation")
	}
	if capability.Status == "managed" && len(capability.Resources) == 0 || capability.Status == "discovery" && len(capability.DataSources) == 0 {
		return errors.New("inventory implementation has no Terraform type")
	}
	if capability.Status != "managed" && (!slices.Contains(entry.Blockers, "api_contract") || !slices.Contains(entry.Blockers, "implementation")) {
		return errors.New("unmanaged capability must track API-contract and implementation work")
	}
	seenBlockers := map[string]bool{}
	for _, blocker := range entry.Blockers {
		if !slices.Contains(auditBlockers, blocker) || seenBlockers[blocker] {
			return errors.New("invalid or duplicate blocker")
		}
		seenBlockers[blocker] = true
	}
	for _, source := range entry.Sources {
		if err := t.validateAuditSource(source); err != nil {
			return err
		}
	}
	seenFields := map[string]bool{}
	for _, field := range entry.ReadFields {
		if !slices.Contains(capability.DataSources, field.DataSource) || strings.TrimSpace(field.APIField) == "" {
			return errors.New("audited read field needs an API field and a data source for this capability")
		}
		for _, name := range strings.Split(field.Attribute, ".") {
			if strings.TrimSpace(name) == "" {
				return errors.New("audited read field has an empty attribute name")
			}
		}
		identity := field.DataSource + "." + field.Attribute
		if seenFields[identity] {
			return fmt.Errorf("duplicate audited read attribute: %s", identity)
		}
		seenFields[identity] = true
	}
	return nil
}

func (t *Tools) validateAuditSource(source string) error {
	if strings.HasPrefix(source, "https://") {
		parsed, err := url.Parse(source)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil {
			return errors.New("invalid public source URL")
		}
		return nil
	}
	file := strings.SplitN(source, "#", 2)[0]
	if file == "" || !filepath.IsLocal(file) {
		return errors.New("audit source must be a repository file or HTTPS URL")
	}
	info, err := os.Stat(filepath.Join(t.Root, file))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("missing audit source file: %s", file)
	}
	return nil
}

// WriteAudit prints either the validated JSON or a deterministic prioritized backlog.
func (t *Tools) WriteAudit(w io.Writer, asJSON bool) error {
	audit, err := t.Audit()
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(audit)
	}
	completeness := "draft; service/field audit incomplete"
	if audit.InventoryComplete {
		completeness = "complete"
	}
	reviewed := 0
	for _, entry := range audit.Capabilities {
		if entry.Review == "source_reviewed" {
			reviewed++
		}
	}
	if _, err = fmt.Fprintf(w, "Capability audit: %s\nReviewed: %s\nTracked areas: %d\nTargeted source checks: %d; retained for further audit: %d\nOperation status describes code, not live verification.\n\n", completeness, audit.Reviewed, len(audit.Capabilities), reviewed, len(audit.Capabilities)-reviewed); err != nil {
		return err
	}
	for _, entry := range audit.Capabilities {
		if _, err = fmt.Fprintf(w, "%s %s [%s; API evidence: %s]\n  Blockers: %s\n  Next: %s\n", entry.Priority, entry.ID, entry.Review, entry.APIContract, strings.Join(entry.Blockers, ", "), entry.NextAction); err != nil {
			return err
		}
	}
	return nil
}
