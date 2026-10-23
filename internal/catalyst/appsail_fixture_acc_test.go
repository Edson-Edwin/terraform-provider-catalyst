// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAccAppSailFixtureRecovery is a gated recovery tool for interrupted test runs.
// It only deletes explicitly selected IDs whose tfacc names encode a recent test timestamp.
func TestAccAppSailFixtureRecovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FIXTURE_RECOVERY") != "1" {
		t.Skip("requires explicit fixture recovery opt-in")
	}
	project := os.Getenv("CATALYST_TEST_PROJECT_ID")
	if !ValidID(project) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated Development project")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), DataCenter: os.Getenv("CATALYST_DATA_CENTER"), Environment: "Development", OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("CATALYST_TEST_FIXTURE_IDS"), ",") {
		if id != "" {
			if !ValidID(id) {
				t.Fatal("invalid recovery ID")
			}
			selected[id] = true
		}
	}
	for _, fn := range items {
		t.Log("Current test-project AppSail identity:", fn.ID, fn.Name)
		if !selected[string(fn.ID)] {
			continue
		}
		timestamp, err := strconv.ParseInt(strings.TrimPrefix(fn.Name, "tfacc"), 36, 64)
		age := time.Since(time.Unix(0, timestamp))
		if !strings.HasPrefix(fn.Name, "tfacc") || err != nil || age < 0 || age > 3*time.Hour {
			t.Fatal("selected AppSail does not match a recent generated fixture")
		}
		if err = c.DeleteAppSail(t.Context(), project, string(fn.ID)); err != nil {
			t.Fatal(err)
		}
		if err = c.WaitAppSailDeleted(t.Context(), project, string(fn.ID)); err != nil {
			t.Fatal(err)
		}
		t.Log("Selected fixture deletion confirmed:", fn.ID)
	}
	remaining, err := c.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Remaining test-project AppSails:", len(remaining))
}
