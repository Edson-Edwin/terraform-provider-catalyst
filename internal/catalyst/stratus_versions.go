// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type StratusVersion struct {
	ID           string
	Latest       bool
	Size         *Integer
	ETag         *string
	LastModified *string
}

// Zoho's examples use both field names and both JSON booleans and quoted values.
func stratusVersionFlag(preferred, alternate json.RawMessage) (bool, error) {
	parse := func(raw json.RawMessage) (bool, error) {
		value := string(raw)
		if len(raw) > 0 && raw[0] == '"' {
			if err := json.Unmarshal(raw, &value); err != nil {
				return false, errors.New("invalid Stratus version flag")
			}
		}
		switch strings.ToLower(value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return false, errors.New("invalid or missing Stratus version flag")
		}
	}
	if len(preferred) == 0 {
		return parse(alternate)
	}
	value, err := parse(preferred)
	if err != nil {
		return false, err
	}
	if len(alternate) > 0 {
		other, err := parse(alternate)
		if err != nil || value != other {
			return false, errors.New("conflicting Stratus version flags")
		}
	}
	return value, nil
}

const stratusVersionsPageSize = 100
const stratusVersionsMaxPages = 100

// ListStratusObjectVersions returns the complete version listing for one key.
// It preserves the service's version identities and sorts them for stable state.
func (c *Client) ListStratusObjectVersions(ctx context.Context, project, bucket, key string) ([]StratusVersion, error) {
	base, err := stratusPath(project)
	if err != nil {
		return nil, err
	}
	if !ValidStratusBucketName(bucket) || !ValidStratusObjectKey(key) {
		return nil, errors.New("invalid Stratus bucket name or object key")
	}
	versions := []StratusVersion{}
	seenIDs, seenTokens := map[string]bool{}, map[string]bool{}
	query := url.Values{"bucket_name": {bucket}, "object_key": {key}, "max_versions": {strconv.Itoa(stratusVersionsPageSize)}}
	for range stratusVersionsMaxPages {
		res, err := c.get(ctx, base+"/bucket/objects/versions?"+query.Encode())
		if err != nil {
			return nil, err
		}
		var page struct {
			Key         string          `json:"key"`
			IsTruncated json.RawMessage `json:"is_truncated"`
			Truncated   json.RawMessage `json:"truncated"`
			Next        string          `json:"next_continuation_token"`
			Versions    []struct {
				ID           string          `json:"version_id"`
				IsLatest     json.RawMessage `json:"is_latest"`
				Latest       json.RawMessage `json:"latest"`
				Size         *Integer        `json:"size"`
				ETag         *string         `json:"etag"`
				LastModified *string         `json:"last_modified"`
			} `json:"version"`
		}
		err = stratusData(res, &page)
		_ = res.Body.Close()
		if err != nil {
			return nil, err
		}
		if page.Key != key || page.Versions == nil || len(page.Versions) > stratusVersionsPageSize {
			return nil, errors.New("unexpected Stratus object version listing")
		}
		for _, item := range page.Versions {
			if strings.TrimSpace(item.ID) == "" || seenIDs[item.ID] {
				return nil, errors.New("missing or duplicate Stratus object version ID")
			}
			if item.Size != nil && *item.Size < 0 {
				return nil, errors.New("negative Stratus object version size")
			}
			latest, err := stratusVersionFlag(item.IsLatest, item.Latest)
			if err != nil {
				return nil, err
			}
			seenIDs[item.ID] = true
			versions = append(versions, StratusVersion{
				ID: item.ID, Latest: latest, Size: item.Size, ETag: item.ETag, LastModified: item.LastModified,
			})
		}
		truncated, err := stratusVersionFlag(page.IsTruncated, page.Truncated)
		if err != nil {
			return nil, err
		}
		if !truncated {
			slices.SortFunc(versions, func(a, b StratusVersion) int { return strings.Compare(a.ID, b.ID) })
			return versions, nil
		}
		if strings.TrimSpace(page.Next) == "" || seenTokens[page.Next] {
			return nil, errors.New("missing or repeated Stratus version continuation token")
		}
		seenTokens[page.Next] = true
		query.Set("continuation_token", page.Next)
	}
	return nil, errors.New("the Stratus object version listing exceeded 100 pages")
}
