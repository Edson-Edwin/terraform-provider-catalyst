// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFileUploadAndDownload(t *testing.T) {
	content := []byte{0, 1, 2, 255, 13, 10}
	c := testClient(t, Config{AccessToken: "secret", OrganizationID: "789", Environment: "Production"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Zoho-oauthtoken secret" || r.Header.Get("Environment") != "Production" || r.Header.Get("CATALYST-ORG") != "789" {
			t.Error("missing auth/routing")
		}
		switch r.URL.Path {
		case "/baas/v1/project/123/folder/456/file":
			if r.Method != "POST" {
				t.Error("unexpected upload method")
			}
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.MultipartForm.RemoveAll() }()
			f, h, err := r.FormFile("code")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(content) || h.Filename != "asset.bin" || r.FormValue("file_name") != "asset.bin" {
				t.Error("multipart upload changed content/name")
			}
			w.WriteHeader(201)
			writeResponse(t, w, `{"status":"success","data":{"id":9007199254740993123,"file_name":"asset.bin","file_size":"6"}}`)
		case "/baas/v1/project/123/folder/456/file/9007199254740993123/download":
			_, _ = w.Write(content)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
	})
	file, err := c.UploadFile(t.Context(), "123", "456", "asset.bin", content)
	if err != nil || string(file.ID) != "9007199254740993123" || *file.Size != 6 {
		t.Fatalf("upload failed: %+v %v", file, err)
	}
	got, err := c.FileSHA256(t.Context(), "123", "456", string(file.ID))
	hash := sha256.Sum256(content)
	if err != nil || got != hex.EncodeToString(hash[:]) {
		t.Fatalf("download hash incorrect: %v", err)
	}
}
func TestFilePartialCreateAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		retains    bool
	}{
		{"malformed_size", `{"status":"success","data":{"id":"123","file_name":"x","file_size":{}}}`, 200, true},
		{"missing_name", `{"status":"success","data":{"id":"123","file_size":1}}`, 200, true},
		{"missing_size", `{"status":"success","data":{"id":"123","file_name":"x"}}`, 200, true},
		{"bad_id", `{"status":"success","data":{"id":1.5}}`, 200, false},
		{"failure", `{"status":"failure","data":{"id":"123"}}`, 200, false},
		{"server", `server-secret`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			})
			file, err := c.UploadFile(t.Context(), "1", "2", "x", []byte("x"))
			if err == nil || strings.Contains(err.Error(), "server-secret") {
				t.Fatalf("expected safe error, got %v", err)
			}
			if (file != nil) != tc.retains {
				t.Fatalf("unexpected retained identity %+v", file)
			}
			if calls != 1 {
				t.Fatal("ambiguous upload retried")
			}
		})
	}
}
func TestFileAbsenceRequiresAccess(t *testing.T) {
	for _, denied := range []string{"", "project", "folder"} {
		t.Run("denied_"+denied, func(t *testing.T) {
			c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/baas/v1/project/1":
					if denied == "project" {
						w.WriteHeader(403)
						return
					}
					writeResponse(t, w, `{"status":"success","data":{"id":"1","project_name":"Test"}}`)
				case "/baas/v1/project/1/folder":
					if denied == "folder" {
						w.WriteHeader(403)
						return
					}
					writeResponse(t, w, `{"status":"success","data":[]}`)
				default:
					w.WriteHeader(404)
					writeResponse(t, w, `{"status":"failure","data":{"error_code":"INVALID_ID"}}`)
				}
			})
			_, err := c.GetFile(t.Context(), "1", "2", "3")
			if errors.Is(err, ErrFileNotFound) != (denied == "") || err == nil {
				t.Fatalf("unexpected absence classification %v", err)
			}
		})
	}
}
func TestFileDeletionAndCancellation(t *testing.T) {
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
		writeResponse(t, w, `{"status":"success","data":{"id":"3","file_name":"x","file_size":1}}`)
	})
	if err := c.DeleteFile(t.Context(), "1", "2", "3"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := c.WaitFileDeleted(ctx, "1", "2", "3"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded wait: %v", err)
	}
	c = testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) {
		writeResponse(t, w, `{"status":"success","data":{"id":"4","file_name":"x","file_size":1}}`)
	})
	if err := c.DeleteFile(t.Context(), "1", "2", "3"); err == nil {
		t.Fatal("accepted mismatched delete identity")
	}
}
func TestFileRejectInvalidInputs(t *testing.T) {
	c := testClient(t, Config{AccessToken: "secret"}, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	for _, name := range []string{"", "..", "../x", "a/b", "a\\b", "a\r\nb"} {
		if _, err := c.UploadFile(t.Context(), "1", "2", name, nil); err == nil {
			t.Errorf("accepted name %q", name)
		}
	}
	if _, err := c.UploadFile(t.Context(), "../1", "2", "x", nil); err == nil {
		t.Fatal("accepted invalid project")
	}
	if _, err := c.GetFile(t.Context(), "1", "2", ""); err == nil {
		t.Fatal("accepted missing file ID")
	}
	if _, err := c.FileSHA256(t.Context(), "1", "2", "../3"); err == nil {
		t.Fatal("accepted invalid file ID")
	}
	if err := c.DeleteFile(t.Context(), "1", "2", "0"); err == nil {
		t.Fatal("accepted zero file ID")
	}
}
