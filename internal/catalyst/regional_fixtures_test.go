// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type regionalCheck int

const (
	regionalProjectRead regionalCheck = iota
	regionalStratusRead
	regionalStratusWrite
)

type regionalFixture struct {
	name, project, bucket, key, sha256 string
	config                             Config
}

// Validate the entire matrix before any request. Environment-specific fixtures
// never inherit another environment's credentials or global provider credentials.
func loadRegionalFixtures(getenv func(string) string, check regionalCheck) ([]regionalFixture, error) {
	selected := getenv("CATALYST_TEST_REGIONS")
	var supported []string
	for dc := range regions {
		supported = append(supported, dc)
	}
	sort.Strings(supported)
	dcs, err := regionalSelection(selected, supported)
	if err != nil {
		return nil, errors.New("CATALYST_TEST_REGIONS must be all or unique supported region codes")
	}
	environments := []string{""} // Preserve original single-environment fixture names.
	if selected := getenv("CATALYST_TEST_ENVIRONMENTS"); selected != "" {
		environments, err = regionalSelection(selected, []string{"Development", "Production"})
		if err != nil {
			return nil, errors.New("CATALYST_TEST_ENVIRONMENTS must be all or unique Development/Production values")
		}
	}
	if check == regionalStratusWrite && getenv("CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES") != "1" {
		return nil, errors.New("regional writes require CATALYST_TEST_ALLOW_REGIONAL_STRATUS_WRITES=1")
	}
	requireBoth := selected == "all" || getenv("CATALYST_TEST_REQUIRE_BOTH_AUTH_MODES") == "1"
	var fixtures []regionalFixture
	for _, dc := range dcs {
		for _, environment := range environments {
			prefix := "CATALYST_TEST_" + strings.ToUpper(dc) + "_"
			if environment != "" {
				prefix += strings.ToUpper(environment) + "_"
			} else {
				environment = getenv(prefix + "ENVIRONMENT")
				if environment == "" {
					environment = "Development"
				}
			}
			base := regionalFixture{project: getenv(prefix + "PROJECT_ID"), config: Config{
				DataCenter: dc, Environment: environment, OrganizationID: getenv(prefix + "ORGANIZATION_ID"),
				AllowedDataCenters: []string{dc}, AllowedEnvironments: []string{environment},
			}}
			if !ValidID(base.project) {
				return nil, fmt.Errorf("%sPROJECT_ID must identify an existing project", prefix)
			}
			if check != regionalProjectRead {
				base.bucket, base.key, base.sha256 = getenv(prefix+"STRATUS_BUCKET"), getenv(prefix+"STRATUS_OBJECT_KEY"), getenv(prefix+"STRATUS_SHA256")
				hash, err := hex.DecodeString(base.sha256)
				if !ValidStratusBucketName(base.bucket) || !ValidStratusObjectKey(base.key) || err != nil || len(hash) != 32 || base.sha256 != strings.ToLower(base.sha256) {
					return nil, fmt.Errorf("%s requires STRATUS_BUCKET, an existing STRATUS_OBJECT_KEY and its lowercase STRATUS_SHA256", prefix)
				}
			}
			if check == regionalStratusWrite && getenv(prefix+"ALLOW_STRATUS_WRITES") != "1" {
				return nil, fmt.Errorf("%sALLOW_STRATUS_WRITES=1 is required for a dedicated disposable-object bucket", prefix)
			}
			count := len(fixtures)
			if token := getenv(prefix + "ACCESS_TOKEN"); token != "" {
				f := base
				f.name, f.config.AccessToken = dc+"/"+environment+"/access-token", token
				fixtures = append(fixtures, f)
			}
			f := base
			f.config.ClientID, f.config.ClientSecret = getenv(prefix+"CLIENT_ID"), getenv(prefix+"CLIENT_SECRET")
			f.config.RefreshToken, f.config.RedirectURI = getenv(prefix+"REFRESH_TOKEN"), getenv(prefix+"REDIRECT_URI")
			if f.config.ClientID != "" || f.config.ClientSecret != "" || f.config.RefreshToken != "" || f.config.RedirectURI != "" {
				f.name = dc + "/" + environment + "/refresh-token"
				fixtures = append(fixtures, f)
			}
			if len(fixtures) == count || (requireBoth && len(fixtures)-count != 2) {
				modes := "access-token or complete refresh credentials"
				if requireBoth {
					modes = "both access-token and complete refresh credentials"
				}
				return nil, fmt.Errorf("%s requires %s", prefix, modes)
			}
		}
	}
	for _, f := range fixtures {
		if _, err := New(f.config, nil); err != nil {
			return nil, fmt.Errorf("invalid %s configuration: %w", f.name, err)
		}
	}
	return fixtures, nil
}

func regionalSelection(value string, supported []string) ([]string, error) {
	if value == "all" {
		return slices.Clone(supported), nil
	}
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if !slices.Contains(supported, item) || slices.Contains(result, item) {
			return nil, errors.New("invalid or duplicate selection")
		}
		result = append(result, item)
	}
	return result, nil
}
