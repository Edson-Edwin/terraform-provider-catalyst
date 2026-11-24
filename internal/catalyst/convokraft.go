// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
)

var ErrSmartTrainMaterialNotFound = errors.New("SmartTrain material is absent from the bot inventory")

type SmartTrainMaterial struct {
	ID     ID      `json:"id"`
	Title  *string `json:"title"`
	Source *string `json:"source"`
	Status *string `json:"status"`
	URL    string  `json:"url"`
}

func (c *Client) smartTrainRequest(ctx context.Context, method, projectID, botID string, payload []byte) (*http.Response, error) {
	if !ValidID(botID) {
		return nil, errors.New("bot ID must be a positive numeric string")
	}
	path, err := inventoryPath(projectID, "convokraft/api/v1/bots/"+botID+"/rag_training_data?type=DEVELOPED")
	if err != nil {
		return nil, err
	}
	if c.config.Environment != "Development" || c.config.OrganizationID == "" {
		return nil, errors.New("SmartTrain requires the Development environment and an explicit organization")
	}
	return c.requestContentForProject(ctx, method, path, payload, "application/json", projectID)
}

// ListSmartTrainMaterials reads existing materials for a developed ConvoKraft bot.
// The API has a dedicated envelope and requires explicit project/environment headers.
func (c *Client) ListSmartTrainMaterials(ctx context.Context, projectID, botID string) ([]SmartTrainMaterial, error) {
	res, err := c.smartTrainRequest(ctx, http.MethodGet, projectID, botID, nil)
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

func (c *Client) GetSmartTrainMaterial(ctx context.Context, projectID, botID, id string) (*SmartTrainMaterial, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid SmartTrain material ID")
	}
	items, err := c.ListSmartTrainMaterials(ctx, projectID, botID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if string(item.ID) == id {
			return &item, nil
		}
	}
	return nil, ErrSmartTrainMaterialNotFound
}

// SmartTrainURLMatches accepts the trailing slash Catalyst adds in its REST examples.
func SmartTrainURLMatches(requested, observed string) bool {
	return requested == observed || (!strings.Contains(requested, "?") && !strings.HasSuffix(requested, "/") && requested+"/" == observed)
}

// CreateSmartTrainMaterial serializes duplicate checks, creation and visibility
// within this client. If Catalyst returned a fresh ID, it is retained on errors.
func (c *Client) CreateSmartTrainMaterial(ctx context.Context, projectID, botID, url, title string) (*SmartTrainMaterial, error) {
	select {
	case c.deploymentWrites <- struct{}{}:
		defer func() { <-c.deploymentWrites }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	items, err := c.ListSmartTrainMaterials(ctx, projectID, botID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if SmartTrainURLMatches(url, item.URL) || SmartTrainURLMatches(item.URL, url) {
			return nil, errors.New("a material already uses this URL; import the existing record by ID to manage it")
		}
	}
	created, err := c.addSmartTrainMaterial(ctx, projectID, botID, url, title)
	if created != nil && ((created.URL != "" && !SmartTrainURLMatches(url, created.URL)) || slices.ContainsFunc(items, func(item SmartTrainMaterial) bool { return item.ID == created.ID })) {
		return nil, errors.New("the Catalyst API returned an existing material ID or a different source URL; review the bot inventory before retrying")
	}
	if err != nil {
		return created, err
	}
	item, err := c.WaitSmartTrainMaterial(ctx, projectID, botID, string(created.ID), true)
	if err != nil {
		return created, err
	}
	return item, nil
}

func (c *Client) addSmartTrainMaterial(ctx context.Context, projectID, botID, url, title string) (*SmartTrainMaterial, error) {
	body, err := json.Marshal(map[string][]map[string]string{"rag_training_data": {{"url": url, "title": title}}})
	if err != nil {
		return nil, errors.New("could not encode SmartTrain material")
	}
	res, err := c.smartTrainRequest(ctx, http.MethodPost, projectID, botID, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusAccepted {
		return nil, statusError("Catalyst SmartTrain creation", res.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Code   string `json:"code"`
		Info   struct {
			Files []SmartTrainMaterial `json:"files"`
		} `json:"info"`
	}
	if decode(res.Body, &result) != nil || len(result.Info.Files) != 1 || !ValidID(string(result.Info.Files[0].ID)) {
		return nil, errors.New("the Catalyst API did not confirm a single created material ID; check the bot inventory before retrying")
	}
	if result.Status != "SUCCESS" || result.Code != "SUCCESS" {
		return &result.Info.Files[0], errors.New("the Catalyst API reported unsuccessful SmartTrain creation")
	}
	return &result.Info.Files[0], nil
}

func (c *Client) DeleteSmartTrainMaterial(ctx context.Context, projectID, botID, id string) error {
	if !ValidID(id) {
		return errors.New("invalid SmartTrain material ID")
	}
	body, err := json.Marshal(map[string][]map[string]string{"rag_training_data": {{"id": id}}})
	if err != nil {
		return errors.New("could not encode SmartTrain material identity")
	}
	res, err := c.smartTrainRequest(ctx, http.MethodDelete, projectID, botID, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return statusError("Catalyst SmartTrain deletion", res.StatusCode)
	}
	var result struct{ Status, Code string }
	if decode(res.Body, &result) != nil || result.Status != "SUCCESS" || result.Code != "SUCCESS" {
		return errors.New("the Catalyst API did not confirm the SmartTrain material deletion request")
	}
	return nil
}

// WaitSmartTrainMaterial waits for the inventory to reflect creation or deletion.
// Visibility of the record does not indicate training completion.
func (c *Client) WaitSmartTrainMaterial(ctx context.Context, projectID, botID, id string, exists bool) (*SmartTrainMaterial, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		item, err := c.GetSmartTrainMaterial(ctx, projectID, botID, id)
		if err != nil && !errors.Is(err, ErrSmartTrainMaterialNotFound) {
			return nil, err
		}
		if (item != nil) == exists {
			return item, nil
		}
		if c.wait(ctx, 2*time.Second) != nil {
			return nil, errors.New("the Catalyst API did not confirm visibility or absence of the SmartTrain material before the deadline")
		}
	}
}
