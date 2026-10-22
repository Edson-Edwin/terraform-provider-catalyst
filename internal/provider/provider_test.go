// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestProviderProtocol exercises the schema and configuration RPCs used by Terraform.
func TestProviderProtocol(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	schema, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, schema.Diagnostics)
	if schema.Provider == nil || schema.Provider.Block == nil {
		t.Fatal("provider schema is missing")
	}

	config := &tfprotov6.DynamicValue{JSON: []byte(`{}`)}
	validated, err := server.ValidateProviderConfig(t.Context(), &tfprotov6.ValidateProviderConfigRequest{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, validated.Diagnostics)

	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "1.16.5",
		Config:           config,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
}

func checkDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("%s: %s", diagnostic.Summary, diagnostic.Detail)
		}
	}
}
