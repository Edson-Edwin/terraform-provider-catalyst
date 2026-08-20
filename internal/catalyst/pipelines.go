// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type Pipeline struct {
	ID      ID      `json:"pipeline_id"`
	Name    string  `json:"name"`
	Status  *string `json:"pipeline_status"`
	Project struct {
		ID ID `json:"id"`
	} `json:"project_details"`
}

// GetPipeline returns allowlisted metadata for one existing pipeline.
func (c *Client) GetPipeline(ctx context.Context, projectID, pipelineID string) (*Pipeline, error) {
	if !ValidID(pipelineID) {
		return nil, errors.New("pipeline ID must be a positive numeric string")
	}
	path, err := inventoryPath(projectID, "pipeline/"+pipelineID)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst pipeline lookup", res)
	}
	var result struct {
		Status string    `json:"status"`
		Data   *Pipeline `json:"data"`
	}
	if decode(res.Body, &result) != nil || result.Status != "success" || result.Data == nil {
		return nil, errors.New("invalid Catalyst pipeline metadata")
	}
	if string(result.Data.ID) != pipelineID || string(result.Data.Project.ID) != projectID || strings.TrimSpace(result.Data.Name) == "" {
		return nil, errors.New("the Catalyst API returned incomplete metadata or a different pipeline or project ID")
	}
	return result.Data, nil
}
