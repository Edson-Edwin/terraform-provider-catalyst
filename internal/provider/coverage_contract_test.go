// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestCapabilityInventoryMatchesProvider(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "coverage", "capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version      int `json:"schema_version"`
		Capabilities []struct {
			ID        string   `json:"id"`
			Status    string   `json:"status"`
			Resources []string `json:"resources"`
			Sources   []string `json:"data_sources"`
		} `json:"capabilities"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Capabilities) == 0 {
		t.Fatal("invalid inventory")
	}
	server := providerserver.NewProtocol6(New("test")())()
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, schemas.Diagnostics)
	ids, resources, sources := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, entry := range manifest.Capabilities {
		if entry.ID == "" || ids[entry.ID] {
			t.Fatalf("duplicate/missing identity %q", entry.ID)
		}
		ids[entry.ID] = true
		if entry.Status != "managed" && entry.Status != "discovery" && entry.Status != "unimplemented" {
			t.Fatalf("invalid status: %s", entry.ID)
		}
		if (entry.Status == "managed") != (len(entry.Resources) > 0) {
			t.Fatalf("false managed claim: %s", entry.ID)
		}
		if entry.Status == "discovery" && len(entry.Sources) == 0 {
			t.Fatalf("false discovery claim: %s", entry.ID)
		}
		if entry.Status == "unimplemented" && (len(entry.Resources)+len(entry.Sources) > 0) {
			t.Fatalf("false unimplemented claim: %s", entry.ID)
		}
		for kind, names := range map[string][]string{"resources": entry.Resources, "data-sources": entry.Sources} {
			for _, name := range names {
				seen := sources
				if kind == "resources" {
					seen = resources
				}
				if seen[name] || !strings.HasPrefix(name, "catalyst_") {
					t.Fatalf("duplicate/invalid type %q", name)
				}
				seen[name] = true
				if _, err = os.Stat(filepath.Join(root, "docs", kind, strings.TrimPrefix(name, "catalyst_")+".md")); err != nil {
					t.Fatalf("missing documentation for %s", name)
				}
				if _, err = os.Stat(filepath.Join(root, "examples", kind, name)); err != nil {
					t.Fatalf("missing example for %s", name)
				}
			}
		}
	}
	if len(resources) != len(schemas.ResourceSchemas) || len(sources) != len(schemas.DataSourceSchemas) {
		t.Fatal("catalog/schema count mismatch")
	}
	for name := range schemas.ResourceSchemas {
		if !resources[name] {
			t.Fatalf("uncatalogued resource %s", name)
		}
	}
	for name := range schemas.DataSourceSchemas {
		if !sources[name] {
			t.Fatalf("uncatalogued source %s", name)
		}
	}
}
