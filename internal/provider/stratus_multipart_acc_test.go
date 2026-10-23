// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"bytes"
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

func TestAccStratusMultipart(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_STRATUS_OBJECT_CREATE") != "1" {
		t.Skip("requires isolated Stratus object write opt-in")
	}
	if os.Getenv("CATALYST_ENVIRONMENT") != "Development" || os.Getenv("CATALYST_DATA_CENTER") != "in" {
		t.Fatal("requires India Development")
	}
	p, b := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_BUCKET")
	c, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: "in", Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := "tfacc-multipart-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".bin"
	source := filepath.Join(t.TempDir(), "object.bin")
	if err = os.WriteFile(source, bytes.Repeat([]byte{0, 1, 255}, 2*1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	config := strings.Replace(stratusObjectConfig(p, b, key, source, "application/octet-stream"), " project_id =", " multipart_part_size_mb = 5\n project_id =", 1)
	address := "catalyst_stratus_object.test"
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(_ *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
		defer cancel()
		_, err := c.GetStratusObject(ctx, p, b, key)
		if !errors.Is(err, catalyst.ErrStratusObjectNotFound) {
			return errors.New("multipart object cleanup not confirmed")
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config, Check: resource.TestCheckResourceAttr(address, "size", "6291456")},
		{ResourceName: address, ImportState: true, ImportStateId: p + "/" + b + "/" + key, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"source", "multipart_part_size_mb"}},
		{Config: config, PlanOnly: true},
		{PreConfig: func() {
			if err := os.WriteFile(source, bytes.Repeat([]byte{2, 0, 254}, 2*1024*1024), 0600); err != nil {
				t.Fatal(err)
			}
		}, Config: config},
		{Config: config, PlanOnly: true},
	}})
}
