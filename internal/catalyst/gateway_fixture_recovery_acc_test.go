// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestAccGatewayGeneratedFixtureRecovery removes only the exact fixture policy
// plus a Function whose downloaded code matches this repository's generator.
func TestAccGatewayGeneratedFixtureRecovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FIXTURE_RECOVERY") != "1" {
		t.Skip("requires isolated fixture recovery opt-in")
	}
	if os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires India Development")
	}
	p := os.Getenv("CATALYST_TEST_PROJECT_ID")
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.WaitGatewayIdle(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !*state.Enabled {
		t.Log("Gateway is already disabled; no recovery writes performed")
		return
	}
	rules, err := c.ListGatewayRules(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Name != "secured" || rules[0].Source != "/secured" || rules[0].Target != "basicio" || !ValidID(string(rules[0].TargetID)) {
		t.Fatal("current rules do not match the generated fixture; no changes made")
	}
	id := string(rules[0].TargetID)
	fn, err := c.GetFunction(t.Context(), p, id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fn.Name, "tfacc") || fn.Type != "basicio" || fn.Stack != "node22" || rules[0].Endpoint != "/server/"+fn.Name+"/execute" {
		t.Fatal("Function does not match the generated fixture identity")
	}
	path, _ := functionPath(p, id)
	r, err := c.get(t.Context(), path+"/download")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 {
		_ = r.Body.Close()
		t.Fatal("fixture code could not be verified")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxFileBytes+1))
	_ = r.Body.Close()
	if err != nil || int64(len(body)) > MaxFileBytes {
		t.Fatal("fixture code unavailable")
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal("fixture archive invalid")
	}
	verified := false
	for _, f := range z.File {
		if f.Name == "index.js" {
			v, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(io.LimitReader(v, 1024))
			_ = v.Close()
			if err != nil {
				t.Fatal(err)
			}
			verified = string(b) == "module.exports = async (context, basicIO) => { basicIO.write('gateway'); context.close(); };"
		}
	}
	if !verified {
		t.Fatal("Function code is not the generated test fixture; no changes made")
	}
	if err = c.SubmitGatewayRules(t.Context(), p, []GatewayRule{}, false, nil); err != nil {
		t.Fatal(err)
	}
	if err = c.SetGatewayEnabled(t.Context(), p, false); err != nil {
		t.Fatal(err)
	}
	if err = c.DeleteFunction(t.Context(), p, id); err != nil {
		t.Fatal(err)
	}
	if err = c.WaitFunctionDeleted(t.Context(), p, id); err != nil {
		t.Fatal(err)
	}
	t.Log("Generated Gateway/Function fixture removed and disabled baseline restored")
}
