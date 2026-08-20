// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"slices"
	"strings"
)

type ProjectEnvironment struct {
	ID      ID       `json:"env_zgid"`
	Name    string   `json:"env_name"`
	Status  *string  `json:"env_status"`
	Type    *Integer `json:"env_type"`
	Default *bool    `json:"is_default"`
	Project *struct {
		ID ID `json:"id"`
	} `json:"project_details"`
}

// ListProjectEnvironments returns existing environments via the official CLI's read contract.
func (c *Client) ListProjectEnvironments(ctx context.Context, projectID string) ([]ProjectEnvironment, error) {
	path, err := inventoryPath(projectID, "environment")
	if err != nil {
		return nil, err
	}
	environments, err := getList[ProjectEnvironment](ctx, c, path)
	if err != nil {
		return nil, err
	}
	seen := map[ID]bool{}
	for _, environment := range environments {
		if !ValidID(string(environment.ID)) || strings.TrimSpace(environment.Name) == "" || seen[environment.ID] {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate environment metadata")
		}
		if environment.Project != nil && string(environment.Project.ID) != projectID {
			return nil, errors.New("the Catalyst API returned an environment from a different or unidentified project")
		}
		seen[environment.ID] = true
	}
	slices.SortFunc(environments, func(a, b ProjectEnvironment) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return environments, nil
}
