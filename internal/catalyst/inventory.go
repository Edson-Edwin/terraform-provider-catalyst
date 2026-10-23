// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
)

// NamedResource is public infrastructure metadata, without user profiles or contents.
type NamedResource struct {
	ID   ID
	Name string
}

// ListTables lists Data Store table identities, not rows or schema definitions.
func (c *Client) ListTables(ctx context.Context, projectID string) ([]NamedResource, error) {
	return c.listNamed(ctx, projectID, "table", "table_id", "table_name")
}

// ListFolders lists existing File Store folders without downloading files.
func (c *Client) ListFolders(ctx context.Context, projectID string) ([]NamedResource, error) {
	return c.listNamed(ctx, projectID, "folder", "id", "folder_name")
}

// ListCacheSegments uses the same read endpoint as the official Catalyst CLI.
// Segment creation is console-only in the public API documentation.
func (c *Client) ListCacheSegments(ctx context.Context, projectID string) ([]NamedResource, error) {
	return c.listNamed(ctx, projectID, "segment", "id", "segment_name")
}

func inventoryPath(projectID, collection string) (string, error) {
	if !ValidID(projectID) {
		return "", errors.New("project id must be a positive integer written as a string")
	}
	return "/baas/v1/project/" + projectID + "/" + collection, nil
}

func (c *Client) listNamed(ctx context.Context, projectID, collection, idKey, nameKey string) ([]NamedResource, error) {
	path, err := inventoryPath(projectID, collection)
	if err != nil {
		return nil, err
	}
	raw, err := getList[map[string]json.RawMessage](ctx, c, path)
	if err != nil {
		return nil, err
	}
	items := make([]NamedResource, 0, len(raw))
	seen := make(map[ID]bool)
	for _, item := range raw {
		var id ID
		var name string
		if json.Unmarshal(item[idKey], &id) != nil || json.Unmarshal(item[nameKey], &name) != nil || !ValidID(string(id)) || strings.TrimSpace(name) == "" || seen[id] {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate infrastructure metadata")
		}
		seen[id] = true
		items = append(items, NamedResource{ID: id, Name: name})
	}
	slices.SortFunc(items, func(a, b NamedResource) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return items, nil
}

// Bucket contains a Stratus bucket's public name and endpoint. Bucket names, not
// synthetic numeric IDs, are the documented identity for this API.
type Bucket struct {
	Name string `json:"bucket_name"`
	URL  string `json:"bucket_url"`
}

func (c *Client) ListBuckets(ctx context.Context, projectID string) ([]Bucket, error) {
	path, err := inventoryPath(projectID, "bucket")
	if err != nil {
		return nil, err
	}
	items, err := getList[Bucket](ctx, c, path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, item := range items {
		endpoint, parseErr := url.Parse(item.URL)
		if strings.TrimSpace(item.Name) == "" || seen[item.Name] || parseErr != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return nil, errors.New("the Catalyst API returned incomplete or duplicate bucket metadata")
		}
		seen[item.Name] = true
	}
	slices.SortFunc(items, func(a, b Bucket) int { return strings.Compare(a.Name, b.Name) })
	return items, nil
}
