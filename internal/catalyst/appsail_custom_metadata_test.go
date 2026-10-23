// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"net/http"
	"testing"
)

func TestAppSailCustomRuntimeMetadataWithoutStack(t *testing.T) {
	fixture := `{"id":"789","name":"container","deployment_type":"custom_runtime","url":"https://container.catalystappsail.in"}`
	c := testClient(t, Config{AccessToken: "offline"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/baas/v1/project/123/appsail" {
			writeResponse(t, w, `{"status":"success","data":[`+fixture+`]}`)
		} else {
			writeResponse(t, w, `{"status":"success","data":`+fixture+`}`)
		}
	})
	items, err := c.ListAppSails(t.Context(), "123")
	if err != nil || len(items) != 1 || items[0].Platform != "custom_runtime" {
		t.Fatal("custom runtime blocked AppSail inventory", err)
	}
	if _, err = c.GetAppSail(t.Context(), "123", "789"); err != nil {
		t.Fatal(err)
	}
	if validAppSail(AppSailMetadata{ID: "789", Name: "invalid"}) {
		t.Fatal("incomplete managed metadata accepted")
	}
}
