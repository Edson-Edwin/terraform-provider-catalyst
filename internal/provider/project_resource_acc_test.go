// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccProjectResource creates only a fresh empty project. Existing projects
// are never adopted as fixtures, and logs contain only the generated identity.
func TestAccProjectResource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("live acceptance tests are disabled")
	}
	if os.Getenv("CATALYST_TEST_ALLOW_PROJECT_CREATE") != "1" {
		t.Fatal("requires CATALYST_TEST_ALLOW_PROJECT_CREATE=1 for disposable project creation and deletion")
	}
	dc, org := os.Getenv("CATALYST_DATA_CENTER"), os.Getenv("CATALYST_ORGANIZATION_ID")
	if dc == "" || !catalyst.ValidID(org) || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires an explicit supported CATALYST_DATA_CENTER, numeric CATALYST_ORGANIZATION_ID and CATALYST_ENVIRONMENT=Development")
	}
	client, err := catalyst.New(catalyst.Config{
		DataCenter: dc, Environment: "Development", OrganizationID: org,
		AllowedDataCenters: []string{dc}, AllowedEnvironments: []string{"Development"}, AllowedOrganizationIDs: []string{org},
		AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), ClientID: os.Getenv("CATALYST_CLIENT_ID"),
		ClientSecret: os.Getenv("CATALYST_CLIENT_SECRET"), RefreshToken: os.Getenv("CATALYST_REFRESH_TOKEN"), RedirectURI: os.Getenv("CATALYST_REDIRECT_URI"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListProjects(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal("could not generate a unique fixture name")
	}
	name := "tfacc" + hex.EncodeToString(random[:])
	t.Log("Disposable project name:", name)
	var mu sync.Mutex
	owned := make(map[string]bool)
	record := func(id, returnedName string) {
		if returnedName != name || !catalyst.ValidID(id) {
			return
		}
		for _, p := range baseline {
			if string(p.ID) == id {
				t.Error("creation returned a pre-existing identity; cleanup refused")
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		owned[id] = true
		t.Log("Disposable project ID:", id)
	}
	// Registered before Terraform starts, so failures after partial creation also
	// get bounded cleanup. A hard process termination still needs manual recovery.
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for id := range owned {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Minute)
			err := client.DeleteProject(ctx, id, name)
			if errors.Is(err, catalyst.ErrProjectNotFound) {
				err = nil
			} else if err == nil {
				err = client.WaitProjectDeleted(ctx, id)
			}
			cancel()
			if err != nil {
				t.Errorf("cleanup of generated project %s needs inspection: %v", id, err)
			}
		}
	})
	providerFields := fmt.Sprintf("\n data_center=%q\n environment=\"Development\"\n organization_id=%q\n allowed_data_centers=[%q]\n allowed_environments=[\"Development\"]\n allowed_organization_ids=[%q]\n", dc, org, dc, org)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": func() (tfprotov6.ProviderServer, error) {
			server := providerserver.NewProtocol6(New("test")())()
			schema, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				return nil, err
			}
			return &projectRecordingServer{ProviderServer: server, projectType: schema.ResourceSchemas["catalyst_project"].ValueType(), record: record}, nil
		}},
		Steps: projectManagementLifecycleSteps(name, providerFields),
		CheckDestroy: func(_ *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if len(owned) != 1 {
				return errors.New("expected exactly one recorded disposable project")
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Minute)
			defer cancel()
			for id := range owned {
				if _, err := client.GetManagedProject(ctx, id); !errors.Is(err, catalyst.ErrProjectNotFound) {
					return errors.New("generated project absence is unverified after Terraform destroy")
				}
			}
			remaining, err := client.ListProjects(ctx)
			if err != nil {
				return err
			}
			// ListProjects sorts by ID. Only compare identities: concurrent metadata
			// changes are not grounds to mutate somebody else's project.
			if len(remaining) != len(baseline) {
				return errors.New("project inventory differs from the initial baseline; inspect without automatic deletion")
			}
			for i := range baseline {
				if baseline[i].ID != remaining[i].ID {
					return errors.New("project inventory differs from the initial baseline; inspect without automatic deletion")
				}
			}
			return nil
		},
	})
}

// Observe returned Terraform state, including partial-create errors. Never infer
// ownership from a listing/name match or capture credentials/HTTP response bodies.
type projectRecordingServer struct {
	tfprotov6.ProviderServer
	projectType tftypes.Type
	record      func(id, name string)
}

func (s *projectRecordingServer) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	resp, err := s.ProviderServer.ApplyResourceChange(ctx, req)
	if err != nil || resp == nil || req.TypeName != "catalyst_project" || req.PriorState == nil || resp.NewState == nil {
		return resp, err
	}
	prior, decodeErr := req.PriorState.Unmarshal(s.projectType)
	if decodeErr != nil || !prior.IsNull() {
		return resp, err
	}
	state, decodeErr := resp.NewState.Unmarshal(s.projectType)
	if decodeErr != nil || state.IsNull() || !state.IsKnown() {
		return resp, err
	}
	var attributes map[string]tftypes.Value
	if state.As(&attributes) != nil {
		return resp, err
	}
	var id, name string
	if attributes["id"].As(&id) == nil && attributes["name"].As(&name) == nil {
		s.record(id, name)
	}
	return resp, err
}
