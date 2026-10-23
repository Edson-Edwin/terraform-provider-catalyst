// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestProviderRegionalPolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    map[string]any
		env       map[string]string
		wantError bool
	}{
		{"allowed", map[string]any{"data_center": "in", "environment": "Production", "allowed_data_centers": []string{"in"}, "allowed_environments": []string{"Production"}}, nil, false},
		{"reject-region", map[string]any{"data_center": "us", "allowed_data_centers": []string{"in"}}, nil, true},
		{"reject-environment", map[string]any{"data_center": "in", "environment": "Production", "allowed_environments": []string{"Development"}}, nil, true},
		{"reject-default-region", map[string]any{"allowed_data_centers": []string{"us"}}, nil, true},
		{"reject-default-environment", map[string]any{"allowed_environments": []string{"Development"}}, nil, true},
		{"environment-fallback", nil, map[string]string{"CATALYST_DATA_CENTER": "in", "CATALYST_ENVIRONMENT": "Production", "CATALYST_ALLOWED_DATA_CENTERS": "in, eu", "CATALYST_ALLOWED_ENVIRONMENTS": "Production"}, false},
		{"explicit-overrides-environment", map[string]any{"data_center": "in", "allowed_data_centers": []string{"in"}}, map[string]string{"CATALYST_ALLOWED_DATA_CENTERS": "us"}, false},
		{"empty-suppresses-environment", map[string]any{"allowed_data_centers": []string{}, "allowed_environments": []string{}}, map[string]string{"CATALYST_ALLOWED_DATA_CENTERS": "in", "CATALYST_ALLOWED_ENVIRONMENTS": "Production"}, false},
		{"null-region-element", map[string]any{"data_center": "in", "allowed_data_centers": []any{nil}}, nil, true},
		{"null-environment-element", map[string]any{"environment": "Development", "allowed_environments": []any{nil}}, nil, true},
		{"invalid-region-environment-list", nil, map[string]string{"CATALYST_DATA_CENTER": "in", "CATALYST_ALLOWED_DATA_CENTERS": "in,"}, true},
		{"guard-before-preflight", map[string]any{"data_center": "in", "allowed_data_centers": []string{"eu"}, "validation_project_id": "123"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvironment(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			config := tc.config
			if config == nil {
				config = map[string]any{}
			}
			config["access_token"] = "secret-token"
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("regional policy contacted the network")
				return nil, fmt.Errorf("unexpected request")
			})}}
			server := providerserver.NewProtocol6(p)()
			result, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, config)})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if strings.Contains(d.Detail, "secret-token") {
					t.Fatal("regional diagnostic exposed credentials")
				}
			}
		})
	}
}
