// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"errors"
	"slices"
)

// regionalEndpoints is the authoritative routing metadata for one data center.
// Sources: https://docs.catalyst.zoho.com/en/api/introduction/multi-dc-support/
// and https://accounts.zoho.com/oauth/serverinfo (canonical OAuth hostnames).
type regionalEndpoints struct {
	api, accounts, stratusSuffix string
}

var regions = map[string]regionalEndpoints{
	"us": {api: "https://api.catalyst.zoho.com", accounts: "https://accounts.zoho.com", stratusSuffix: "com"},
	"eu": {api: "https://api.catalyst.zoho.eu", accounts: "https://accounts.zoho.eu", stratusSuffix: "eu"},
	"in": {api: "https://api.catalyst.zoho.in", accounts: "https://accounts.zoho.in", stratusSuffix: "in"},
	"au": {api: "https://api.catalyst.zoho.com.au", accounts: "https://accounts.zoho.com.au", stratusSuffix: "com.au"},
	"ca": {api: "https://api.catalyst.zohocloud.ca", accounts: "https://accounts.zohocloud.ca", stratusSuffix: "ca"},
	"jp": {api: "https://api.catalyst.zoho.jp", accounts: "https://accounts.zoho.jp", stratusSuffix: "jp"},
	"sa": {api: "https://api.catalyst.zoho.sa", accounts: "https://accounts.zoho.sa", stratusSuffix: "sa"},
}

// Validate before applying defaults: a residency policy must never approve an
// accidental default destination. These are local guardrails, not authorization.
func validateRegionalPolicy(config Config) error {
	if len(config.AllowedDataCenters) > 0 {
		if config.DataCenter == "" {
			return errors.New("allowed_data_centers requires an explicit data_center or CATALYST_DATA_CENTER")
		}
		for _, dc := range config.AllowedDataCenters {
			if _, ok := regions[dc]; !ok {
				return errors.New("allowed_data_centers must contain only supported data center codes")
			}
		}
		if !slices.Contains(config.AllowedDataCenters, config.DataCenter) {
			return errors.New("configured data_center is not in allowed_data_centers")
		}
	}
	if len(config.AllowedEnvironments) > 0 {
		if config.Environment == "" {
			return errors.New("allowed_environments requires an explicit environment or CATALYST_ENVIRONMENT")
		}
		for _, environment := range config.AllowedEnvironments {
			if environment != "Development" && environment != "Production" {
				return errors.New("allowed_environments must contain only Development or Production")
			}
		}
		if !slices.Contains(config.AllowedEnvironments, config.Environment) {
			return errors.New("configured environment is not in allowed_environments")
		}
	}
	return nil
}
