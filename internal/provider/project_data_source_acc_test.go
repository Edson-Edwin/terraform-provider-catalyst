// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// TestAccProjectDataSource reads an existing project; it never manages remote resources.
func TestAccProjectDataSource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_PROJECT_ID to run against a live Catalyst account")
	}
	projectID := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(projectID) {
		t.Fatal("CATALYST_TEST_PROJECT_ID must identify an existing project using a positive numeric ID")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}

data "catalyst_project" "test" {
  id = %q
}
`, projectID)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"catalyst": providerserver.NewProtocol6WithError(New("test")()),
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.catalyst_project.test", tfjsonpath.New("id"), knownvalue.StringExact(projectID)),
					statecheck.ExpectKnownValue("data.catalyst_project.test", tfjsonpath.New("name"), knownvalue.StringRegexp(regexp.MustCompile(`\S`))),
				},
			},
			// A second plan must remain stable after the live read has been written to local state.
			{Config: config, PlanOnly: true},
		},
	})
}
