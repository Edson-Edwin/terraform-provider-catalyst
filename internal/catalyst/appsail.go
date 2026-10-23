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

// AppSailMetadata intentionally excludes environment variables. Its authenticated URL identifies the scheduling origin.
// Endpoints follow the official CLI; deployment configuration uses a separate type.
type AppSailMetadata struct {
	ID    ID     `json:"id"`
	Name  string `json:"name"`
	Stack string `json:"stack"`
	URL   string `json:"url,omitempty"`
}

func appsailPath(projectID, id string) (string, error) {
	if !ValidID(projectID) || (id != "" && !ValidID(id)) {
		return "", errors.New("project and appsail IDs must be positive integer strings")
	}
	path := "/baas/v1/project/" + projectID + "/appsail"
	if id != "" {
		path += "/" + id
	}
	return path, nil
}
func validAppSail(f AppSailMetadata) bool {
	return ValidID(string(f.ID)) && strings.TrimSpace(f.Name) != "" && strings.TrimSpace(f.Stack) != ""
}

// ListAppSails reads managed and custom runtime metadata without configuration secrets.
// Empty responses are valid; incomplete/duplicate metadata is not.
func (c *Client) ListAppSails(ctx context.Context, projectID string) ([]AppSailMetadata, error) {
	path, err := appsailPath(projectID, "")
	if err != nil {
		return nil, err
	}
	appsails, err := getList[AppSailMetadata](ctx, c, path)
	if err != nil {
		return nil, err
	}
	ids := make(map[ID]bool)
	for _, f := range appsails {
		if !validAppSail(f) || ids[f.ID] {
			return nil, errors.New("the Catalyst returned incomplete or duplicate appsail metadata")
		}
		ids[f.ID] = true
	}
	slices.SortFunc(appsails, func(a, b AppSailMetadata) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return appsails, nil
}

// GetAppSail requires a matching identity. Null/404 absence is confirmed using
// parent access and a successful complete AppSail listing.
func (c *Client) GetAppSail(ctx context.Context, projectID, id string) (*AppSailMetadata, error) {
	return readAppSailEventually(ctx, c, func(ctx context.Context) (*AppSailMetadata, error) { return c.getAppSailOnce(ctx, projectID, id) })
}
func (c *Client) getAppSailOnce(ctx context.Context, projectID, id string) (*AppSailMetadata, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid appsail ID")
	}
	path, err := appsailPath(projectID, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, c.appsailError(ctx, res, projectID, id)
	}

	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("the Catalyst returned invalid appsail metadata")
	}
	if string(envelope.Data) == "null" {
		return nil, c.confirmAppSailAbsent(ctx, projectID, id)
	}
	var value AppSailMetadata
	if json.Unmarshal(envelope.Data, &value) != nil || !validAppSail(value) || string(value.ID) != id {
		return nil, errors.New("the Catalyst returned invalid appsail metadata")
	}
	return &value, nil
}
