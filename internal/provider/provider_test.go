// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DATA_CENTER", "ENVIRONMENT", "ORGANIZATION_ID", "ACCESS_TOKEN", "CLIENT_ID", "CLIENT_SECRET", "REFRESH_TOKEN", "REDIRECT_URI", "ALLOWED_ORGANIZATION_IDS", "FORBIDDEN_ORGANIZATION_IDS", "VALIDATION_PROJECT_ID", "ALLOWED_DATA_CENTERS", "ALLOWED_ENVIRONMENTS"} {
		t.Setenv("CATALYST_"+name, "")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func checkDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, d := range diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("%s: %s", d.Summary, d.Detail)
		}
	}
}

func hasError(diagnostics []*tfprotov6.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}

func jsonConfig(t *testing.T, values map[string]any) *tfprotov6.DynamicValue {
	t.Helper()
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return &tfprotov6.DynamicValue{JSON: raw}
}

// TestProjectProtocol verifies provider configuration, API routing and Terraform state through RPCs.
func TestProjectProtocol(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "environment-token")
	t.Setenv("CATALYST_DATA_CENTER", "eu")
	calls := 0
	p := &CatalystProvider{version: "test", httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://api.catalyst.zoho.in/baas/v1/project/9007199254740993123" {
			t.Errorf("wrong URL: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Zoho-oauthtoken explicit-token" {
			t.Error("explicit credential did not override environment")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"id":9007199254740993123,"project_name":"Example","project_type":"Live","timezone":"Asia/Kolkata"}}`)), Header: make(http.Header)}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, schemas.Diagnostics)
	if schemas.DataSourceSchemas["catalyst_project"] == nil {
		t.Fatal("project data source not registered")
	}
	for _, attribute := range schemas.Provider.Block.Attributes {
		if attribute.Name == "access_token" || attribute.Name == "client_secret" || attribute.Name == "refresh_token" {
			if !attribute.Sensitive {
				t.Errorf("%s must be sensitive", attribute.Name)
			}
		}
	}
	config := jsonConfig(t, map[string]any{"access_token": "explicit-token", "data_center": "in"})
	validated, err := server.ValidateProviderConfig(t.Context(), &tfprotov6.ValidateProviderConfigRequest{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, validated.Diagnostics)
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{TerraformVersion: "1.16.5", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
	if calls != 0 {
		t.Fatal("configuration should not make an API request")
	}
	read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: "catalyst_project", Config: jsonConfig(t, map[string]any{"id": "9007199254740993123"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, read.Diagnostics)
	if read.State == nil {
		t.Fatal("missing data source state")
	}
	value, err := read.State.Unmarshal(schemas.DataSourceSchemas["catalyst_project"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]tftypes.Value
	if err := value.As(&state); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"id": "9007199254740993123", "name": "Example", "project_type": "Live", "timezone": "Asia/Kolkata"} {
		var got string
		if err := state[key].As(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: got %s, want %s", key, got, want)
		}
	}
	if !state["domain"].IsNull() || !state["domain_name"].IsNull() {
		t.Fatal("missing metadata should remain null")
	}
	if calls != 1 {
		t.Fatalf("unexpected request count: %d", calls)
	}
}

func TestProviderConfiguration(t *testing.T) {
	cases := []struct {
		name      string
		config    map[string]any
		env       map[string]string
		wantError bool
	}{
		{"missing", nil, nil, true},
		{"static", map[string]any{"access_token": "token"}, nil, false},
		{"environment", nil, map[string]string{"CATALYST_ACCESS_TOKEN": "token", "CATALYST_DATA_CENTER": "in"}, false},
		{"refresh", map[string]any{"client_id": "id", "client_secret": "secret", "refresh_token": "refresh"}, nil, false},
		{"partial-refresh", map[string]any{"client_id": "id"}, nil, true},
		{"conflict", map[string]any{"access_token": "token", "refresh_token": "refresh"}, nil, true},
		{"invalid-dc", map[string]any{"access_token": "token", "data_center": "bad"}, nil, true},
		{"explicit-empty", map[string]any{"access_token": ""}, map[string]string{"CATALYST_ACCESS_TOKEN": "token"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvironment(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			server := providerserver.NewProtocol6(New("test")())()
			values := tc.config
			if values == nil {
				values = map[string]any{}
			}
			result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{TerraformVersion: "1.16.5", Config: jsonConfig(t, values)})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
		})
	}
}

func TestUnknownConfigurationDoesNotUseEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "token")
	server := providerserver.NewProtocol6(New("test")())()
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]tftypes.Value{}
	objectType, ok := schemas.Provider.ValueType().(tftypes.Object)
	if !ok {
		t.Fatal("provider schema must be an object")
	}
	for _, attr := range schemas.Provider.Block.Attributes {
		fields[attr.Name] = tftypes.NewValue(objectType.AttributeTypes[attr.Name], nil)
	}
	fields["data_center"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	typ := schemas.Provider.ValueType()
	config, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, fields))
	if err != nil {
		t.Fatal(err)
	}
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(configured.Diagnostics) {
		t.Fatal("unknown data center must not fall back to environment")
	}
}

func TestProjectValidationAndReadErrors(t *testing.T) {
	clearEnvironment(t)
	calls := 0
	p := &CatalystProvider{version: "test", httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("secret-from-server")), Header: make(http.Header)}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "token"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
	for _, id := range []string{"../1", "", "0", "12.3"} {
		validated, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: "catalyst_project", Config: jsonConfig(t, map[string]any{"id": id})})
		if err != nil {
			t.Fatal(err)
		}
		if !hasError(validated.Diagnostics) {
			t.Errorf("invalid project ID accepted: %q", id)
		}
	}
	if calls != 0 {
		t.Fatal("validation must not call the API")
	}
	read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: "catalyst_project", Config: jsonConfig(t, map[string]any{"id": "123"})})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(read.Diagnostics) {
		t.Fatal("not-found project must produce an error")
	}
	for _, d := range read.Diagnostics {
		if strings.Contains(d.Detail, "secret-from-server") {
			t.Fatal("server response leaked into diagnostics")
		}
	}
}
