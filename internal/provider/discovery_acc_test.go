// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/base64"
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

// TestAccInfrastructureDetails verifies the vendor's responses through Terraform.
func TestAccInfrastructureDetails(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live metadata read opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	pool := os.Getenv("CATALYST_TEST_JOB_POOL_ID")
	bucket := os.Getenv("CATALYST_TEST_BUCKET")
	if !catalyst.ValidID(project) || !catalyst.ValidID(pool) || !catalyst.ValidStratusBucketName(bucket) {
		t.Fatal("set CATALYST_TEST_PROJECT_ID, CATALYST_TEST_JOB_POOL_ID and CATALYST_TEST_BUCKET to existing fixtures")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_job_pool" "test" {
  project_id = %q
  id         = %q
}
data "catalyst_stratus_bucket" "test" {
  project_id  = %q
  bucket_name = %q
}
data "catalyst_stratus_bucket_cors" "test" {
  project_id  = %q
  bucket_name = %q
}
`, project, pool, project, bucket, project, bucket)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config, ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.catalyst_job_pool.test", tfjsonpath.New("id"), knownvalue.StringExact(pool)),
				statecheck.ExpectKnownValue("data.catalyst_job_pool.test", tfjsonpath.New("type"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue("data.catalyst_stratus_bucket.test", tfjsonpath.New("versioning_enabled"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue("data.catalyst_stratus_bucket_cors.test", tfjsonpath.New("rules"), knownvalue.NotNull()),
			}},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestAccStratusObjectVersions reads a pre-existing versioned object.
func TestAccStratusObjectVersions(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live Stratus version read opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	bucket := os.Getenv("CATALYST_TEST_VERSIONED_BUCKET")
	key := os.Getenv("CATALYST_TEST_VERSIONED_OBJECT_KEY")
	if !catalyst.ValidID(project) || !catalyst.ValidStratusBucketName(bucket) || !catalyst.ValidStratusObjectKey(key) {
		t.Fatal("set CATALYST_TEST_PROJECT_ID, CATALYST_TEST_VERSIONED_BUCKET and CATALYST_TEST_VERSIONED_OBJECT_KEY to an existing versioned fixture")
	}
	expectedLatest := os.Getenv("CATALYST_TEST_LATEST_VERSION_ID")
	if expectedLatest == "" {
		t.Fatal("set CATALYST_TEST_LATEST_VERSION_ID to the expected latest fixture version")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_stratus_object_versions" "test" {
  project_id  = %q
  bucket_name = %q
  object_key  = base64decode(%q)
}
output "latest_matches_fixture" {
  value = jsonencode([for v in data.catalyst_stratus_object_versions.test.versions : v.version_id if v.is_latest]) == jsonencode([base64decode(%q)])
}
`, project, bucket, base64.StdEncoding.EncodeToString([]byte(key)), base64.StdEncoding.EncodeToString([]byte(expectedLatest)))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckOutput("latest_matches_fixture", "true")},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestAccAuthUserDataSource reads an existing user without changing the account.
func TestAccAuthUserDataSource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live application-user read opt-in")
	}
	project, user := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_AUTH_USER_ID")
	role := os.Getenv("CATALYST_TEST_AUTH_FALLBACK_ROLE_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(user) || !catalyst.ValidID(role) {
		t.Fatal("requires existing project and user IDs and the user's expected role ID")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_auth_user" "test" {
  project_id = %q
  user_id    = %q
}
`, project, user)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config, ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.catalyst_auth_user.test", tfjsonpath.New("role_id"), knownvalue.StringExact(role)),
			}},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestAccWebClientVersions reads existing hosting history without making a deployment.
func TestAccWebClientVersions(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live Web Client Hosting read opt-in")
	}
	project, historyID := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_WEB_CLIENT_LIVE_HISTORY_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(historyID) {
		t.Fatal("requires an existing project and the live Web Client Hosting history ID")
	}
	config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_web_client_versions" "test" { project_id = %q }
output "fixture_is_live" {
  value = contains([for v in data.catalyst_web_client_versions.test.versions : v.history_id if v.live == true], %q)
}
`, project, historyID)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckOutput("fixture_is_live", "true")},
			{Config: config, PlanOnly: true},
		},
	})
}

// TestAccInventoryDataSources performs metadata reads only, including empty lists.
func TestAccInventoryDataSources(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_PROJECT_ID to test live inventory")
	}
	projectID := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(projectID) {
		t.Fatal("CATALYST_TEST_PROJECT_ID must identify an existing test project")
	}
	for kind, fixture := range inventoryFixtures {
		if kind == "slate_apps" {
			continue // Slate has an independent read gate and OAuth scope.
		}
		t.Run(kind, func(t *testing.T) {
			config := fmt.Sprintf("provider \"catalyst\" {}\ndata \"catalyst_%s\" \"test\" { project_id = %q }", kind, projectID)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
				Steps: []resource.TestStep{
					{Config: config, ConfigStateChecks: []statecheck.StateCheck{statecheck.ExpectKnownValue("data.catalyst_"+kind+".test", tfjsonpath.New(fixture.collection), knownvalue.NotNull())}},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}

func TestAccExtendedDiscovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("requires live discovery opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	for _, scenario := range []struct {
		name, output, idArgument, idVariable string
	}{
		{"organizations", "organizations", "", ""},
		{"project_environments", "environments", "", ""},
		{"slate_apps", "apps", "", ""},
		{"pipeline", "name", "id", "CATALYST_TEST_PIPELINE_ID"},
		{"smarttrain_materials", "materials", "bot_id", "CATALYST_TEST_SMARTTRAIN_BOT_ID"},
		{"datastream_channels", "channels", "channel_id", "CATALYST_TEST_DATASTREAM_CHANNEL_ID"},
		{"auth_users", "users", "", ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var arguments string
			if scenario.name != "organizations" {
				if !catalyst.ValidID(project) {
					t.Fatal("requires an existing disposable project ID")
				}
				arguments = fmt.Sprintf("  project_id = %q\n", project)
			}
			if scenario.idVariable != "" {
				id := os.Getenv(scenario.idVariable)
				if !catalyst.ValidID(id) {
					t.Fatalf("set %s to the existing fixture's numeric ID", scenario.idVariable)
				}
				arguments += fmt.Sprintf("  %s = %q\n", scenario.idArgument, id)
			}
			address := "data.catalyst_" + scenario.name + ".test"
			config := fmt.Sprintf(`
provider "catalyst" {}
data "catalyst_%s" "test" {
%s}
`, scenario.name, arguments)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
				Steps: []resource.TestStep{
					{Config: config, ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownValue(address, tfjsonpath.New(scenario.output), knownvalue.NotNull()),
					}},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}
