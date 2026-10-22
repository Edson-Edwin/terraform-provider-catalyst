// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestDiscoveryProtocol(t *testing.T) {
	for _, kind := range []string{"projects", "job_pools"} {
		for _, empty := range []bool{false, true} {
			name := kind
			if empty {
				name += "_empty"
			}
			t.Run(name, func(t *testing.T) {
				clearEnvironment(t)
				body := `{"status":"success","data":[{"id":9007199254740993123,"project_name":"Project","name":"Pool","type":"Webhook"}]}`
				if empty {
					body = `{"status":"success","data":[]}`
				}
				calls := 0
				p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})}}
				server := providerserver.NewProtocol6(p)()
				configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(t, configured.Diagnostics)
				schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(t, schemas.Diagnostics)
				typeName := "catalyst_" + kind
				config := map[string]any{}
				listAttr := "projects"
				if kind == "job_pools" {
					config["project_id"] = "456"
					listAttr = "pools"
				}
				validated, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: typeName, Config: jsonConfig(t, config)})
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(t, validated.Diagnostics)
				read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: typeName, Config: jsonConfig(t, config)})
				if err != nil {
					t.Fatal(err)
				}
				checkDiagnostics(t, read.Diagnostics)
				if read.State == nil {
					t.Fatal("missing state")
				}
				value, err := read.State.Unmarshal(schemas.DataSourceSchemas[typeName].ValueType())
				if err != nil {
					t.Fatal(err)
				}
				var attrs map[string]tftypes.Value
				if err := value.As(&attrs); err != nil {
					t.Fatal(err)
				}
				if attrs[listAttr].IsNull() || !attrs[listAttr].IsKnown() {
					t.Fatal("list must be known and non-null")
				}
				var items []tftypes.Value
				if err := attrs[listAttr].As(&items); err != nil {
					t.Fatal(err)
				}
				expected := 1
				if empty {
					expected = 0
				}
				if len(items) != expected {
					t.Fatalf("got %d items", len(items))
				}
				if !empty {
					var item map[string]tftypes.Value
					if err := items[0].As(&item); err != nil {
						t.Fatal(err)
					}
					var id string
					if err := item["id"].As(&id); err != nil {
						t.Fatal(err)
					}
					if id != "9007199254740993123" {
						t.Fatal("ID lost precision")
					}
					if kind == "projects" && !item["timezone"].IsNull() {
						t.Fatal("missing metadata must be null")
					}
				}
				if calls != 1 {
					t.Fatalf("expected one read, got %d", calls)
				}
			})
		}
	}
}

func TestJobPoolsProtocolValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, id := range []string{"", "0", "../123", "123"} {
		validated, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: "catalyst_job_pools", Config: jsonConfig(t, map[string]any{"project_id": id})})
		if err != nil {
			t.Fatal(err)
		}
		if hasError(validated.Diagnostics) != (id != "123") {
			t.Fatalf("unexpected validation for %q: %v", id, validated.Diagnostics)
		}
	}
}

func TestDiscoveryProtocolReadErrors(t *testing.T) {
	for _, kind := range []string{"projects", "job_pools"} {
		t.Run(kind, func(t *testing.T) {
			clearEnvironment(t)
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("secret-from-server")), Header: make(http.Header)}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			config := map[string]any{}
			if kind == "job_pools" {
				config["project_id"] = "456"
			}
			read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: "catalyst_" + kind, Config: jsonConfig(t, config)})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(read.Diagnostics) {
				t.Fatal("access denied must not return an empty successful list")
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail, "secret-from-server") {
					t.Fatal("response leaked into diagnostics")
				}
			}
		})
	}
}
