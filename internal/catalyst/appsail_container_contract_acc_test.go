// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAccAppSailContainerContract(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" || os.Getenv("CATALYST_TEST_CONTAINER_ARCHIVE") == "" {
		t.Skip("requires isolated container fixture opt-ins")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !ValidID(project) || os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development project")
	}
	archive, err := os.ReadFile(os.Getenv("CATALYST_TEST_CONTAINER_ARCHIVE"))
	if err != nil || int64(len(archive)) > MaxFileBytes {
		t.Fatal("container fixture unavailable or too large")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfaccOCI" + strconv.FormatInt(time.Now().UnixNano(), 36)
	base := "/baas/v1/project/" + project + "/appsail"
	read := func(ctx context.Context, path string) (json.RawMessage, error) {
		r, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		var e struct {
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if r.StatusCode != 200 {
			return nil, apiResponseError("container contract read", r)
		}
		if err = decode(r.Body, &e); err != nil || e.Status != "success" {
			return nil, statusError("container contract read", r.StatusCode)
		}
		return e.Data, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Minute)
		defer cancel()
		raw, err := read(ctx, base)
		if err != nil {
			t.Error(err)
			return
		}
		var items []struct {
			ID   ID     `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &items) != nil {
			t.Error("container fixture cleanup listing invalid")
			return
		}
		for _, item := range items {
			if item.Name == name {
				if err = c.DeleteAppSail(ctx, project, string(item.ID)); err != nil {
					t.Error(err)
					return
				}
				if err = c.WaitAppSailDeleted(ctx, project, string(item.ID)); err != nil {
					t.Error(err)
					return
				}
			}
		}
		t.Log("Generated container AppSail cleanup confirmed")
	})
	raw, err := read(t.Context(), base+"/get-signature?"+url.Values{"name": {name}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		URL string `json:"signed_url"`
		Key string `json:"object_key"`
	}
	if json.Unmarshal(raw, &signed) != nil || signed.Key == "" {
		t.Fatal("container upload contract incomplete")
	}
	u, err := url.Parse(signed.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".zohostratus.in") {
		t.Fatal("container upload origin not an expected regional Stratus host")
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, signed.URL, bytes.NewReader(archive))
	if err != nil {
		t.Fatal("could not construct signed container upload")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	r, err := c.http.Do(req)
	if err != nil {
		t.Fatal("container transfer failed; signed URL withheld")
	}
	status := r.StatusCode
	_ = r.Body.Close()
	if status != 200 {
		t.Fatal(statusError("container upload", status))
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range map[string]string{"name": name, "memory": "2048", "platform": "custom_runtime", "local_object_key": signed.Key, "configuration": `{"startup_command":"/app","port":8080,"catalyst_auth":false,"login_redirect":"/","environment":{"variables":{"TF_FIXTURE":"one"}}}`} {
		if err = w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = c.requestContent(t.Context(), http.MethodPut, base+"/upsert", body.Bytes(), w.FormDataContentType())
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if r.StatusCode != 200 {
		err = apiResponseError("container callback", r)
		_ = r.Body.Close()
		t.Fatal(err)
	}
	err = decode(r.Body, &e)
	_ = r.Body.Close()
	if err != nil || e.Status != "success" {
		t.Fatal("invalid container callback")
	}
	var identity struct {
		ID       ID     `json:"id"`
		Name     string `json:"name"`
		Stack    string `json:"stack"`
		Platform string `json:"deployment_type"`
	}
	if json.Unmarshal(e.Data, &identity) != nil || !ValidID(string(identity.ID)) || identity.Name != name {
		t.Fatal("container identity not confirmed")
	}
	t.Log("Container metadata stack/platform:", identity.Stack, identity.Platform)
	raw, err = read(t.Context(), base+"/"+string(identity.ID))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("container metadata invalid")
	}
	keys := []string{}
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	t.Log("Container metadata field names:", keys)
	var config map[string]json.RawMessage
	if json.Unmarshal(fields["configuration"], &config) != nil {
		t.Fatal("container configuration missing")
	}
	keys = nil
	for k := range config {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	t.Log("Container configuration field names:", keys)
	// Custom deployments may still be processing when upsert returns. Give the
	// control plane a full minute to expose readable deployment configuration.
	readyCtx, cancel := context.WithTimeout(t.Context(), time.Minute)
	for len(config["startup_command"]) == 0 {
		if c.wait(readyCtx, 2*time.Second) != nil {
			break
		}
		latest, err := read(readyCtx, base+"/"+string(identity.ID))
		if err != nil {
			break
		}
		var next struct {
			Configuration map[string]json.RawMessage `json:"configuration"`
		}
		if json.Unmarshal(latest, &next) != nil {
			break
		}
		config = next.Configuration
	}
	cancel()
	keys = nil
	for k := range config {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	t.Log("Container configuration fields after readiness wait:", keys)
	r, err = c.get(t.Context(), base+"/"+string(identity.ID)+"/download")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(apiResponseError("container download", r))
	}
	download, err := io.ReadAll(io.LimitReader(r.Body, MaxFileBytes+1))
	if err != nil || int64(len(download)) > MaxFileBytes {
		t.Fatal("container download unavailable")
	}
	t.Log("Container artifact matches uploaded TAR:", sha256.Sum256(download) == sha256.Sum256(archive), "bytes:", len(download))
}

func TestAccAppSailContainerFixtureCleanup(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CREATE") != "1" || os.Getenv("CATALYST_TEST_CONTAINER_ARCHIVE") == "" {
		t.Skip("requires isolated container fixture opt-ins")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires India Development")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := os.Getenv("CATALYST_TEST_PROJECT_ID")
	items, err := c.ListAppSails(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name, "tfaccOCI") {
			if err = c.DeleteAppSail(t.Context(), p, string(item.ID)); err != nil {
				t.Fatal(err)
			}
			if err = c.WaitAppSailDeleted(t.Context(), p, string(item.ID)); err != nil {
				t.Fatal(err)
			}
		}
	}
	items, err = c.ListAppSails(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Name, "tfaccOCI") {
			t.Fatal("generated container fixture remains")
		}
	}
	t.Log("No generated OCI AppSail fixtures remain")
}
