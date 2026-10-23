// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestAccProjectManagementContract(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_PROJECT_CREATE") != "1" {
		t.Skip("requires explicit disposable project create/delete opt-in")
	}
	if os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires India Development")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var id string
	t.Cleanup(func() {
		if id == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		current, err := c.GetManagedProject(ctx, id)
		if errors.Is(err, ErrProjectNotFound) {
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		if current.Name != name {
			t.Error("fixture identity mismatch; deletion refused")
			return
		}
		if err = c.DeleteProject(ctx, id, name); err != nil {
			t.Error(err)
			return
		}
		if err = c.WaitProjectDeleted(ctx, id); err != nil {
			t.Error(err)
		}
	})
	p, err := c.CreateProject(t.Context(), name)
	if p != nil {
		id = string(p.ID)
		t.Log("Created disposable project ID:", id)
	}
	if err != nil {
		t.Fatal(err)
	}
	p, err = c.GetManagedProject(t.Context(), id)
	if err != nil || p.Name != name {
		t.Fatalf("project read failed: %v", err)
	}
	t.Log("Public project creation/read passed; exact created ID is cleaned up")
}
