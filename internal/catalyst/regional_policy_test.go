// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegionalPolicyFailsBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    Config
		wantError bool
	}{
		{"allowed", Config{DataCenter: "in", Environment: "Production", AllowedDataCenters: []string{"in", "eu"}, AllowedEnvironments: []string{"Production"}}, false},
		{"wrong-region", Config{DataCenter: "us", AllowedDataCenters: []string{"in"}}, true},
		{"default-region", Config{AllowedDataCenters: []string{"us"}}, true},
		{"invalid-allowed-region", Config{DataCenter: "in", AllowedDataCenters: []string{"secret-invalid"}}, true},
		{"empty-region-policy-value", Config{DataCenter: "in", AllowedDataCenters: []string{""}}, true},
		{"wrong-environment", Config{DataCenter: "in", Environment: "Production", AllowedEnvironments: []string{"Development"}}, true},
		{"default-environment", Config{DataCenter: "in", AllowedEnvironments: []string{"Development"}}, true},
		{"invalid-allowed-environment", Config{DataCenter: "in", Environment: "Development", AllowedEnvironments: []string{"secret-invalid"}}, true},
		{"empty-policy", Config{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := tc.config
			config.AccessToken = "secret"
			_, err := New(config, &http.Client{Transport: retryTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("local policy check contacted the network")
				return nil, nil
			})})
			if (err != nil) != tc.wantError || (err != nil && strings.Contains(err.Error(), "secret")) {
				t.Fatalf("invalid regional policy result: %v", err)
			}
		})
	}
}
