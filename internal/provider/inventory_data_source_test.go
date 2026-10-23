// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var inventoryFixtures = map[string]struct{ collection, endpoint, body string }{
	"functions":         {"functions", "function", `{"id":9007199254740993123,"name":"Example","stack":"node22","type":"basicio"}`},
	"datastore_tables":  {"tables", "table", `{"table_id":9007199254740993123,"table_name":"Example"}`},
	"filestore_folders": {"folders", "folder", `{"id":9007199254740993123,"folder_name":"Example"}`},
	"cache_segments":    {"segments", "segment", `{"id":9007199254740993123,"segment_name":"Example"}`},
	"stratus_buckets":   {"buckets", "bucket", `{"bucket_name":"Example","bucket_url":"https://example.zohostratus.in"}`},
}

// Real Terraform validates list state, precision, refresh and stable plans.
func TestInventoryTerraform(t *testing.T) {
	for kind, fixture := range inventoryFixtures {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", kind, empty), func(t *testing.T) {
				clearEnvironment(t)
				t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
				content := fixture.body
				count := "1"
				if empty {
					content = ""
					count = "0"
				}
				p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method != "GET" || r.URL.Path != "/baas/v1/project/123/"+fixture.endpoint {
						return nil, fmt.Errorf("unexpected inventory request")
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"success","data":[` + content + `]}`)), Header: make(http.Header)}, nil
				})}}
				address := "data.catalyst_" + kind + ".test"
				config := fmt.Sprintf("provider \"catalyst\" {}\ndata \"catalyst_%s\" \"test\" { project_id = \"123\" }", kind)
				checks := []resource.TestCheckFunc{resource.TestCheckResourceAttr(address, fixture.collection+".#", count)}
				if !empty {
					checks = append(checks, resource.TestCheckResourceAttr(address, fixture.collection+".0.name", "Example"))
					if kind != "stratus_buckets" {
						checks = append(checks, resource.TestCheckResourceAttr(address, fixture.collection+".0.id", "9007199254740993123"))
					}
				}
				resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, Steps: []resource.TestStep{
					{Config: config, Check: resource.ComposeTestCheckFunc(checks...)},
					{Config: config, PlanOnly: true},
				}})
			})
		}
	}
}
func TestInventoryProtocolErrors(t *testing.T) {
	for kind := range inventoryFixtures {
		t.Run(kind, func(t *testing.T) {
			clearEnvironment(t)
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("server-secret")), Header: make(http.Header)}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			name := "catalyst_" + kind
			for _, id := range []string{"", "0", "../1", "123"} {
				validated, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: name, Config: jsonConfig(t, map[string]any{"project_id": id})})
				if err != nil {
					t.Fatal(err)
				}
				if hasError(validated.Diagnostics) != (id != "123") {
					t.Fatalf("unexpected validation for %q", id)
				}
			}
			read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: name, Config: jsonConfig(t, map[string]any{"project_id": "123"})})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(read.Diagnostics) {
				t.Fatal("permission denial must fail")
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail, "server-secret") {
					t.Fatal("response leaked")
				}
			}
		})
	}
}
