// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

type Organization struct {
	ID      ID     `json:"id"`
	Name    string `json:"name"`
	Default *bool  `json:"is_default"`
}

// ListOrganizations returns the organizations visible to this OAuth identity.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	organizations, err := getList[Organization](ctx, c, "/baas/v1/orgs")
	if err != nil {
		return nil, err
	}
	seen := map[ID]bool{}
	for _, organization := range organizations {
		if !ValidID(string(organization.ID)) || strings.TrimSpace(organization.Name) == "" || seen[organization.ID] {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate organization metadata")
		}
		seen[organization.ID] = true
	}
	slices.SortFunc(organizations, func(a, b Organization) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return organizations, nil
}

// JobPool contains public metadata needed to identify an existing scheduling pool.
type JobPool struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// JobPoolDetails includes capacity for the target types that report it.
type JobPoolDetails struct {
	JobPool
	MemoryMB *int64
	Count    *int64
}

// GetJobPool reads one existing pool by its precision-preserving numeric ID.
func (c *Client) GetJobPool(ctx context.Context, projectID, poolID string) (*JobPoolDetails, error) {
	if !ValidID(projectID) || !ValidID(poolID) {
		return nil, errors.New("project and job pool IDs must be positive integers written as strings")
	}
	res, err := c.get(ctx, "/baas/v1/project/"+projectID+"/job_scheduling/jobpool/"+poolID)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst job pool lookup", res)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			JobPool
			Capacity struct {
				Memory json.Number `json:"memory"`
				Count  json.Number `json:"number"`
			} `json:"capacity"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || string(envelope.Data.ID) != poolID ||
		strings.TrimSpace(envelope.Data.Name) == "" || strings.TrimSpace(envelope.Data.Type) == "" {
		return nil, errors.New("invalid job pool metadata or a different pool ID")
	}
	result := &JobPoolDetails{JobPool: envelope.Data.JobPool}
	for _, field := range []struct {
		value  json.Number
		target **int64
	}{
		{envelope.Data.Capacity.Memory, &result.MemoryMB},
		{envelope.Data.Capacity.Count, &result.Count},
	} {
		if field.value == "" {
			continue
		}
		value, err := field.value.Int64()
		if err != nil || value < 0 {
			return nil, errors.New("invalid job pool capacity returned by Catalyst")
		}
		*field.target = &value
	}
	return result, nil
}

// ListProjects returns visible projects in deterministic ID order.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	projects, err := getList[Project](ctx, c, "/baas/v1/project")
	if err != nil {
		return nil, err
	}
	seen := make(map[ID]bool)
	for _, p := range projects {
		if !ValidID(string(p.ID)) || p.Name == "" || seen[p.ID] {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate project metadata")
		}
		seen[p.ID] = true
	}
	slices.SortFunc(projects, func(a, b Project) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return projects, nil
}

// ListJobPools returns pools belonging to a project without submitting any jobs.
func (c *Client) ListJobPools(ctx context.Context, projectID string) ([]JobPool, error) {
	if !ValidID(projectID) {
		return nil, errors.New("project id must be a positive integer written as a string")
	}
	pools, err := getList[JobPool](ctx, c, "/baas/v1/project/"+projectID+"/job_scheduling/jobpool")
	if err != nil {
		return nil, err
	}
	seen := make(map[ID]bool)
	for _, p := range pools {
		if !ValidID(string(p.ID)) || p.Name == "" || p.Type == "" || seen[p.ID] {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate job pool metadata")
		}
		seen[p.ID] = true
	}
	slices.SortFunc(pools, func(a, b JobPool) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return pools, nil
}

func getList[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst list request", res)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   []T    `json:"data"`
	}
	if err := decode(res.Body, &envelope); err != nil {
		return nil, errors.New("the Catalyst API returned an invalid list response")
	}
	// Missing/null data is not an empty successful listing. Reject it so permissions
	// and malformed responses cannot silently erase a discovery result from state.
	if envelope.Status != "success" || envelope.Data == nil {
		return nil, errors.New("the Catalyst API did not return a successful list response")
	}
	return envelope.Data, nil
}
