// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAccStratusObjectProbe(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE") != "1" {
		t.Skip("requires explicit isolated Stratus object write opt-in")
	}
	project, bucket := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_BUCKET")
	if !ValidID(project) || bucket == "" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires isolated India Development bucket")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/baas/v1/project/" + project
	bucketQuery := url.Values{"bucket_name": {bucket}}
	raw, err := getList[json.RawMessage](t.Context(), c, path+"/bucket?"+bucketQuery.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatal("test bucket identity is not unique")
	}
	var details struct {
		Name string `json:"bucket_name"`
		URL  string `json:"bucket_url"`
		Meta struct {
			Versioning *bool `json:"versioning"`
		} `json:"bucket_meta"`
	}
	if json.Unmarshal(raw[0], &details) != nil || details.Name != bucket || details.Meta.Versioning == nil || *details.Meta.Versioning {
		t.Fatal("requires verified unversioned test bucket")
	}
	if details.URL != "https://"+bucket+"-development.zohostratus.in" {
		t.Fatal("unexpected Stratus transfer origin")
	}
	t.Log("Verified authenticated test bucket identity and unversioned metadata")
	res, err := c.request(t.Context(), http.MethodPost, path+"/bucket/signature?"+bucketQuery.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		err = apiResponseError("Stratus signature", res)
		_ = res.Body.Close()
		t.Fatal(err)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Signature string  `json:"signature"`
			Expiry    Integer `json:"expiry_time"`
		} `json:"data"`
	}
	err = decode(res.Body, &envelope)
	_ = res.Body.Close()
	if err != nil || envelope.Status != "success" || envelope.Data.Signature == "" {
		t.Fatal("invalid Stratus signature response")
	}
	signature, err := url.ParseQuery(envelope.Data.Signature)
	if err != nil || len(signature) == 0 {
		t.Fatal("invalid signature query")
	}
	t.Log("Temporary bucket signature obtained; its value is not logged")
	key := "tfacc-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".bin"
	params := url.Values{"bucket_name": {bucket}, "object_key": {key}}
	// Register exact-key cleanup before issuing a potentially ambiguous upload.
	attempted := false
	t.Cleanup(func() {
		if !attempted {
			return
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		payload, err := json.Marshal(map[string]any{"objects": []map[string]string{{"key": key}}})
		if err != nil {
			t.Error(err)
			return
		}
		r, err := c.request(ctx, http.MethodPut, path+"/bucket/object?"+bucketQuery.Encode(), payload)
		if err != nil {
			t.Error(err)
			return
		}
		if r.StatusCode != 200 {
			err = apiResponseError("Stratus fixture deletion", r)
			_ = r.Body.Close()
			t.Error(err)
			return
		}
		_ = r.Body.Close()
		for {
			r, err = c.get(ctx, path+"/bucket/object?"+params.Encode())
			if err != nil {
				t.Error(err)
				return
			}
			status := r.StatusCode
			_ = r.Body.Close()
			if status == 404 {
				t.Log("Exact Stratus object cleanup confirmed; empty bucket retained as test prerequisite")
				return
			}
			if status != 200 {
				t.Error("Stratus cleanup absence unverified")
				return
			}
			if err = c.wait(ctx, 2*time.Second); err != nil {
				t.Error(err)
				return
			}
		}
	})
	before, err := c.get(t.Context(), path+"/bucket/object?"+params.Encode())
	if err != nil {
		t.Fatal(err)
	}
	status := before.StatusCode
	_ = before.Body.Close()
	if status != 404 {
		t.Fatal("generated object key is not absent")
	}
	transfer := func(method string, body []byte, overwrite bool) (int, []byte) {
		req, err := http.NewRequestWithContext(t.Context(), method, details.URL+"/_signed/"+url.PathEscape(key)+"?"+signature.Encode(), bytes.NewReader(body))
		if err != nil {
			t.Fatal("could not construct signed object request")
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/octet-stream")
			req.Header.Set("compress", "false")
			req.Header.Set("overwrite", strconv.FormatBool(overwrite))
		}
		r, err := c.http.Do(req)
		if err != nil {
			t.Fatal("Stratus transfer failed; signed URL withheld")
		}
		defer r.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil {
			t.Fatal("Stratus response unreadable")
		}
		return r.StatusCode, raw
	}
	attempted = true
	first := []byte{0, 1, 2, 255, 4}
	status, _ = transfer(http.MethodPut, first, false)
	if status != 200 {
		t.Fatalf("Stratus create HTTP %d", status)
	}
	status, content := transfer(http.MethodGet, nil, false)
	if status != 200 || !bytes.Equal(content, first) {
		t.Fatal("Stratus binary read-back mismatch")
	}
	status, _ = transfer(http.MethodPut, []byte("other"), false)
	if status == 200 {
		t.Fatal("overwrite=false did not protect existing key")
	}
	t.Log("Existing-key creation refused with HTTP", status)
	second := []byte{4, 255, 2, 1, 0}
	status, _ = transfer(http.MethodPut, second, true)
	if status != 200 {
		t.Fatalf("Stratus update HTTP %d", status)
	}
	status, content = transfer(http.MethodGet, nil, false)
	if status != 200 || !bytes.Equal(content, second) {
		t.Fatal("Stratus update read-back mismatch")
	}
	r, err := c.get(t.Context(), path+"/bucket/object?"+params.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 {
		_ = r.Body.Close()
		t.Fatal("Stratus object metadata read failed")
	}
	var metadata struct {
		Status string                     `json:"status"`
		Data   map[string]json.RawMessage `json:"data"`
	}
	err = decode(r.Body, &metadata)
	_ = r.Body.Close()
	if err != nil || metadata.Status != "success" {
		t.Fatal("invalid object metadata")
	}
	keys := make([]string, 0, len(metadata.Data))
	for key := range metadata.Data {
		keys = append(keys, key)
	}
	t.Log("Object metadata field names:", keys)
	t.Log("Stratus create/read/overwrite protection/update verified with five-byte fixtures")
}
