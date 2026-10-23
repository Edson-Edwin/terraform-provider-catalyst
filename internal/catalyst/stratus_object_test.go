// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const stratusBucketJSON = `{"status":"success","data":[{"bucket_name":"test-bucket","bucket_url":"https://test-bucket-development.zohostratus.in","bucket_meta":{"versioning":false}}]}`
const stratusObjectJSON = `{"status":"success","data":{"key":"nested/a ?#.bin","size":5,"content_type":"application/octet-stream","etag":"opaque","last_modified":"today","object_url":"https://ignored.invalid"}}`

func stratusTestClient(t *testing.T, handle func(*http.Request) (int, string, error)) *Client {
	t.Helper()
	c, err := New(Config{AccessToken: "oauth-secret", DataCenter: "in", OrganizationID: "123"}, &http.Client{Transport: retryTransport(func(r *http.Request) (*http.Response, error) {
		status, body, err := handle(r)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	c.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return c
}
func stratusSignatureJSON() string {
	return fmt.Sprintf(`{"status":"success","data":{"signature":"token=signature-secret&bucket=test","expiry_time":%d}}`, time.Now().Add(time.Hour).UnixMilli())
}

func TestStratusTransferIsolationAndBinaryLifecycle(t *testing.T) {
	exists := false
	body := []byte{0, 255, 1, 3, 4}
	writes, signatures, reads := 0, 0, 0
	c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
		if strings.HasSuffix(r.URL.Hostname(), ".zohostratus.in") {
			if r.Header.Get("Authorization") != "" || r.Header.Get("CATALYST-ORG") != "" || r.URL.Query().Get("token") != "signature-secret" || r.URL.Path != "/_signed/nested/a ?#.bin" || r.URL.Fragment != "" {
				t.Errorf("transfer isolation or key encoding broken")
			}
			if r.Method == "PUT" {
				if r.Header.Get("overwrite") != fmt.Sprint(exists) || r.Header.Get("compress") != "false" {
					t.Error("wrong create/update guard")
				}
				got, _ := io.ReadAll(r.Body)
				if !bytes.Equal(got, body) {
					t.Error("binary source corrupted")
				}
				exists = true
				writes++
				return 200, "", nil
			}
			reads++
			return 200, string(body), nil
		}
		if r.Header.Get("Authorization") != "Zoho-oauthtoken oauth-secret" {
			t.Error("API authentication missing")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/signature"):
			signatures++
			return 200, stratusSignatureJSON(), nil
		case strings.HasSuffix(r.URL.Path, "/objects"):
			return 200, `{"status":"success","data":{"contents":[],"truncated":"false"}}`, nil
		case strings.HasSuffix(r.URL.Path, "/object"):
			if r.Method == "PUT" {
				exists = false
				return 200, `{"status":"success","data":{}}`, nil
			}
			if !exists {
				return 404, "", nil
			}
			return 200, stratusObjectJSON, nil
		default:
			return 200, stratusBucketJSON, nil
		}
	})
	for _, create := range []bool{true, false} {
		accepted, err := c.PutStratusObject(t.Context(), "123", "test-bucket", "nested/a ?#.bin", "application/octet-stream", body, create)
		if err != nil || !accepted {
			t.Fatalf("upload failed: %v", err)
		}
		hash := sha256.Sum256(body)
		if _, err = c.WaitStratusObject(t.Context(), "123", "test-bucket", "nested/a ?#.bin", "application/octet-stream", hex.EncodeToString(hash[:]), 5); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.DeleteStratusObject(t.Context(), "123", "test-bucket", "nested/a ?#.bin"); err != nil {
		t.Fatal(err)
	}
	if writes != 2 || signatures != 1 || reads != 6 || exists {
		t.Fatalf("unexpected lifecycle: writes=%d signatures=%d reads=%d", writes, signatures, reads)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.stratusSignature(ctx, "123", "test-bucket"); !errors.Is(err, context.Canceled) {
		t.Fatal("cached signature ignored cancellation")
	}
}

func TestStratusAbsenceNeverHidesPermissionOrMalformedResponses(t *testing.T) {
	for _, scenario := range []string{"absent", "denied", "listed", "null_listing", "bad_pagination", "later_page", "duplicate_key"} {
		t.Run(scenario, func(t *testing.T) {
			c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/bucket"):
					return 200, stratusBucketJSON, nil
				case strings.HasSuffix(r.URL.Path, "/object"):
					if scenario == "denied" {
						return 403, "server-secret", nil
					}
					return 404, "", nil
				case strings.HasSuffix(r.URL.Path, "/objects"):
					switch scenario {
					case "listed":
						return 200, `{"status":"success","data":{"contents":[{"key":"key"}],"truncated":false}}`, nil
					case "null_listing":
						return 200, `{"status":"success","data":{"contents":null,"truncated":false}}`, nil
					case "bad_pagination":
						return 200, `{"status":"success","data":{"contents":[],"truncated":true}}`, nil
					case "later_page", "duplicate_key":
						if r.URL.Query().Get("continuation_token") == "" {
							return 200, `{"status":"success","data":{"contents":[{"key":"key-other"}],"truncated":"true","next_continuation_token":"second"}}`, nil
						}
						key := "key"
						if scenario == "duplicate_key" {
							key = "key-other"
						}
						return 200, fmt.Sprintf(`{"status":"success","data":{"contents":[{"key":%q}],"truncated":false}}`, key), nil
					}
					return 200, `{"status":"success","data":{"contents":[],"truncated":false}}`, nil
				default:
					t.Error("unexpected API call")
					return 500, "", nil
				}
			})
			_, err := c.GetStratusObject(t.Context(), "123", "test-bucket", "key")
			if errors.Is(err, ErrStratusObjectNotFound) != (scenario == "absent") || err == nil || strings.Contains(err.Error(), "server-secret") {
				t.Fatalf("unsafe absence decision: %v", err)
			}
		})
	}
}
func TestStratusWritesRefuseUnsafeOwnership(t *testing.T) {
	for _, scenario := range []string{"exists", "denied", "foreign_origin", "versioned", "custom_metadata", "racing_create", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			writes := 0
			c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
				if strings.HasSuffix(r.URL.Hostname(), ".zohostratus.in") {
					writes++
					if scenario == "ambiguous" {
						return 0, "", errors.New("signature-secret")
					}
					return 409, "server-secret", nil
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/signature"):
					return 200, stratusSignatureJSON(), nil
				case strings.HasSuffix(r.URL.Path, "/objects"):
					return 200, `{"status":"success","data":{"contents":[],"truncated":false}}`, nil
				case strings.HasSuffix(r.URL.Path, "/object"):
					if scenario == "denied" {
						return 403, "server-secret", nil
					}
					if scenario == "racing_create" || scenario == "ambiguous" {
						return 404, "", nil
					}
					body := stratusObjectJSON
					if scenario == "custom_metadata" {
						body = strings.Replace(body, `"etag":"opaque"`, `"etag":"opaque","custom_meta":{"owned":"elsewhere"}`, 1)
					}
					return 200, body, nil
				default:
					body := stratusBucketJSON
					if scenario == "foreign_origin" {
						body = strings.ReplaceAll(body, "test-bucket-development.zohostratus.in", "attacker.invalid")
					}
					if scenario == "versioned" {
						body = strings.ReplaceAll(body, `"versioning":false`, `"versioning":true`)
					}
					return 200, body, nil
				}
			})
			accepted, err := c.PutStratusObject(t.Context(), "123", "test-bucket", "nested/a ?#.bin", "application/octet-stream", []byte("bytes"), true)
			if err == nil || accepted || strings.Contains(err.Error(), "server-secret") || strings.Contains(err.Error(), "signature-secret") {
				t.Fatalf("unsafe write outcome: %v", err)
			}
			want := 0
			if scenario == "racing_create" || scenario == "ambiguous" {
				want = 1
			}
			if writes != want {
				t.Fatalf("unexpected write replay: %d", writes)
			}
		})
	}
}
func TestStratusReadReconcilesReplicaAndDetectsSameSizeDrift(t *testing.T) {
	reads := 0
	c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
		if strings.HasSuffix(r.URL.Hostname(), ".zohostratus.in") {
			reads++
			if reads == 1 {
				return 404, "", nil
			}
			if reads == 2 {
				return 200, "old", nil
			}
			return 200, "drift", nil
		}
		if strings.HasSuffix(r.URL.Path, "/signature") {
			return 200, stratusSignatureJSON(), nil
		}
		if strings.HasSuffix(r.URL.Path, "/object") {
			return 200, stratusObjectJSON, nil
		}
		return 200, stratusBucketJSON, nil
	})
	_, hash, err := c.ReadStratusObject(t.Context(), "123", "test-bucket", "nested/a ?#.bin")
	sum := sha256.Sum256([]byte("drift"))
	if err != nil || hash != hex.EncodeToString(sum[:]) || reads != 3 {
		t.Fatalf("replica/hash reconciliation failed: %s %v", hash, err)
	}
}
func TestStratusInputValidation(t *testing.T) {
	for _, key := range []string{"", "/absolute", "../relative", "a/../b", "a//b", "a\\b", "a\n", strings.Repeat("a", 1025)} {
		if ValidStratusObjectKey(key) {
			t.Errorf("accepted key %q", key)
		}
	}
	for _, key := range []string{"nested/key", "a ?#.bin", "é/😀"} {
		if !ValidStratusObjectKey(key) {
			t.Errorf("rejected key %q", key)
		}
	}
	for _, value := range []string{"TEXT/PLAIN", "invalid", "text/plain\r\nX: injected"} {
		if ValidObjectContentType(value) {
			t.Errorf("accepted MIME %q", value)
		}
	}
}

func TestStratusDeletedBucketRequiresVerifiedProjectAndListing(t *testing.T) {
	for _, mode := range []string{"absent", "parent_denied", "still_listed", "null_list"} {
		t.Run(mode, func(t *testing.T) {
			c := stratusTestClient(t, func(r *http.Request) (int, string, error) {
				if strings.HasSuffix(r.URL.Path, "/bucket") {
					if r.URL.Query().Get("bucket_name") != "" {
						return 404, "", nil
					}
					if mode == "still_listed" {
						return 200, stratusBucketJSON, nil
					}
					if mode == "null_list" {
						return 200, `{"status":"success","data":null}`, nil
					}
					return 200, `{"status":"success","data":[]}`, nil
				}
				if mode == "parent_denied" {
					return 403, "server-secret", nil
				}
				return 200, `{"status":"success","data":{"id":"123","project_name":"test"}}`, nil
			})
			_, err := c.GetStratusObject(t.Context(), "123", "test-bucket", "asset")
			if errors.Is(err, ErrStratusObjectNotFound) != (mode == "absent") {
				t.Fatalf("unsafe parent absence classification: %v", err)
			}
		})
	}
}
