// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
)

func TestFunctionFixtureRecoveryOwnership(t *testing.T) {
	for _, scenario := range []string{"success", "baseline_id", "renamed", "different_type", "denied", "invalid_id"} {
		t.Run(scenario, func(t *testing.T) {
			requests, deletes := 0, 0
			deleted := false
			client, err := catalyst.New(catalyst.Config{AccessToken: "test"}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.Method == "DELETE" {
					deletes++
					deleted = true
				}
				name, kind, status := "tfaccfixture", "cron", 200
				if scenario == "renamed" {
					name = "application"
				}
				if scenario == "different_type" {
					kind = "job"
				}
				if scenario == "denied" {
					status = 403
				}
				body := fmt.Sprintf(`{"status":"success","data":{"id":"789","name":%q,"stack":"node24","type":%q}}`, name, kind)
				if req.Method == "DELETE" {
					body = `{"status":"success","data":{"id":"789"}}`
				} else if deleted {
					body = `{"status":"success","data":null}`
					if req.URL.Path == "/baas/v1/project/456" {
						body = `{"status":"success","data":{"id":"456","project_name":"Test"}}`
					}
					if strings.HasSuffix(req.URL.Path, "/function") {
						body = `{"status":"success","data":[]}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			var baseline []catalyst.FunctionMetadata
			if scenario == "baseline_id" {
				baseline = []catalyst.FunctionMetadata{{ID: "789"}}
			}
			id := "789"
			if scenario == "invalid_id" {
				id = ""
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err = cleanupKnownFunctionFixture(ctx, client, "456", id, "tfaccfixture", "cron", baseline)
			if scenario == "success" {
				if err != nil || deletes != 1 {
					t.Fatalf("exact fixture cleanup failed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if deletes != 0 {
				t.Fatal("foreign or inaccessible fixture deleted")
			}
			if (scenario == "baseline_id" || scenario == "invalid_id") && requests != 0 {
				t.Fatal("invalid recovery made requests")
			}
		})
	}
}
