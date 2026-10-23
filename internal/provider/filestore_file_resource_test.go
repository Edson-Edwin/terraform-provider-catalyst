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

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func fileConfig(project, folder, name, source string) string {
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_filestore_file" "test" {
 project_id = %q
 folder_id = %q
 name = %q
 source = %q
 source_sha256 = filesha256(%q)
}
`, project, folder, name, source, source)
}
func TestFilestoreFileLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	source := filepath.Join(t.TempDir(), "source.bin")
	original := []byte{0, 1, 255, 5, 3}
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var content []byte
	id, name := "", ""
	creates, deletes := 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		var body []byte
		metadata := func() []byte {
			b, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"id": id, "file_name": name, "file_size": len(content)}})
			return b
		}
		switch {
		case req.URL.Path == "/baas/v1/project/123":
			body = []byte(`{"status":"success","data":{"id":"123","project_name":"Test"}}`)
		case req.URL.Path == "/baas/v1/project/123/folder":
			body = []byte(`{"status":"success","data":[{"id":"456","folder_name":"Tests"}]}`)
		case req.Method == "POST":
			if err := req.ParseMultipartForm(1024); err != nil {
				return nil, err
			}
			defer func() { _ = req.MultipartForm.RemoveAll() }()
			f, _, err := req.FormFile("code")
			if err != nil {
				return nil, err
			}
			defer f.Close()
			content, err = io.ReadAll(f)
			if err != nil {
				return nil, err
			}
			creates++
			id = fmt.Sprint(9007199254740993000 + creates)
			name = req.FormValue("file_name")
			body = metadata()
		case id == "":
			status = 404
			body = []byte(`{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
		case req.Method == "DELETE":
			body = metadata()
			id = ""
			content = nil
			deletes++
		case strings.HasSuffix(req.URL.Path, "/download"):
			body = bytes.Clone(content)
		case req.Method == "GET":
			body = metadata()
		default:
			return nil, errors.New("unexpected file request")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	config := fileConfig("123", "456", "asset.bin", source)
	renamed := fileConfig("123", "456", "renamed.bin", source)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)},
		CheckDestroy: func(_ *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if id != "" {
				return errors.New("file survived destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "size", "5")},
			{ResourceName: "catalyst_filestore_file.test", ImportState: true, ImportStateId: "123/456/9007199254740993001", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}},
			{Config: config, PlanOnly: true},
			{Config: renamed, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "id", "9007199254740993002")},
			// Remote bytes changed without a size change: hashing must detect this drift.
			{PreConfig: func() { mu.Lock(); content = []byte{5, 4, 3, 2, 1}; mu.Unlock() }, Config: renamed, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "id", "9007199254740993003")},
			{PreConfig: func() { mu.Lock(); id = ""; content = nil; mu.Unlock() }, Config: renamed, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "id", "9007199254740993004")},
			{Config: renamed, PlanOnly: true},
		},
	})
	if creates != 4 || deletes != 3 {
		t.Fatalf("unexpected lifecycle calls: creates=%d deletes=%d", creates, deletes)
	}
}
func TestReadUploadSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	content := []byte{0, 255, 2}
	if err := os.WriteFile(source, content, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	got, err := readUploadSource(source, hex.EncodeToString(hash[:]))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("source changed: %v", err)
	}
	for _, tc := range []struct{ source, hash string }{{source, strings.Repeat("0", 64)}, {dir, ""}, {filepath.Join(dir, "absent"), ""}} {
		if _, err := readUploadSource(tc.source, tc.hash); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestFilestoreFileRecoveryState(t *testing.T) {
	for _, mode := range []string{"malformed_create", "corrupt_upload", "denied_delete", "denied_confirmation", "denied_read"} {
		t.Run(mode, func(t *testing.T) {
			clearEnvironment(t)
			source := filepath.Join(t.TempDir(), "asset")
			if err := os.WriteFile(source, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("fixture"))
			p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status, body := 403, "server-secret"
				if req.Method == "POST" {
					status = 201
					body = `{"status":"success","data":{"id":"789","file_size":{}}}`
					if mode == "corrupt_upload" {
						body = `{"status":"success","data":{"id":"789","file_name":"asset","file_size":7}}`
					}
				}
				if mode == "corrupt_upload" && req.Method == "GET" {
					status = 200
					body = "corrupt"
				}
				if mode == "denied_confirmation" && req.Method == "DELETE" {
					status = 200
					body = `{"status":"success","data":{"id":"789","file_name":"asset","file_size":7}}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			server := providerserver.NewProtocol6(p)()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			values := map[string]any{"project_id": "123", "folder_id": "456", "name": "asset", "source": source, "source_sha256": hex.EncodeToString(hash[:])}
			var newState *tfprotov6.DynamicValue
			var diagnostics []*tfprotov6.Diagnostic
			if mode == "denied_read" {
				values["id"] = "789"
				read, readErr := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "catalyst_filestore_file", CurrentState: jsonConfig(t, values)})
				if readErr != nil {
					t.Fatal(readErr)
				}
				newState, diagnostics = read.NewState, read.Diagnostics
			} else {
				prior, planned := jsonConfig(t, nil), jsonConfig(t, values)
				if strings.HasPrefix(mode, "denied") {
					values["id"] = "789"
					prior, planned = jsonConfig(t, values), jsonConfig(t, nil)
				}
				applied, applyErr := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_filestore_file", PriorState: prior, PlannedState: planned, Config: planned})
				if applyErr != nil {
					t.Fatal(applyErr)
				}
				newState, diagnostics = applied.NewState, applied.Diagnostics
			}
			if !hasError(diagnostics) || newState == nil {
				t.Fatal("expected diagnostic with retained recovery state")
			}
			for _, d := range diagnostics {
				if strings.Contains(d.Detail, "server-secret") {
					t.Fatal("response leaked")
				}
			}
			schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			state, err := newState.Unmarshal(schemas.ResourceSchemas["catalyst_filestore_file"].ValueType())
			if err != nil {
				t.Fatal(err)
			}
			var attrs map[string]tftypes.Value
			if state.IsNull() {
				t.Fatal("state erased on failure")
			}
			if err = state.As(&attrs); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = attrs["id"].As(&id); err != nil || id != "789" {
				t.Fatalf("lost file identity: %q %v", id, err)
			}
		})
	}
}
func TestFilestoreImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, id := range []string{"1", "1/2", "1/2/0", "1/2/3/4", "../2/3", "1/2/3"} {
		result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_filestore_file", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if hasError(result.Diagnostics) != (id != "1/2/3") {
			t.Fatalf("invalid import validation %q", id)
		}
	}
}
