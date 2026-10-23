// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

var ErrAuthUserNotFound = errors.New("the application user no longer exists in the selected project")

// AuthUser exposes access metadata only. Email is used internally because the
// official updateUserDetails API requires it, but is never returned to Terraform.
type AuthUser struct {
	ID, RoleID                 ID
	RoleName, UserType, Status string
	email                      string
}

func authUserPath(projectID, userID string) (string, error) {
	if !ValidID(projectID) || !ValidID(userID) {
		return "", errors.New("project and user IDs must be positive integers written as strings")
	}
	return "/baas/v1/project/" + projectID + "/project-user/" + userID, nil
}

// GetAuthUser uses the documented specific-user API. A 404 is treated as a
// missing user only after the parent project remains readable. Other failures
// preserve ownership rather than silently forgetting an access assignment.
func (c *Client) GetAuthUser(ctx context.Context, projectID, userID string) (*AuthUser, error) {
	path, err := authUserPath(projectID, userID)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		if _, err := c.GetProject(ctx, projectID); err != nil {
			return nil, err
		}
		return nil, ErrAuthUserNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst application user read", res)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   *struct {
			ID       ID     `json:"user_id"`
			Email    string `json:"email_id"`
			UserType string `json:"user_type"`
			Status   string `json:"status"`
			Role     struct {
				ID   ID     `json:"role_id"`
				Name string `json:"role_name"`
			} `json:"role_details"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || envelope.Data == nil {
		return nil, errors.New("the Catalyst API returned an invalid application user response")
	}
	d := envelope.Data
	if string(d.ID) != userID || !ValidID(string(d.Role.ID)) || strings.TrimSpace(d.Role.Name) == "" || strings.TrimSpace(d.UserType) == "" || strings.TrimSpace(d.Status) == "" || d.Email == "" {
		return nil, errors.New("the Catalyst API returned incomplete or mismatched application user metadata")
	}
	return &AuthUser{ID: d.ID, RoleID: d.Role.ID, RoleName: d.Role.Name, UserType: d.UserType, Status: d.Status, email: d.Email}, nil
}

// SetAuthUserRole assigns an existing role to an existing end-user using the
// official SDK's PUT /project-user/{id} contract. It neither invites nor deletes
// users, changes their profile, nor changes the permissions of the role itself.
func (c *Client) SetAuthUserRole(ctx context.Context, projectID, userID, roleID string) (*AuthUser, error) {
	path, err := authUserPath(projectID, userID)
	if err != nil {
		return nil, err
	}
	if !ValidID(roleID) {
		return nil, errors.New("role ID must be a positive integer written as a string")
	}
	u, err := c.GetAuthUser(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	// Never modify a project administrator/collaborator through an end-user
	// assignment resource. Existing end-users and their roles are prerequisites.
	if u.UserType != "App User" {
		return nil, errors.New("role assignment is limited to application end-users; administrator and collaborator accounts cannot be managed")
	}
	if string(u.RoleID) == roleID {
		return u, nil
	}
	body, err := json.Marshal(struct {
		Email  string `json:"email_id"`
		RoleID string `json:"role_id"`
	}{u.email, roleID})
	if err != nil {
		return nil, errors.New("could not encode the application user role assignment")
	}
	res, err := c.request(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst application user role update", res)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, errors.New("the Catalyst API did not confirm the application user role update; inspect the user before retrying")
	}
	updated, err := c.GetAuthUser(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	if string(updated.RoleID) != roleID {
		return nil, errors.New("the Catalyst API did not persist the requested role; inspect the application user before retrying")
	}
	return updated, nil
}
