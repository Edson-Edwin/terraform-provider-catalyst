// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestAccGatewayProbe verifies the asynchronous ruleset contract without invoking routes.
func TestAccGatewayProbe(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_GATEWAY_CREATE") != "1" {
		t.Skip("requires explicit isolated Development Gateway write opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !ValidID(project) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development project")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.GetProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	path := "/baas/v1/project/" + project + "/api-gateway"
	call := func(ctx context.Context, method, route string, payload []byte, contentType string) (json.RawMessage, error) {
		res, err := c.requestContent(ctx, method, route, payload, contentType)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, apiResponseError("Gateway probe", res)
		}
		var result struct {
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if decode(res.Body, &result) != nil || result.Status != "success" || len(result.Data) == 0 || string(result.Data) == "null" {
			return nil, errors.New("invalid Gateway response")
		}
		return result.Data, nil
	}
	status := func(ctx context.Context) (bool, bool, error) {
		data, err := call(ctx, http.MethodGet, path, nil, "")
		if err != nil {
			return false, false, err
		}
		var state struct {
			Enabled   *bool `json:"status"`
			Scheduled *bool `json:"scheduled"`
		}
		if json.Unmarshal(data, &state) != nil || state.Enabled == nil || state.Scheduled == nil {
			return false, false, errors.New("incomplete Gateway status")
		}
		return *state.Enabled, *state.Scheduled, nil
	}
	enabled, scheduled, err := status(t.Context())
	if err != nil || enabled || scheduled {
		t.Fatal("test requires disabled idle Gateway baseline")
	}
	list := func(ctx context.Context) ([]json.RawMessage, error) {
		data, err := call(ctx, http.MethodGet, path+"/api?start=1&num_of_apis=200&enhance=true", nil, "")
		if err != nil {
			return nil, err
		}
		var rules []json.RawMessage
		if json.Unmarshal(data, &rules) != nil || rules == nil {
			return nil, errors.New("invalid Gateway rules")
		}
		return rules, nil
	}
	wait := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		for {
			_, busy, err := status(ctx)
			if err != nil {
				return err
			}
			if !busy {
				return nil
			}
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	deploy := func(ctx context.Context, rules []json.RawMessage) error {
		if err := wait(ctx); err != nil {
			return err
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("config_file", "rules.json")
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(rules)
		if err != nil {
			return err
		}
		if _, err = part.Write(encoded); err != nil {
			return err
		}
		if err = writer.Close(); err != nil {
			return err
		}
		if _, err = call(ctx, http.MethodPut, path+"/api", body.Bytes(), writer.FormDataContentType()); err != nil {
			return err
		}
		if err = wait(ctx); err != nil {
			return err
		}
		data, err := call(ctx, http.MethodGet, path+"/schedule?prev=true", nil, "")
		if err != nil {
			return err
		}
		var report struct {
			State                  Integer `json:"state"`
			Create, Update, Delete struct {
				Success Integer `json:"success_count"`
				Fail    Integer `json:"fail_count"`
			}
		}
		if json.Unmarshal(data, &report) != nil || report.State != 4 || report.Create.Fail+report.Update.Fail+report.Delete.Fail != 0 {
			return errors.New("Gateway ruleset job did not complete successfully")
		}
		return nil
	}
	name := "tfprobe" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// Register cleanup before enabling the implicit project-level configuration.
	fixtureDeployed := false
	var fnID string
	t.Cleanup(func() {
		if fnID == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Minute)
		defer cancel()
		if err := c.DeleteFunction(ctx, project, fnID); err != nil {
			t.Error(err)
			return
		}
		if err := c.WaitFunctionDeleted(ctx, project, fnID); err != nil {
			t.Error(err)
		} else {
			t.Log("Function fixture deletion confirmed")
		}
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
		defer cancel()
		if err := wait(ctx); err != nil {
			t.Error("Gateway cleanup could not wait for migration:", err)
			return
		}
		if fixtureDeployed {
			current, err := list(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			for _, raw := range current {
				var identity struct{ Name string }
				if json.Unmarshal(raw, &identity) != nil || (identity.Name != name && identity.Name != name+"updated") {
					t.Error("unexpected rule; refused destructive cleanup")
					return
				}
			}
			if len(current) > 0 {
				if err = deploy(ctx, []json.RawMessage{}); err != nil {
					t.Error("Gateway fixture removal:", err)
					return
				}
			}
			current, err = list(ctx)
			if err != nil || len(current) != 0 {
				t.Error("Gateway baseline rules not restored")
				return
			}
		}
		if _, err := call(ctx, http.MethodPut, path, []byte(`{"status":false}`), "application/json"); err != nil {
			t.Error(err)
			return
		}
		on, busy, err := status(ctx)
		if err != nil || on || busy {
			t.Error("Gateway disabled baseline not restored")
			return
		}
		t.Log("Gateway empty/disabled baseline restored; no route invoked")
	})
	if _, err = call(t.Context(), http.MethodPut, path, []byte(`{"status":true}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	baseline, err := list(t.Context())
	if err != nil || len(baseline) != 0 {
		t.Fatal("requires empty Gateway rules baseline")
	}
	// A generated Function is the only route target; never invoke it.
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for file, content := range map[string]string{"index.js": "module.exports = async (context, basicIO) => { basicIO.write('fixture'); context.close(); };", "package.json": fmt.Sprintf(`{"name":%q,"main":"index.js","version":"1.0.0"}`, name), "catalyst-config.json": fmt.Sprintf(`{"deployment":{"name":%q,"stack":"node22","type":"basicio"},"execution":{"main":"index.js"}}`, name)} {
		f, err := writer.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(f, content); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{Name: name, Stack: "node22", Type: "basicio"}}
	desired.Configuration.Memory = 128
	desired.Configuration.Environment.Variables = map[string]string{}
	fn, err := c.DeployFunction(t.Context(), project, "", desired, archive.Bytes())
	if fn != nil && ValidID(string(fn.ID)) {
		fnID = string(fn.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	rule := map[string]any{"name": name, "source_endpoint": "/" + name, "target_endpoint": "/server/" + name + "/execute", "target": "basicio", "target_id": string(fn.ID), "method": "GET", "throttling": map[string]any{"overall": map[string]any{}, "ip": map[string]any{}}}
	if os.Getenv("CATALYST_TEST_GATEWAY_AUTH_PROBE") == "1" {
		rule["authentication"] = []string{"OAuth"}
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	fixtureDeployed = true
	if err = deploy(t.Context(), []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	current, err := list(t.Context())
	if err != nil || len(current) != 1 {
		t.Fatal("Gateway create read-back failed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(current[0], &fields) != nil {
		t.Fatal("invalid created rule")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	t.Log("Created rule field names:", keys)
	for _, key := range []string{"authentication", "throttling", "target", "method", "status", "system"} {
		if value, ok := fields[key]; ok {
			t.Log("Non-secret fixture field", key, string(value))
		}
	}
	rule["name"] = name + "updated"
	rule["source_endpoint"] = "/" + name + "updated"
	raw, err = json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	if err = deploy(t.Context(), []json.RawMessage{raw}); err != nil {
		t.Fatal(err)
	}
	current, err = list(t.Context())
	if err != nil || len(current) != 1 {
		t.Fatal("Gateway replacement read-back failed")
	}
	var identity struct{ Name string }
	if json.Unmarshal(current[0], &identity) != nil || identity.Name != name+"updated" {
		t.Fatal("ruleset replacement did not remove old rule")
	}
	t.Log("Gateway create/read/replace verified; no route or Function invoked")
}
