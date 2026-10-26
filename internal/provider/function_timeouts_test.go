// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestFunctionTimeoutConfiguration(t *testing.T) {
	fields := map[string]attr.Type{"read": types.StringType}
	for _, value := range []types.String{types.StringValue("10m"), types.StringNull(), types.StringValue("0s"), types.StringValue("-1m"), types.StringValue("invalid"), types.StringUnknown()} {
		var diagnostics diag.Diagnostics
		timeout := types.ObjectValueMust(fields, map[string]attr.Value{"read": value})
		ctx, cancel := operationContext(t.Context(), timeout, "read", 2*time.Minute, "Function", &diagnostics)
		invalid := value.IsUnknown() || (!value.IsNull() && value.ValueString() != "10m")
		if diagnostics.HasError() != invalid {
			t.Fatalf("unexpected diagnostics for %v: %v", value, diagnostics)
		}
		if !invalid {
			deadline, ok := ctx.Deadline()
			want := 2 * time.Minute
			if !value.IsNull() {
				want = 10 * time.Minute
			}
			if !ok || time.Until(deadline) > want || time.Until(deadline) < want-time.Second {
				t.Fatal("configured timeout ignored")
			}
		}
		cancel()
	}
	parent, stop := context.WithCancel(t.Context())
	stop()
	var diagnostics diag.Diagnostics
	ctx, cancel := operationContext(parent, types.ObjectNull(fields), "read", time.Minute, "Function", &diagnostics)
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("parent cancellation ignored")
	}
}

func TestFunctionReadTimeoutRetainsState(t *testing.T) {
	clearEnvironment(t)
	calls := 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}}
	server := providerserver.NewProtocol6(p)()
	configureScopeTest(t, server)
	values := map[string]any{"id": "789", "project_id": "456", "name": "example", "stack": "node22", "type": "basicio", "memory": 128, "source": "unused.zip", "source_sha256": "unused", "environment_variables": map[string]string{}, "timeouts": map[string]any{"read": "20ms"}}
	prior := boundResourceState(t, values)
	result, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_function", CurrentState: prior})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(result.Diagnostics) || calls != 1 {
		t.Fatalf("timeout failed: calls=%d diagnostics=%v", calls, result.Diagnostics)
	}
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := prior.Unmarshal(schemas.ResourceSchemas["catalyst_function"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	after, err := result.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_function"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) {
		t.Fatal("timeout changed recoverable Function state")
	}
}

func TestFunctionCreateTimeoutRetainsIdentity(t *testing.T) {
	clearEnvironment(t)
	source := writeFunctionArchive(t, t.TempDir(), "example", "fixture")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	writes := 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":"success","data":[]}`
		if req.Method == "PUT" {
			writes++
			body = `{"status":"success","data":{"id":"789","name":"example","stack":"node22","type":"basicio","configuration":{"memory":128,"environment":{"variables":{}}}}}`
		} else if strings.HasSuffix(req.URL.Path, "/789") {
			// Deployment exists but never reaches the requested configuration before timeout.
			body = `{"status":"success","data":{"id":"789","name":"example","stack":"node22","type":"basicio","configuration":{"memory":256,"environment":{"variables":{}}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	configureScopeTest(t, server)
	values := map[string]any{"project_id": "456", "name": "example", "stack": "node22", "type": "basicio", "memory": 128, "source": source, "source_sha256": hex.EncodeToString(hash[:]), "environment_variables": map[string]string{}, "timeouts": map[string]any{"create": "50ms"}}
	applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_function", PriorState: jsonConfig(t, nil), PlannedState: boundResourceState(t, values), Config: jsonConfig(t, values)})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(applied.Diagnostics) || writes != 1 {
		t.Fatalf("expected one write and timeout: %v", applied.Diagnostics)
	}
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := applied.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_function"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err = state.As(&attrs); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = attrs["id"].As(&id); err != nil || id != "789" {
		t.Fatal("timeout lost created ID")
	}
	if attrs["routing_scope"].IsNull() || !attrs["routing_scope"].IsKnown() {
		t.Fatal("timeout lost routing scope")
	}
}
