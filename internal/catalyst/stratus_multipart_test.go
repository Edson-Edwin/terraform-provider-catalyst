// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStratusMultipartProtocol(t *testing.T) {
	for _, scenario := range []string{"success", "part_failure", "completion_lost"} {
		t.Run(scenario, func(t *testing.T) {
			body := bytes.Repeat([]byte{0, 1, 255}, 2*1024*1024)
			var received []byte
			parts, completions := 0, 0
			c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
				if strings.HasSuffix(r.URL.Hostname(), ".zohostratus.in") {
					if r.Header.Get("Authorization") != "" || r.Header.Get("CATALYST-ORG") != "" || r.URL.Query().Get("token") != "signature-secret" {
						t.Error("multipart credential isolation broken")
					}
					q := r.URL.Query()
					if q.Has("multipart") {
						return 200, `{"bucket":"test-bucket-development","key":"fixture.bin","upload_id":"session-1","status":"PENDING"}`, nil
					}
					if q.Has("partNumber") {
						parts++
						if q.Get("uploadId") != "session-1" {
							t.Error("wrong upload session")
						}
						b, _ := io.ReadAll(r.Body)
						received = append(received, b...)
						if scenario == "part_failure" {
							return 503, "secret-response", nil
						}
						return 200, "", nil
					}
					if q.Has("completeMultipart") {
						completions++
						if r.Header.Get("overwrite") != "false" {
							t.Error("create overwrite guard lost")
						}
						if scenario == "completion_lost" {
							return 0, "", errors.New("signed-url-secret")
						}
						return 202, "", nil
					}
					return 500, "", nil
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/signature"):
					return 200, stratusSignatureJSON(), nil
				case strings.HasSuffix(r.URL.Path, "/objects"):
					return 200, `{"status":"success","data":{"contents":[],"truncated":"false"}}`, nil
				case strings.HasSuffix(r.URL.Path, "/object"):
					return 404, "", nil
				default:
					return 200, stratusBucketJSON, nil
				}
			})
			accepted, err := c.PutStratusObjectMultipart(t.Context(), "123", "test-bucket", "fixture.bin", "application/octet-stream", body, true, 5)
			if scenario == "success" {
				if err != nil || !accepted || parts != 2 || completions != 1 || !bytes.Equal(body, received) {
					t.Fatal("multipart assembly failed", err)
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "secret-response") || strings.Contains(err.Error(), "signed-url-secret") {
					t.Fatal("multipart failure leaked or accepted")
				}
				if scenario == "part_failure" && (parts != 1 || completions != 0 || accepted) {
					t.Fatal("part failure replayed or completed")
				}
				if scenario == "completion_lost" && (completions != 1 || !accepted) {
					t.Fatal("uncertain completion lost recovery identity")
				}
			}
		})
	}
}
