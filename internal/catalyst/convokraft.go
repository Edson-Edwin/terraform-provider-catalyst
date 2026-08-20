// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
)

type SmartTrainMaterial struct {
	ID     ID      `json:"id"`
	Title  *string `json:"title"`
	Source *string `json:"source"`
	Status *string `json:"status"`
}

// ListSmartTrainMaterials reads existing materials for a developed ConvoKraft bot.
// The API has a dedicated envelope and requires explicit project/environment headers.
func (c *Client) ListSmartTrainMaterials(ctx context.Context, projectID, botID string) ([]SmartTrainMaterial, error) {
	if !ValidID(botID) {
		return nil, errors.New("bot ID must be a positive numeric string")
	}
	path, err := inventoryPath(projectID, "convokraft/api/v1/bots/"+botID+"/rag_training_data?type=DEVELOPED")
	if err != nil {
		return nil, err
	}
	if c.config.Environment != "Development" || c.config.OrganizationID == "" {
		return nil, errors.New("SmartTrain metadata requires the Development environment and an explicit organization")
	}
	res, err := c.requestContentForProject(ctx, http.MethodGet, path, nil, "application/json", projectID)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, statusError("Catalyst SmartTrain listing", res.StatusCode)
	}
	var result struct {
		Materials []SmartTrainMaterial `json:"rag_training_data"`
	}
	if decode(res.Body, &result) != nil || result.Materials == nil {
		return nil, errors.New("invalid Catalyst SmartTrain material listing")
	}
	seen := map[ID]bool{}
	for _, material := range result.Materials {
		if !ValidID(string(material.ID)) || seen[material.ID] {
			return nil, errors.New("the Catalyst API returned an invalid or duplicate SmartTrain material ID")
		}
		seen[material.ID] = true
	}
	slices.SortFunc(result.Materials, func(a, b SmartTrainMaterial) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return result.Materials, nil
}
