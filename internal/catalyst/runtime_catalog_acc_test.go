// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"os"
	"testing"
)

func TestAccRuntimeCatalog(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live read opt-in")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.get(t.Context(), "/baas/get-details?feature_name=runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(apiResponseError("runtime catalog", res))
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Runtimes []string       `json:"runtimes"`
			EOL      map[string]int `json:"eol_runtimes"`
		} `json:"data"`
	}
	if err = decode(res.Body, &envelope); err != nil || envelope.Status != "success" || len(envelope.Data.Runtimes) == 0 {
		t.Fatal("runtime catalog omitted its supported runtimes")
	}
	t.Log("Vendor runtime catalog:", envelope.Data.Runtimes)
	t.Log("Vendor EOL policy:", envelope.Data.EOL)
}
