// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// TestAccDiscoveryDataSources only reads metadata; no remote resources are managed.
func TestAccDiscoveryDataSources(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_PROJECT_ID to test live discovery")
	}
	projectID := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(projectID) {
		t.Fatal("CATALYST_TEST_PROJECT_ID must identify an existing project using a positive numeric ID")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_projects" "test" {}
data "catalyst_job_pools" "test" { project_id = %q }
output "contains_project" { value = contains([for p in data.catalyst_projects.test.projects : p.id], %q) }
`, projectID, projectID)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckOutput("contains_project", "true"), ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.catalyst_projects.test", tfjsonpath.New("projects"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue("data.catalyst_job_pools.test", tfjsonpath.New("project_id"), knownvalue.StringExact(projectID)),
				statecheck.ExpectKnownValue("data.catalyst_job_pools.test", tfjsonpath.New("pools"), knownvalue.NotNull()),
			}},
			{Config: config, PlanOnly: true},
		},
	})
}
