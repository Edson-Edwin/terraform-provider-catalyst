// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
)

// StratusCORSRule holds the fields exposed by the official JavaScript SDK.
type StratusCORSRule struct {
	Domain         string   `json:"domain"`
	AllowedMethods []string `json:"allowed_methods"`
}

// ListStratusCORS reads existing rules through the regional Catalyst API.
func (c *Client) ListStratusCORS(ctx context.Context, project, bucket string) ([]StratusCORSRule, error) {
	base, err := stratusPath(project)
	if err != nil {
		return nil, err
	}
	if !ValidStratusBucketName(bucket) {
		return nil, errors.New("bucket must be an existing lowercase DNS label")
	}
	rules, err := getList[StratusCORSRule](ctx, c, base+"/bucket/cors?"+url.Values{"bucket_name": {bucket}}.Encode())
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if strings.TrimSpace(rule.Domain) == "" || rule.AllowedMethods == nil {
			return nil, errors.New("incomplete Stratus CORS metadata")
		}
		for _, method := range rule.AllowedMethods {
			if strings.TrimSpace(method) == "" {
				return nil, errors.New("empty Stratus CORS method returned by Catalyst")
			}
		}
		slices.Sort(rule.AllowedMethods)
	}
	slices.SortFunc(rules, func(a, b StratusCORSRule) int {
		if order := strings.Compare(a.Domain, b.Domain); order != 0 {
			return order
		}
		return slices.Compare(a.AllowedMethods, b.AllowedMethods)
	})
	return rules, nil
}
