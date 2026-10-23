// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"strings"
	"testing"
)

// A public sunset must be visible during configuration validation before any API write.
func TestFilestoreSunsetWarnings(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	resourceResult, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "catalyst_filestore_file", Config: jsonConfig(t, map[string]any{"project_id": "123", "folder_id": "456", "name": "example.txt", "source": "example.txt", "source_sha256": strings.Repeat("a", 64)})})
	if err != nil {
		t.Fatal(err)
	}
	dataResult, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: "catalyst_filestore_folders", Config: jsonConfig(t, map[string]any{"project_id": "123"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostics := range [][]*tfprotov6.Diagnostic{resourceResult.Diagnostics, dataResult.Diagnostics} {
		found := false
		for _, d := range diagnostics {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("deprecation must retain compatibility: %v", d)
			}
			if d.Severity == tfprotov6.DiagnosticSeverityWarning && strings.Contains(d.Detail, "31 October 2026") && strings.Contains(d.Detail, "catalyst_stratus_object") {
				found = true
			}
		}
		if !found {
			t.Fatal("File Store configuration did not surface its sunset and migration direction")
		}
	}
}
