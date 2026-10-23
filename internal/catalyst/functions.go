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

// FunctionMetadata intentionally excludes environment variables and execution URLs.
// Endpoints follow the official CLI; deployment configuration uses a separate type.
type FunctionMetadata struct {
	ID    ID     `json:"id"`
	Name  string `json:"name"`
	Stack string `json:"stack"`
	Type  string `json:"type"`
}

func functionPath(projectID, id string) (string, error) {
	if !ValidID(projectID) || (id != "" && !ValidID(id)) {
		return "", errors.New("project and function IDs must be positive integer strings")
	}
	path := "/baas/v1/project/" + projectID + "/function"
	if id != "" {
		path += "/" + id
	}
	return path, nil
}
func validFunction(f FunctionMetadata) bool {
	return ValidID(string(f.ID)) && strings.TrimSpace(f.Name) != "" && strings.TrimSpace(f.Stack) != "" && strings.TrimSpace(f.Type) != ""
}

// ListFunctions reads all types without the CLI's optional type filtering.
// Empty responses are valid; incomplete/duplicate metadata is not.
func (c *Client) ListFunctions(ctx context.Context, projectID string) ([]FunctionMetadata, error) {
	path, err := functionPath(projectID, "")
	if err != nil {
		return nil, err
	}
	functions, err := getList[FunctionMetadata](ctx, c, path)
	if err != nil {
		return nil, err
	}
	ids := make(map[ID]bool)
	for _, f := range functions {
		if !validFunction(f) || ids[f.ID] {
			return nil, errors.New("the Catalyst returned incomplete or duplicate function metadata")
		}
		ids[f.ID] = true
	}
	slices.SortFunc(functions, func(a, b FunctionMetadata) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return functions, nil
}

// GetFunction requires a matching identity. Null/404 absence is confirmed using
// parent access and a successful complete Function listing.
func (c *Client) GetFunction(ctx context.Context, projectID, id string) (*FunctionMetadata, error) {
	return readFunctionEventually(ctx, c, func(ctx context.Context) (*FunctionMetadata, error) { return c.getFunctionOnce(ctx, projectID, id) })
}
func (c *Client) getFunctionOnce(ctx context.Context, projectID, id string) (*FunctionMetadata, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid function ID")
	}
	path, err := functionPath(projectID, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, c.functionError(ctx, res, projectID, id)
	}

	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("the Catalyst returned invalid function metadata")
	}
	if string(envelope.Data) == "null" {
		return nil, c.confirmFunctionAbsent(ctx, projectID, id)
	}
	var value FunctionMetadata
	if json.Unmarshal(envelope.Data, &value) != nil || !validFunction(value) || string(value.ID) != id {
		return nil, errors.New("the Catalyst returned invalid function metadata")
	}
	return &value, nil
}

// ListFunctionIdentities supports inventory without exposing configuration secrets.
func (c *Client) ListFunctionIdentities(ctx context.Context, projectID string) ([]NamedResource, error) {
	functions, err := c.ListFunctions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]NamedResource, 0, len(functions))
	for _, f := range functions {
		result = append(result, NamedResource{ID: f.ID, Name: f.Name})
	}
	return result, nil
}
