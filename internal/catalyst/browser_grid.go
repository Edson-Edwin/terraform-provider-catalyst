// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// BrowserGrid contains configuration metadata without browser endpoints or credentials.
type BrowserGrid struct {
	ID            ID       `json:"id"`
	Name          string   `json:"name"`
	MemoryMB      *Integer `json:"memory"`
	EndpointType  *Integer `json:"endpoint_type"`
	ConfigType    *Integer `json:"config_type"`
	MaxNodes      *Integer `json:"max_nodes_count"`
	MaxSessions   *Integer `json:"max_session_count"`
	MaxConcurrent *Integer `json:"max_concurrent_count"`
	Project       struct {
		ID ID `json:"id"`
	} `json:"project_details"`
}

// ReadBrowserGrids reads all visible grids or one exact grid when gridID is set.
func (c *Client) ReadBrowserGrids(ctx context.Context, projectID, gridID string) ([]BrowserGrid, error) {
	if !ValidID(projectID) || (gridID != "" && !ValidID(gridID)) {
		return nil, errors.New("project and Browser Grid IDs must be positive numeric strings")
	}
	if c.config.OrganizationID == "" {
		return nil, errors.New("an explicit organization is required for Browser Grid discovery")
	}
	path := "/browser360/v1/project/" + projectID + "/browser-grid"
	if gridID != "" {
		res, err := c.get(ctx, path+"/"+gridID)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, statusError("Catalyst Browser Grid lookup", res.StatusCode)
		}
		var result struct {
			Status string      `json:"status"`
			Data   BrowserGrid `json:"data"`
		}
		if decode(res.Body, &result) != nil || result.Status != "success" || string(result.Data.ID) != gridID || !result.Data.matchesProject(projectID) {
			return nil, errors.New("the Catalyst API returned incomplete or mismatched Browser Grid metadata")
		}
		return []BrowserGrid{result.Data}, nil
	}

	grids, err := getList[BrowserGrid](ctx, c, path)
	if err != nil {
		return nil, err
	}
	seen := make(map[ID]bool, len(grids))
	for _, grid := range grids {
		if !grid.matchesProject(projectID) || seen[grid.ID] {
			return nil, errors.New("the Catalyst API returned incomplete, duplicate or mismatched Browser Grid metadata")
		}
		seen[grid.ID] = true
	}
	slices.SortFunc(grids, func(a, b BrowserGrid) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return grids, nil
}

func (grid BrowserGrid) matchesProject(projectID string) bool {
	if string(grid.Project.ID) != projectID || !ValidID(string(grid.ID)) || strings.TrimSpace(grid.Name) == "" {
		return false
	}
	for _, value := range []*Integer{grid.MemoryMB, grid.EndpointType, grid.ConfigType, grid.MaxNodes, grid.MaxSessions, grid.MaxConcurrent} {
		if value != nil && *value < 0 {
			return false
		}
	}
	return true
}
