// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"archive/zip"
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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func writeAppSailArchive(t *testing.T, directory, name, message string) string {
	t.Helper()
	var content bytes.Buffer
	w := zip.NewWriter(&content)
	pkg := fmt.Sprintf(`{"name":%q,"version":"1.0.0","main":"index.js"}`, name)
	for _, entry := range []struct{ name, content string }{{"index.js", "require('http').createServer((q,s)=>s.end('" + message + "')).listen(process.env.X_ZOHO_CATALYST_LISTEN_PORT || 9000);"}, {"package.json", pkg}} {
		f, err := w.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, name+"-"+message+".zip")
	if err := os.WriteFile(source, content.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return source
}
func appsailConfig(project, name, source string, updated bool) string {
	env, mem := "{}", 128
	command, port := "node index.js", 9000
	if updated {
		env = `{ TEST_VALUE = "fixture" }`
		mem = 256
		command, port = "node --no-warnings index.js", 9001
	}
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_appsail" "test" {
 project_id = %q
 name = %q
 source = %q
 source_sha256 = filesha256(%q)
 memory = %d
startup_command = %q
port = %d
catalyst_auth = true
 environment_variables = %s
}
`, project, name, source, source, mem, command, port, env)
}
func TestAppSailLifecycle(t *testing.T)       { testAppSailLifecycle(t, "node22") }
func TestAppSailNode24Lifecycle(t *testing.T) { testAppSailLifecycle(t, "node24") }
func testAppSailLifecycle(t *testing.T, stack string) {
	t.Helper()
	clearEnvironment(t)
	t.Setenv("CATALYST_ORGANIZATION_ID", "123")
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	dir := t.TempDir()
	first := writeAppSailArchive(t, dir, "testappsail", "one")
	second := writeAppSailArchive(t, dir, "testappsail", "two")
	var mu sync.Mutex
	var stored *catalyst.AppSailDeployment
	var archive []byte
	writes, deletes := 0, 0
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		status := 200
		var response any
		var raw []byte
		switch {
		case req.URL.Path == "/baas/v1/project/123":
			response = map[string]any{"status": "success", "data": map[string]any{"id": "123", "project_name": "Test"}}
		case req.URL.Path == "/baas/v1/project/123/appsail" && req.Method == "GET":
			items := []catalyst.AppSailMetadata{}
			if stored != nil {
				items = append(items, stored.AppSailMetadata)
			}
			response = map[string]any{"status": "success", "data": items}
		case req.URL.Path == "/baas/v1/project/123/appsail/upsert":
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				return nil, err
			}
			defer func() { _ = req.MultipartForm.RemoveAll() }()
			file, _, err := req.FormFile("code")
			if err != nil {
				return nil, err
			}
			defer file.Close()
			archive, err = io.ReadAll(file)
			if err != nil {
				return nil, err
			}
			stored = &catalyst.AppSailDeployment{AppSailMetadata: catalyst.AppSailMetadata{ID: "9007199254740993123", Name: req.FormValue("name"), Stack: req.FormValue("stack")}}
			if err = json.Unmarshal([]byte(req.FormValue("configuration")), &stored.Configuration); err != nil {
				return nil, err
			}
			mem, err := strconv.ParseInt(req.FormValue("memory"), 10, 64)
			if err != nil {
				return nil, err
			}
			stored.Configuration.Memory = catalyst.Integer(mem)
			writes++
			response = map[string]any{"status": "success", "data": stored}
		case stored == nil:
			response = map[string]any{"status": "success", "data": nil}
		case req.Method == "DELETE":
			response = map[string]any{"status": "success", "data": stored}
			stored = nil
			deletes++
		case strings.HasSuffix(req.URL.Path, "/download"):
			raw = bytes.Clone(archive)
		case req.Method == "GET":
			response = map[string]any{"status": "success", "data": stored}
		default:
			return nil, errors.New("unexpected AppSail request")
		}
		if raw == nil {
			raw, _ = json.Marshal(response)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}}
	initial := strings.Replace(appsailConfig("123", "testappsail", first, false), `project_id = "123"`, `stack = "`+stack+`"`+"\n"+`project_id = "123"`, 1)
	updated := strings.Replace(appsailConfig("123", "testappsail", second, true), `project_id = "123"`, `stack = "`+stack+`"`+"\n"+`project_id = "123"`, 1)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if stored != nil {
			return errors.New("AppSail survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail.test", "id", "9007199254740993123"), resource.TestCheckResourceAttr("catalyst_appsail.test", "stack", stack))},
		{ResourceName: "catalyst_appsail.test", ImportState: true, ImportStateId: "123/9007199254740993123", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_appsail.test", "id", "9007199254740993123"), resource.TestCheckResourceAttr("catalyst_appsail.test", "memory", "256"), resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.TEST_VALUE", "fixture"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_appsail.test", "environment_variables.%", "0")},
		{PreConfig: func() { mu.Lock(); archive = append(bytes.Clone(archive), []byte("remote drift")...); mu.Unlock() }, Config: initial},
		{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: initial},
		{Config: initial, PlanOnly: true},
	}})
	if writes != 5 || deletes != 1 {
		t.Fatalf("unexpected mutation counts %d %d", writes, deletes)
	}
}
func TestAppSailArchiveValidation(t *testing.T) {
	source := writeAppSailArchive(t, t.TempDir(), "example", "one")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAppSailArchive(content); err != nil {
		t.Fatal(err)
	}
	if err = validateAppSailArchive([]byte("invalid")); err == nil {
		t.Fatal("invalid archive accepted")
	}

}

func TestAppSailCreateFailureRetainsIdentity(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ORGANIZATION_ID", "123")
	source := writeAppSailArchive(t, t.TempDir(), "example", "one")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":"success","data":[]}`
		if req.Method == "PUT" {
			body = `{"status":"success","data":{"id":"789","configuration":{"memory":{}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	server := providerserver.NewProtocol6(p)()
	configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: jsonConfig(t, map[string]any{"access_token": "test"})})
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnostics(t, configured.Diagnostics)
	values := map[string]any{"project_id": "123", "name": "example", "stack": "node22", "startup_command": "node index.js", "port": 9000, "catalyst_auth": true, "memory": 128, "source": source, "source_sha256": hex.EncodeToString(digest[:]), "environment_variables": map[string]string{}}
	applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_appsail", PriorState: jsonConfig(t, nil), PlannedState: boundResourceState(t, values), Config: jsonConfig(t, values)})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(applied.Diagnostics) || applied.NewState == nil {
		t.Fatal("expected error with recovery state")
	}
	schemas, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := applied.NewState.Unmarshal(schemas.ResourceSchemas["catalyst_appsail"].ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err = state.As(&attrs); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = attrs["id"].As(&id); err != nil || id != "789" {
		t.Fatal("created identity was lost")
	}
	if !attrs["url"].IsKnown() {
		t.Fatal("recovery URL was left unknown")
	}
}
func TestAppSailImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	configureScopeTest(t, server)
	for _, id := range []string{"123", "123/0", "../789", "123/789/1", "123/789"} {
		result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_appsail", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if hasError(result.Diagnostics) != (id != "123/789") {
			t.Fatalf("invalid import accepted: %s", id)
		}
	}
}

func TestDeploymentArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, entry := range []string{"../escape", "dir/../../escape", "/absolute", "dir/../alias", "dir//file", `dir\file`, "duplicate"} {
		t.Run(entry, func(t *testing.T) {
			var content bytes.Buffer
			writer := zip.NewWriter(&content)
			f, err := writer.Create(entry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write([]byte("fixture")); err != nil {
				t.Fatal(err)
			}
			if entry == "duplicate" {
				if _, err = writer.Create(entry); err != nil {
					t.Fatal(err)
				}
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = deploymentArchiveFiles(content.Bytes()); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
}
