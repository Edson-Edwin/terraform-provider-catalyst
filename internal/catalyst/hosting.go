// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"slices"
	"strings"
)

type WebClientVersion struct {
	HistoryID ID     `json:"history_id"`
	Version   string `json:"app_version"`
	Live      *bool  `json:"status"`
}

// ListWebClientVersions reads Web Client Hosting history using Zoho's CLI contract.
func (c *Client) ListWebClientVersions(ctx context.Context, projectID string) ([]WebClientVersion, error) {
	if !ValidID(projectID) {
		return nil, errors.New("project ID must be a positive numeric string")
	}
	versions, err := getList[WebClientVersion](ctx, c, "/baas/v1/project/"+projectID+"/webapp/history")
	if err != nil {
		return nil, err
	}
	seen := make(map[ID]bool, len(versions))
	for _, version := range versions {
		if !ValidID(string(version.HistoryID)) || seen[version.HistoryID] || strings.TrimSpace(version.Version) == "" {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate web client version metadata")
		}
		seen[version.HistoryID] = true
	}
	slices.SortFunc(versions, func(a, b WebClientVersion) int {
		return strings.Compare(string(a.HistoryID), string(b.HistoryID))
	})
	return versions, nil
}
