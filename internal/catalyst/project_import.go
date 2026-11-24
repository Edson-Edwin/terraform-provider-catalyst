// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ProjectImportJob contains the status of a Catalyst CLI or console IaC import.
// ProjectID is populated only when the import reports completion.
type ProjectImportJob struct {
	Status    string
	ProjectID ID
}

func (c *Client) GetProjectImportJob(ctx context.Context, id string) (*ProjectImportJob, error) {
	if !ValidID(id) {
		return nil, errors.New("project import job ID must be a positive numeric string")
	}
	if c.config.Environment != "Development" || c.config.OrganizationID == "" {
		return nil, errors.New("project import status requires Development and an explicit organization")
	}
	headers := make(http.Header)
	headers.Set("Accept", "application/vnd.catalyst.v2+json")
	headers.Set("X-CATALYST-Environment", c.config.Environment)
	res, err := c.requestWithHeaders(ctx, http.MethodGet, "/baas/v1/project/import/"+id, nil, "", headers)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, statusError("Catalyst project import status", res.StatusCode)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ID      ID     `json:"id"`
			Status  string `json:"status"`
			Project struct {
				ID json.RawMessage `json:"id"`
			} `json:"project_details"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || string(envelope.Data.ID) != id || strings.TrimSpace(envelope.Data.Status) == "" {
		return nil, errors.New("the Catalyst API returned incomplete or mismatched import job metadata")
	}
	job := &ProjectImportJob{Status: envelope.Data.Status}
	if job.Status == "Completed" {
		if json.Unmarshal(envelope.Data.Project.ID, &job.ProjectID) != nil {
			return nil, errors.New("the completed Catalyst import job did not return a valid project ID")
		}
	}
	return job, nil
}
