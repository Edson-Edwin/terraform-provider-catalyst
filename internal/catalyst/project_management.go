// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrProjectNotFound = errors.New("the project is absent after successful account listings")

func ValidProjectName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 50 && strings.IndexFunc(name, unicode.IsControl) < 0
}

// CreateProject uses the public project endpoint. A successful identity is returned
// even if the remaining response cannot be decoded, so Terraform can retain it.
func (c *Client) CreateProject(ctx context.Context, name string) (*Project, error) {
	if !ValidProjectName(name) {
		return nil, errors.New("project name must contain 1–50 characters without surrounding whitespace or control characters")
	}
	select {
	case c.deploymentWrites <- struct{}{}:
		defer func() { <-c.deploymentWrites }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.Name == name {
			return nil, errors.New("project name already exists; import the existing project explicitly")
		}
	}
	body, _ := json.Marshal(map[string]string{"project_name": name})
	res, err := c.request(ctx, http.MethodPost, "/baas/v1/project", body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 201 {
		return nil, apiResponseError("Catalyst project creation", res)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("invalid project creation response")
	}
	var identity struct {
		ID ID `json:"id"`
	}
	if json.Unmarshal(envelope.Data, &identity) != nil || !ValidID(string(identity.ID)) {
		return nil, errors.New("project creation omitted a valid project ID; inspect the account before retrying")
	}
	value := &Project{ID: identity.ID}
	if json.Unmarshal(envelope.Data, value) != nil || value.Name != name {
		return &Project{ID: identity.ID}, errors.New("incomplete project creation metadata; created ID retained for recovery")
	}
	return value, nil
}

// Missing project access must not be inferred from a permission or malformed response.
func (c *Client) GetManagedProject(ctx context.Context, id string) (*Project, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid project ID")
	}
	res, err := c.get(ctx, "/baas/v1/project/"+id)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 404 {
		return readProject(res, id)
	}
	var failure apiFailure
	if decode(res.Body, &failure) != nil || failure.Status != "failure" || failure.Data.Code != "INVALID_ID" {
		return nil, failureError("Catalyst project lookup", 404, failure)
	}
	for attempt := 0; attempt < 3; attempt++ {
		projects, err := c.ListProjects(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			if string(p.ID) == id {
				return nil, errors.New("project lookup and listing disagree; state retained")
			}
		}
		if attempt < 2 {
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return nil, err
			}
		}
	}
	return nil, ErrProjectNotFound
}

// DeleteProject deletes only the immutable ID after checking its expected name.
// It never retries an ambiguous write or deletes projects found only by name.
func (c *Client) DeleteProject(ctx context.Context, id, expectedName string) error {
	current, err := c.GetManagedProject(ctx, id)
	if err != nil {
		return err
	}
	if !ValidProjectName(expectedName) || current.Name != expectedName {
		return errors.New("project identity changed; refresh the plan before deletion")
	}
	body, _ := json.Marshal(map[string]string{"project_name": expectedName})
	res, err := c.request(ctx, http.MethodDelete, "/baas/v1/project/"+id, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return apiResponseError("Catalyst project deletion", res)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ID ID `json:"id"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || string(envelope.Data.ID) != id {
		return errors.New("project deletion did not confirm the requested ID")
	}
	return nil
}
func (c *Client) WaitProjectDeleted(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := c.GetManagedProject(ctx, id)
		if errors.Is(err, ErrProjectNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}
