// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStratusObject(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE") != "1" {
		t.Skip("requires TF_ACC=1 and explicit isolated Stratus object write opt-in")
	}
	project, bucket := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_BUCKET")
	if !catalyst.ValidID(project) || !catalyst.ValidStratusBucketName(bucket) || os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development test project and unversioned bucket")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetStratusBucket(t.Context(), project, bucket); err != nil {
		t.Fatal(err)
	}
	key := "tfacc-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "/binary ?#.bin"
	if _, err = client.GetStratusObject(t.Context(), project, bucket, key); !errors.Is(err, catalyst.ErrStratusObjectNotFound) {
		t.Fatalf("new test key absence unverified: %v", err)
	}
	// Cleanup owns only this previously verified absent unique key, including ambiguous writes.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Minute)
		defer cancel()
		if err := client.DeleteStratusObject(ctx, project, bucket, key); err != nil {
			t.Error(err)
			return
		}
		if _, err := client.GetStratusObject(ctx, project, bucket, key); !errors.Is(err, catalyst.ErrStratusObjectNotFound) {
			t.Error("exact fixture key absence unverified")
		}
	})
	dir := t.TempDir()
	first := filepath.Join(dir, "first.bin")
	second := filepath.Join(dir, "second.bin")
	for file, body := range map[string][]byte{first: {0, 255, 1, 2, 3}, second: {5, 6, 7, 8, 0}} {
		if err = os.WriteFile(file, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	initial := stratusObjectConfig(project, bucket, key, first, "application/octet-stream")
	updated := stratusObjectConfig(project, bucket, key, second, "text/plain")
	bare := updated
	initial = strings.Replace(initial, `resource "catalyst_stratus_object" "test" {`, `resource "catalyst_stratus_object" "test" {
 metadata={owner="terraform",site="website"}`, 1)
	updated = strings.Replace(updated, `resource "catalyst_stratus_object" "test" {`, `resource "catalyst_stratus_object" "test" {
 metadata={owner="updated"}`, 1)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		_, err := client.GetStratusObject(ctx, project, bucket, key)
		if !errors.Is(err, catalyst.ErrStratusObjectNotFound) {
			return errors.New("Stratus object survived destroy or absence is unverified")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: initial, Check: resource.TestCheckResourceAttr("catalyst_stratus_object.test", "size", "5")},
		{ResourceName: "catalyst_stratus_object.test", ImportState: true, ImportStateId: project + "/" + bucket + "/" + key, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source"}},
		{Config: updated, Check: resource.TestCheckResourceAttr("catalyst_stratus_object.test", "content_type", "text/plain")},
		{Config: bare, Check: resource.TestCheckResourceAttr("catalyst_stratus_object.test", "metadata.%", "0")},
		{PreConfig: func() {
			if err := client.PutStratusObjectMetadata(t.Context(), project, bucket, key, map[string]string{"owner": "external"}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.PutStratusObject(t.Context(), project, bucket, key, "text/plain", []byte{9, 9, 9, 9, 9}, false); err != nil {
				t.Fatal(err)
			}
		}, Config: updated},
		{PreConfig: func() {
			if err := client.DeleteStratusObject(t.Context(), project, bucket, key); err != nil {
				t.Fatal(err)
			}
		}, Config: updated},
		{Config: updated, PlanOnly: true},
	}})
}
