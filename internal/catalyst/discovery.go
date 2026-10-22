// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// JobPool contains public metadata needed to identify an existing scheduling pool.
type JobPool struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
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
		return nil, statusError("Catalyst list request", res.StatusCode)
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
