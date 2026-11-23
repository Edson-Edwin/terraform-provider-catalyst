// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const MaxAuthUsersPageSize = 200

type AuthUserAccess struct {
	ID     ID      `json:"user_id"`
	Type   *string `json:"user_type"`
	Status *string `json:"status"`
	Role   *struct {
		ID   ID      `json:"role_id"`
		Name *string `json:"role_name"`
	} `json:"role_details"`
}

// ListAuthUsersPage reads a bounded page of access metadata for an application.
func (c *Client) ListAuthUsersPage(ctx context.Context, projectID string, start, limit int64) ([]AuthUserAccess, error) {
	base, err := inventoryPath(projectID, "project-user")
	if err != nil {
		return nil, err
	}
	if start < 0 || limit < 1 || limit > MaxAuthUsersPageSize {
		return nil, errors.New("user discovery needs a non-negative offset and a page size between 1 and 200")
	}
	query := url.Values{"start": {strconv.FormatInt(start, 10)}, "end": {strconv.FormatInt(limit, 10)}}
	users, err := getList[AuthUserAccess](ctx, c, base+"?"+query.Encode())
	if err != nil {
		return nil, err
	}
	if int64(len(users)) > limit {
		return nil, errors.New("the Catalyst API returned more users than the requested page size")
	}
	seen := make(map[ID]bool, len(users))
	for _, user := range users {
		if !ValidID(string(user.ID)) || seen[user.ID] || (user.Role != nil && user.Role.ID != "" && !ValidID(string(user.Role.ID))) {
			return nil, errors.New("the Catalyst API returned invalid or duplicate application user metadata")
		}
		seen[user.ID] = true
	}
	slices.SortFunc(users, func(a, b AuthUserAccess) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return users, nil
}
