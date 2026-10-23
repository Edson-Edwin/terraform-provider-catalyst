// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccFilestoreFile creates tiny binary fixtures only, never personal files.
func TestAccFilestoreFile(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FILE_CREATE") != "1" {
		t.Skip("requires TF_ACC=1 and explicit CATALYST_TEST_ALLOW_FILE_CREATE=1")
	}
	project, folder := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_FOLDER_ID")
	if !catalyst.ValidID(project) || !catalyst.ValidID(folder) {
		t.Fatal("set an isolated CATALYST_TEST_PROJECT_ID and CATALYST_TEST_FOLDER_ID")
	}
	if env := os.Getenv("CATALYST_ENVIRONMENT"); env != "" && env != "Development" {
		t.Fatal("file acceptance tests only run in Development")
	}
	client, err := catalyst.New(catalyst.Config{DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), ClientID: os.Getenv("CATALYST_CLIENT_ID"), ClientSecret: os.Getenv("CATALYST_CLIENT_SECRET"), RefreshToken: os.Getenv("CATALYST_REFRESH_TOKEN"), RedirectURI: os.Getenv("CATALYST_REDIRECT_URI")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "fixture.bin")
	if err = os.WriteFile(source, []byte{0, 1, 2, 255, 10}, 0600); err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".bin"
	config := fileConfig(project, folder, name, source)
	updated := fileConfig(project, folder, "u"+name, source)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(state *terraform.State) error {
			for _, r := range state.RootModule().Resources {
				if r.Type != "catalyst_filestore_file" {
					continue
				}
				_, err := client.GetFile(t.Context(), project, folder, r.Primary.ID)
				if !errors.Is(err, catalyst.ErrFileNotFound) {
					return fmt.Errorf("test file %s deletion not verified", r.Primary.ID)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "size", "5")},
			{ResourceName: "catalyst_filestore_file.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}, ImportStateIdFunc: func(state *terraform.State) (string, error) {
				item := state.RootModule().Resources["catalyst_filestore_file.test"]
				if item == nil {
					return "", errors.New("test file not in state")
				}
				return project + "/" + folder + "/" + item.Primary.ID, nil
			}},
			{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_filestore_file.test", "name", "u"+name)},
			{Config: updated, PlanOnly: true},
		},
	})
}
