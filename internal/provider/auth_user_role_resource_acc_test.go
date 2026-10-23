// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccAuthUserRole never creates/invites/deletes a user. The operator must
// provide a disposable end-user and explicitly authorize role changes.
func TestAccAuthUserRole(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_AUTH_USER_ROLE_UPDATE") != "1" {
		t.Skip("requires TF_ACC=1 and explicit CATALYST_TEST_ALLOW_AUTH_USER_ROLE_UPDATE=1 with disposable user/role fixtures")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("role acceptance tests require explicit CATALYST_ENVIRONMENT=Development")
	}
	projectID, userID, roleID, fallback := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_AUTH_USER_ID"), os.Getenv("CATALYST_TEST_AUTH_ROLE_ID"), os.Getenv("CATALYST_TEST_AUTH_FALLBACK_ROLE_ID")
	for _, id := range []string{projectID, userID, roleID, fallback} {
		if !catalyst.ValidID(id) {
			t.Fatal("provide existing isolated project, disposable end-user and role/fallback IDs")
		}
	}
	if roleID == fallback {
		t.Fatal("test role and fallback must differ to exercise a real role change")
	}
	client, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), ClientID: os.Getenv("CATALYST_CLIENT_ID"), ClientSecret: os.Getenv("CATALYST_CLIENT_SECRET"), RefreshToken: os.Getenv("CATALYST_REFRESH_TOKEN"), RedirectURI: os.Getenv("CATALYST_REDIRECT_URI")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := client.GetAuthUser(t.Context(), projectID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if u.UserType != "App User" || string(u.RoleID) != fallback {
		t.Fatal("fixture must be an existing end-user initially assigned to the supplied fallback role")
	}
	// Best-effort recovery after an interrupted Terraform step. Process termination
	// can still prevent cleanup; the documented fixture IDs allow manual recovery.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cancel()
		if _, err := client.SetAuthUserRole(ctx, projectID, userID, fallback); err != nil {
			t.Errorf("test user's fallback-role recovery failed: %v", err)
		}
	})
	config := func(role string) string {
		return fmt.Sprintf(`provider "catalyst" {}
resource "catalyst_auth_user_role" "test" {
  project_id = %q
  user_id = %q
  role_id = %q
  destroy_role_id = %q
}
data "catalyst_auth_user" "test" {
  project_id = %q
  user_id = %q
  depends_on = [catalyst_auth_user_role.test]
}`, projectID, userID, role, fallback, projectID, userID)
	}
	address := "catalyst_auth_user_role.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(_ *terraform.State) error {
			u, err := client.GetAuthUser(t.Context(), projectID, userID)
			if err != nil {
				return err
			}
			if string(u.RoleID) != fallback {
				return fmt.Errorf("test user did not retain the expected fallback role")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config(roleID), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(address, "role_id", roleID), resource.TestCheckResourceAttr("data.catalyst_auth_user.test", "role_id", roleID))},
			{ResourceName: address, ImportState: true, ImportStateId: projectID + "/" + userID + "/" + fallback, ImportStateVerify: true},
			{Config: config(fallback), Check: resource.TestCheckResourceAttr(address, "role_id", fallback)},
			{Config: config(roleID), Check: resource.TestCheckResourceAttr(address, "role_id", roleID)},
			{Config: config(roleID), PlanOnly: true},
		},
	})
}
