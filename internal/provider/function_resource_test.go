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

func writeFunctionArchive(t *testing.T, directory, name, message string) string {
	t.Helper()
	return writeFunctionArchiveVariant(t, directory, name, message, "node22", "basicio")
}

func writeFunctionArchiveVariant(t *testing.T, directory, name, message, stack, kind string) string {
	t.Helper()
	var content bytes.Buffer
	w := zip.NewWriter(&content)
	config := fmt.Sprintf(`{"deployment":{"name":%q,"stack":%q,"type":%q},"execution":{"main":"index.js"}}`, name, stack, kind)
	pkg := fmt.Sprintf(`{"name":%q,"version":"1.0.0","main":"index.js"}`, name)
	code := "module.exports = async (context, basicIO) => { basicIO.write('" + message + "'); context.close(); };"
	if kind == "advancedio" {
		code = "module.exports = (req, res) => { res.setHeader('Content-Type', 'text/plain'); res.end('" + message + "'); };"
	}
	for _, entry := range []struct{ name, content string }{{"index.js", code}, {"catalyst-config.json", config}, {"package.json", pkg}} {
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
func functionConfig(project, name, source string, updated bool) string {
	env, mem := "{}", 128
	if updated {
		env = `{ TEST_VALUE = "fixture" }`
		mem = 256
	}
	return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_function" "test" {
 project_id = %q
 name = %q
 source = %q
 source_sha256 = filesha256(%q)
 memory = %d
 environment_variables = %s
}
`, project, name, source, source, mem, env)
}
func TestFunctionLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	dir := t.TempDir()
	first := writeFunctionArchive(t, dir, "test_function", "one")
	second := writeFunctionArchive(t, dir, "test_function", "two")
	var mu sync.Mutex
	var stored *catalyst.FunctionDeployment
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
		case req.URL.Path == "/baas/v1/project/123/function" && req.Method == "GET":
			items := []catalyst.FunctionMetadata{}
			if stored != nil {
				items = append(items, stored.FunctionMetadata)
			}
			response = map[string]any{"status": "success", "data": items}
		case req.URL.Path == "/baas/v1/project/123/function/upsert":
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
			stored = &catalyst.FunctionDeployment{FunctionMetadata: catalyst.FunctionMetadata{ID: "9007199254740993123", Name: req.FormValue("name"), Stack: req.FormValue("stack"), Type: req.FormValue("type")}}
			if err = json.Unmarshal([]byte(req.FormValue("configuration")), &stored.Configuration); err != nil {
				return nil, err
			}
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
			return nil, errors.New("unexpected Function request")
		}
		if raw == nil {
			raw, _ = json.Marshal(response)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})}}
	initial := functionConfig("123", "test_function", first, false)
	updated := functionConfig("123", "test_function", second, true)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)}, CheckDestroy: func(_ *terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if stored != nil {
			return errors.New("Function survived destroy")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function.test", "id", "9007199254740993123")},
		{ResourceName: "catalyst_function.test", ImportState: true, ImportStateId: "123/9007199254740993123", ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}},
		{Config: updated, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_function.test", "id", "9007199254740993123"), resource.TestCheckResourceAttr("catalyst_function.test", "memory", "256"), resource.TestCheckResourceAttr("catalyst_function.test", "environment_variables.TEST_VALUE", "fixture"))},
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_function.test", "environment_variables.%", "0")},
		{PreConfig: func() { mu.Lock(); archive = append(bytes.Clone(archive), []byte("remote drift")...); mu.Unlock() }, Config: initial},
		{PreConfig: func() { mu.Lock(); stored = nil; mu.Unlock() }, Config: initial},
		{Config: initial, PlanOnly: true},
	}})
	if writes != 5 || deletes != 1 {
		t.Fatalf("unexpected mutation counts %d %d", writes, deletes)
	}
}
func TestFunctionArchiveValidation(t *testing.T) {
	source := writeFunctionArchive(t, t.TempDir(), "example", "one")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateFunctionArchive(content, "example", "node22", "basicio"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		content           []byte
		name, stack, kind string
	}{{content, "other", "node22", "basicio"}, {content, "example", "node20", "basicio"}, {[]byte("invalid"), "example", "node22", "basicio"}} {
		if err = validateFunctionArchive(tc.content, tc.name, tc.stack, tc.kind); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
}

func TestFunctionCreateFailureRetainsIdentity(t *testing.T) {
	clearEnvironment(t)
	source := writeFunctionArchive(t, t.TempDir(), "example", "one")
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
	values := map[string]any{"project_id": "123", "name": "example", "stack": "node22", "type": "basicio", "memory": 128, "source": source, "source_sha256": hex.EncodeToString(digest[:]), "environment_variables": map[string]string{}}
	applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "catalyst_function", PriorState: jsonConfig(t, nil), PlannedState: jsonConfig(t, values), Config: jsonConfig(t, values)})
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
		t.Fatal("created identity was lost")
	}
}
func TestFunctionImportValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, id := range []string{"123", "123/0", "../789", "123/789/1", "123/789"} {
		result, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "catalyst_function", ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if hasError(result.Diagnostics) != (id != "123/789") {
			t.Fatalf("invalid import accepted: %s", id)
		}
	}
}
