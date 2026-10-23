// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAccStratusMetadataContract(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE") != "1" {
		t.Skip("requires isolated Stratus fixture opt-in")
	}
	project, bucket := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_BUCKET")
	if !ValidID(project) || !ValidStratusBucketName(bucket) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development bucket")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	details, err := c.GetStratusBucket(t.Context(), project, bucket)
	if err != nil {
		t.Fatal(err)
	}
	key := "tfacc-meta-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt"
	if _, err := c.GetStratusObject(t.Context(), project, bucket, key); !errors.Is(err, ErrStratusObjectNotFound) {
		t.Fatal("fixture absence unverified")
	}
	base := "/baas/v1/project/" + project + "/bucket/object?" + url.Values{"bucket_name": {bucket}}.Encode()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		body, _ := json.Marshal(map[string]any{"objects": []any{map[string]string{"key": key}}})
		r, err := c.request(ctx, "PUT", base, body)
		if err != nil {
			t.Error(err)
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Error("fixture cleanup failed")
			return
		}
		for {
			err = c.confirmStratusObjectAbsent(ctx, project, bucket, key)
			if errors.Is(err, ErrStratusObjectNotFound) {
				return
			}
			if !errors.Is(err, errStratusPending) {
				t.Error(err)
				return
			}
			if err = c.wait(ctx, time.Second); err != nil {
				t.Error(err)
				return
			}
		}
	})
	signature, err := c.stratusSignature(t.Context(), project, bucket)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(details.URL)
	if err != nil {
		t.Fatal("invalid verified origin")
	}
	endpoint.Path = "/_signed/" + key
	endpoint.RawQuery = signature
	for i, meta := range []string{"fixture=first;purpose=website", "fixture=second", ""} {
		req, err := http.NewRequestWithContext(t.Context(), "PUT", endpoint.String(), bytes.NewBufferString("fixture"+strconv.Itoa(i)))
		if err != nil {
			t.Fatal("request construction failed")
		}
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("compress", "false")
		req.Header.Set("overwrite", strconv.FormatBool(i > 0))
		if meta != "" {
			req.Header.Set("x-user-meta", meta)
		}
		r, err := c.http.Do(req)
		if err != nil {
			t.Fatal("fixture transfer failed; signed URL withheld")
		}
		status := r.StatusCode
		_ = r.Body.Close()
		if status != 200 {
			t.Fatalf("metadata transfer HTTP %d", status)
		}

		want := []map[string]string{{"fixture": "first", "purpose": "website"}, {"fixture": "second"}, {}}[i]
		if i > 0 {
			payload, _ := json.Marshal(map[string]any{"meta_data": want})
			r, err = c.request(t.Context(), "PUT", "/baas/v1/project/"+project+"/bucket/object/metadata?"+url.Values{"bucket_name": {bucket}, "object_key": {key}}.Encode(), payload)
			if err != nil {
				t.Fatal(err)
			}
			if r.StatusCode != 200 {
				err = apiResponseError("Stratus fixture metadata update", r)
				_ = r.Body.Close()
				t.Fatal(err)
			}
			_ = r.Body.Close()
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		for {
			r, err = c.get(ctx, base+"&"+url.Values{"object_key": {key}}.Encode())
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			var data map[string]json.RawMessage
			err = stratusData(r, &data)
			_ = r.Body.Close()
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			var values map[string]string
			if raw, ok := data["meta_data"]; ok {
				if json.Unmarshal(raw, &values) != nil {
					cancel()
					t.Fatal("custom metadata was not a string map")
				}
			}
			if maps.Equal(values, want) {
				t.Log("Custom metadata write verified, step", i+1)
				break
			}
			if err = c.wait(ctx, 2*time.Second); err != nil {
				cancel()
				t.Fatalf("custom metadata did not converge at step %d", i+1)
			}
		}
		cancel()

	}
}
