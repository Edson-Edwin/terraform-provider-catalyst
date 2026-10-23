// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"os"
	"testing"
)

// TestAccRegionalAuthentication only reads existing projects. Each selected DC
// has its own credentials; global CATALYST_* credentials are never inherited.
func TestAccRegionalAuthentication(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 and CATALYST_TEST_REGIONS for regional authentication smoke tests")
	}
	selected := os.Getenv("CATALYST_TEST_REGIONS")
	if selected == "" {
		t.Fatal("CATALYST_TEST_REGIONS must explicitly list the regions to verify")
	}
	fixtures, err := loadRegionalFixtures(os.Getenv, regionalProjectRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			c, err := New(f.config, nil)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				project, err := c.GetProject(t.Context(), f.project)
				if err != nil {
					t.Fatal(err)
				}
				if string(project.ID) != f.project {
					t.Fatal("project response did not match the requested fixture identity")
				}
			}
		})
	}
}
