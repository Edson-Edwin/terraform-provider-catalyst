// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func stratusObjectConfig(project, bucket, key, source, contentType string) string {
	return fmt.Sprintf(`provider "catalyst" { data_center = "in" }
resource "catalyst_stratus_object" "test" {
 project_id = %q
 bucket_name = %q
 key = %q
 source = %q
 source_sha256 = filesha256(%q)
 content_type = %q
}
`, project, bucket, key, source, source, contentType)
}
func TestStratusObjectLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	source := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(source, []byte{0, 255, 1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var content []byte
	contentType := ""
	exists := false
	creates, updates, deletes := 0, 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		body := ""
		if req.URL.Hostname() == "test-bucket-development.zohostratus.in" {
			if req.Header.Get("Authorization") != "" {
				t.Error("OAuth forwarded to data host")
			}
			if req.Method == "PUT" {
				if exists && req.Header.Get("overwrite") == "false" {
					status = 409
				} else {
					if exists {
						updates++
					} else {
						creates++
					}
					exists = true
					content, _ = io.ReadAll(req.Body)
					contentType = req.Header.Get("Content-Type")
				}
			} else {
				if !exists {
					status = 404
				} else {
					body = string(content)
				}
			}
		} else {
			switch {
			case strings.HasSuffix(req.URL.Path, "/signature"):
				body = fmt.Sprintf(`{"status":"success","data":{"signature":"token=secret","expiry_time":%d}}`, time.Now().Add(time.Hour).UnixMilli())
			case strings.HasSuffix(req.URL.Path, "/objects"):
				objects := []map[string]string{}
				if exists {
					objects = append(objects, map[string]string{"key": "nested/binary ?#.bin"})
				}
				b, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"contents": objects, "truncated": false}})
				body = string(b)
			case strings.HasSuffix(req.URL.Path, "/object"):
				if req.Method == "PUT" {
					exists = false
					deletes++
					body = `{"status":"success","data":{}}`
				} else if !exists {
					status = 404
				} else {
					hash := sha256.Sum256(content)
					b, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"key": "nested/binary ?#.bin", "size": len(content), "content_type": contentType, "etag": hex.EncodeToString(hash[:])}})
					body = string(b)
				}
			default:
				body = `{"status":"success","data":[{"bucket_name":"test-bucket","bucket_url":"https://test-bucket-development.zohostratus.in","bucket_meta":{"versioning":false}}]}`
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	config := stratusObjectConfig("123", "test-bucket", "nested/binary ?#.bin", source, "application/octet-stream")
	updated := stratusObjectConfig("123", "test-bucket", "nested/binary ?#.bin", source, "text/plain")
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if exists {
			return errors.New("object survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config, Check: resource.TestCheckResourceAttr("catalyst_stratus_object.test", "size", "5")},
		{ResourceName: "catalyst_stratus_object.test", ImportState: true, ImportStateId: "123/test-bucket/nested/binary ?#.bin", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}},
		{Config: config, PlanOnly: true},
		{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_stratus_object.test", "content_type", "text/plain")},
		{PreConfig: func() { mu.Lock(); content = bytes.Repeat([]byte{7}, 5); mu.Unlock() }, Config: updated},
		{PreConfig: func() { mu.Lock(); exists = false; mu.Unlock() }, Config: updated},
		{Config: updated, PlanOnly: true},
	}})
	if creates != 2 || updates != 2 || deletes != 1 {
		t.Fatalf("unexpected lifecycle: creates=%d updates=%d deletes=%d", creates, updates, deletes)
	}
}
func TestStratusObjectFailureRetainsState(t *testing.T) {
	for _, mode := range []string{"accepted_then_denied", "denied_read", "denied_delete", "denied_confirmation"} {
		t.Run(mode, func(t *testing.T) {
			clearEnvironment(t)
			source := filepath.Join(t.TempDir(), "asset")
			content := []byte("bytes")
			if err := os.WriteFile(source, content, 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(content)
			accepted := false
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status, body := 403, "server-secret"
				switch {
				case strings.HasSuffix(req.URL.Path, "/bucket"):
					status = 200
					body = `{"status":"success","data":[{"bucket_name":"test-bucket","bucket_url":"https://test-bucket-development.zohostratus.in","bucket_meta":{"versioning":false}}]}`
				case strings.HasSuffix(req.URL.Path, "/objects"):
					status = 200
					body = `{"status":"success","data":{"contents":[],"truncated":false}}`
				case strings.HasSuffix(req.URL.Path, "/signature"):
					status = 200
					body = fmt.Sprintf(`{"status":"success","data":{"signature":"token=signature-secret","expiry_time":%d}}`, time.Now().Add(time.Hour).UnixMilli())
				case req.URL.Hostname() == "test-bucket-development.zohostratus.in" && req.Method == "PUT":
					accepted = true
					status = 200
					body = ""
				case mode == "accepted_then_denied" && !accepted:
					status = 404
					body = ""
				case mode == "denied_confirmation" && req.Method == "PUT":
					status = 200
					body = `{"status":"success","data":{}}`
					accepted = true
				case (mode == "denied_delete" || mode == "denied_confirmation") && req.Method == "GET" && !accepted:
					status = 200
					body = `{"status":"success","data":{"key":"asset","size":5,"etag":"opaque","content_type":"application/octet-stream"}}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test", "data_center": "in"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			values := map[string]any{"project_id": "123", "bucket_name": "test-bucket", "key": "asset", "source": source, "source_sha256": hex.EncodeToString(hash[:]), "content_type": "application/octet-stream"}
			var state *tfprotov6.DynamicValue
			var diagnostics []*tfprotov6.Diagnostic
			if mode == "denied_read" {
				values["id"] = "123/test-bucket/asset"
				read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_stratus_object", CurrentState: jsonConfig(t, values)})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = read.NewState, read.Diagnostics
			} else {
				prior, planned := jsonConfig(t, nil), jsonConfig(t, values)
				if strings.HasPrefix(mode, "denied") {
					values["id"] = "123/test-bucket/asset"
					prior, planned = jsonConfig(t, values), jsonConfig(t, nil)
				}
				applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_stratus_object", PriorState: prior, PlannedState: planned, Config: planned})
				if err != nil {
					t.Fatal(err)
				}
				state, diagnostics = applied.NewState, applied.Diagnostics
			}
			if !hasError(diagnostics) || state == nil {
				t.Fatal("expected failure with recovery state")
			}
			for _, d := range diagnostics {
				if strings.Contains(d.Detail, "server-secret") || strings.Contains(d.Detail, "signature-secret") {
					t.Fatal("secret leaked")
				}
			}
			schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := state.Unmarshal(schemas.ResourceSchemas["catalyst_stratus_object"].ValueType())
			if err != nil || decoded.IsNull() {
				t.Fatal("state lost on failure")
			}
			attrs := map[string]tftypes.Value{}
			if err = decoded.As(&attrs); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = attrs["id"].As(&id); err != nil || id != "123/test-bucket/asset" {
				t.Fatal("identity lost on failure")
			}
		})
	}
}
func TestStratusObjectImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, id := range []string{"1/bucket/key", "1/bucket/nested/key", "0/bucket/key", "1/bucket/../bad", "1/UPPER/key", "1/bucket/", "1/bucket"} {
		result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_stratus_object", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		valid := id == "1/bucket/key" || id == "1/bucket/nested/key"
		if hasError(result.Diagnostics) == valid {
			t.Fatalf("unexpected validation for %s", id)
		}
	}
}
