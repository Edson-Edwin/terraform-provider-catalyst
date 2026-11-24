// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestSmartTrainMaterialLifecycle(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("CATALYST_ORGANIZATION_ID", "321")
	t.Setenv("CATALYST_ACCESS_TOKEN", "offline-test")
	const route = "/baas/v1/project/123/convokraft/api/v1/bots/456/rag_training_data?type=DEVELOPED"
	const address = "catalyst_smarttrain_material.test"
	const baseURL = "https://example.com/guide"
	const firstID = "9007199254740993123"
	const secondID = "9007199254740993124"
	var mu sync.Mutex
	var id, title string
	var creates, deletes int
	p := &CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.RequestURI() != route || req.Header.Get("CATALYST-ORG") != "321" || req.Header.Get("X-CATALYST-projectid") != "123" || req.Header.Get("X-CATALYST-Environment") != "Development" {
			return nil, fmt.Errorf("unexpected SmartTrain routing")
		}
		status, body := 200, ""
		switch req.Method {
		case http.MethodGet:
			body = `{"rag_training_data":[{"id":"12", "title":"Unrelated", "source":"URL", "url":"https://example.com/other"}]}`
			if id != "" {
				body = strings.Replace(body, `}]}`, fmt.Sprintf(`},{"id":%q,"title":%q,"source":"URL","url":%q,"status":"TRAINING","created_by":{"email":"excluded@example.com"}}]}`, id, title, baseURL+"/"), 1)
			}
		case http.MethodPost, http.MethodDelete:
			var request struct {
				Items []map[string]string `json:"rag_training_data"`
			}
			if json.NewDecoder(req.Body).Decode(&request) != nil || len(request.Items) != 1 {
				return nil, fmt.Errorf("expected one SmartTrain material per write")
			}
			if req.Method == http.MethodPost {
				if id != "" || request.Items[0]["url"] != baseURL || request.Items[0]["title"] == "" || len(request.Items[0]) != 2 {
					return nil, fmt.Errorf("unexpected SmartTrain create request")
				}
				creates++
				id, title = firstID, request.Items[0]["title"]
				if creates == 2 {
					id = secondID
				}
				status, body = 202, fmt.Sprintf(`{"status":"SUCCESS","code":"SUCCESS","info":{"files":[{"id":%q,"url":%q}]}}`, id, baseURL+"/")
			} else {
				if id == "" || request.Items[0]["id"] != id || len(request.Items[0]) != 1 {
					return nil, fmt.Errorf("unexpected SmartTrain deletion request")
				}
				deletes++
				id = ""
				body = `{"status":"SUCCESS","code":"SUCCESS"}`
			}
		default:
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	config := func(title string) string {
		return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_smarttrain_material" "test" {
  project_id = "123"
  bot_id     = "456"
  url        = %q
  title      = %q
}`, baseURL, title)
	}
	initial, changed := config("Initial"), config("Renamed")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(p)},
		Steps: []resource.TestStep{
			{Config: initial, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "id", firstID), resource.TestCheckResourceAttr(address, "url", baseURL), resource.TestCheckResourceAttr(address, "status", "TRAINING"))},
			{ResourceName: address, ImportState: true, ImportStateId: "123/456/" + firstID, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"url"}, ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 || states[0].Attributes["url"] != baseURL+"/" {
					return fmt.Errorf("import did not preserve the canonical SmartTrain URL")
				}
				return nil
			}},
			{Config: changed, Check: resource.TestCheckResourceAttr(address, "id", secondID)},
			{Config: changed, PlanOnly: true},
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if id != "" || creates != 2 || deletes != 2 {
		t.Fatalf("unexpected SmartTrain lifecycle: created %d, deleted %d, managed fixture present %t", creates, deletes, id != "")
	}
}
