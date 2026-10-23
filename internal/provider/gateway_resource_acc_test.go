// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAccGatewayResources(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_GATEWAY_CREATE") != "1" {
		t.Skip("requires explicit isolated Development Gateway write opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !catalyst.ValidID(project) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development project")
	}
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := client.GetGateway(t.Context(), project)
	if err != nil || *state.Enabled || *state.Scheduled {
		t.Fatal("requires disabled idle Gateway baseline")
	}
	baseline, err := client.ListFunctions(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	source := writeFunctionArchive(t, t.TempDir(), name, "gateway")
	function := functionConfig(project, name, source, false)
	config := func(mode int) string {
		return function + gatewayConfig(project, "catalyst_function.test.id", "/server/"+name+"/execute", mode)
	}
	advancedName := name + "a"
	advancedSource := writeFunctionArchiveVariant(t, t.TempDir(), advancedName, "gateway", "node22", "advancedio")
	advancedFunction := strings.ReplaceAll(functionConfig(project, advancedName, advancedSource, false), `"test"`, `"advanced"`)
	advancedFunction = strings.Replace(advancedFunction, `provider "catalyst" {}`, "", 1)
	advancedFunction = strings.Replace(advancedFunction, `resource "catalyst_function" "advanced" {`, "resource \"catalyst_function\" \"advanced\" {\n type = \"advancedio\"\n", 1)
	advancedConfig := function + advancedFunction + strings.Replace(gatewayConfig(project, "catalyst_function.advanced.id", "", 3), "/server/example/path", "/server/"+advancedName+"/health", 1)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Minute)
		defer cancel()
		items, err := client.ListFunctions(ctx, project)
		if err != nil {
			t.Error(err)
			return
		}
		owned := map[catalyst.ID]bool{}
		for _, item := range items {
			file := ""
			if item.Name == name {
				file = source
			}
			if item.Name == advancedName {
				file = advancedSource
			}
			if file == "" {
				continue
			}
			b, err := os.ReadFile(file)
			if err != nil {
				t.Error(err)
				return
			}
			expected := sha256.Sum256(b)
			actual, err := client.FunctionSHA256(ctx, project, string(item.ID))
			if err != nil || actual != hex.EncodeToString(expected[:]) {
				t.Error("generated Function identity/code no longer matches; cleanup refused")
				return
			}
			owned[item.ID] = true
		}
		state, err := client.WaitGatewayIdle(ctx, project)
		if err != nil {
			t.Error(err)
			return
		}
		if *state.Enabled {
			rules, err := client.ListGatewayRules(ctx, project)
			if err != nil {
				t.Error(err)
				return
			}
			for _, rule := range rules {
				if !owned[rule.TargetID] {
					t.Error("foreign Gateway rule found; cleanup refused")
					return
				}
			}
			if len(rules) > 0 {
				if err = client.SubmitGatewayRules(ctx, project, []catalyst.GatewayRule{}, false, nil); err != nil {
					t.Error(err)
					return
				}
			}
			if err = client.SetGatewayEnabled(ctx, project, false); err != nil {
				t.Error(err)
				return
			}
		}
		for id := range owned {
			if err = client.DeleteFunction(ctx, project, string(id)); err != nil {
				t.Error(err)
				return
			}
			if err = client.WaitFunctionDeleted(ctx, project, string(id)); err != nil {
				t.Error(err)
				return
			}
		}
	})
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(_ *terraform.State) error {
		state, err := client.GetGateway(t.Context(), project)
		if err != nil || *state.Enabled || *state.Scheduled {
			return errors.New("Gateway disabled baseline not restored")
		}
		current, err := client.ListFunctions(t.Context(), project)
		if err != nil || len(current) != len(baseline) {
			return errors.New("Function baseline not restored")
		}
		for i := range current {
			if current[i].ID != baseline[i].ID {
				return errors.New("Function IDs differ from baseline")
			}
		}
		return nil
	}, Steps: []resource.TestStep{
		{Config: config(0), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.%", "2")},
		{ResourceName: "catalyst_api_gateway.test", ImportState: true, ImportStateId: project, ImportStateVerify: true},
		{ResourceName: "catalyst_api_gateway_ruleset.test", ImportState: true, ImportStateId: project, ImportStateVerify: true},
		{Config: config(1), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.updated.method", "ANY"), resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.%", "1"))},
		{Config: config(5), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.secured.authentication.#", "3")},
		{Config: advancedConfig, Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.advanced.target", "advancedio")},
		{ResourceName: "catalyst_api_gateway_ruleset.test", ImportState: true, ImportStateId: project, ImportStateVerify: true},
		{Config: advancedConfig, PlanOnly: true},
		{Config: config(2), Check: resource.TestCheckResourceAttr("catalyst_api_gateway_ruleset.test", "rules.%", "0")},
		{Config: config(2), PlanOnly: true},
	}})
	fmt.Println("Gateway Terraform lifecycle passed; no routes/Functions invoked; Gateway and Function baselines restored.")
}
