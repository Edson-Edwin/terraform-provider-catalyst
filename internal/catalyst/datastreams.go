// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
)

type DataStreamChannel struct {
	ID             ID       `json:"id"`
	ProjectID      ID       `json:"project_id"`
	Name           string   `json:"name"`
	ControlType    *Integer `json:"control_type"`
	RetentionHours *Integer `json:"retention"`
}

// ListDataStreamChannels returns channel configuration without reading stream events.
func (c *Client) ListDataStreamChannels(ctx context.Context, projectID string) ([]DataStreamChannel, error) {
	path, err := inventoryPath(projectID, "datastreams/channel")
	if err != nil {
		return nil, err
	}
	channels, err := getList[DataStreamChannel](ctx, c, path)
	if err != nil {
		return nil, err
	}
	seen := make(map[ID]bool, len(channels))
	for _, channel := range channels {
		if !channel.matchesProject(projectID) || seen[channel.ID] {
			return nil, errors.New("the Catalyst API returned incomplete, duplicate or mismatched Data Streams channel metadata")
		}
		seen[channel.ID] = true
	}
	slices.SortFunc(channels, func(a, b DataStreamChannel) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return channels, nil
}

// GetDataStreamChannel reads one exact channel and checks both returned identities.
func (c *Client) GetDataStreamChannel(ctx context.Context, projectID, channelID string) (*DataStreamChannel, error) {
	if !ValidID(channelID) {
		return nil, errors.New("channel ID must be a positive numeric string")
	}
	path, err := inventoryPath(projectID, "datastreams/channel/"+channelID)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("Catalyst Data Streams channel lookup", res)
	}
	var result struct {
		Status string             `json:"status"`
		Data   *DataStreamChannel `json:"data"`
	}
	if decode(res.Body, &result) != nil || result.Status != "success" || result.Data == nil ||
		string(result.Data.ID) != channelID || !result.Data.matchesProject(projectID) {
		return nil, errors.New("the Catalyst API returned incomplete or mismatched Data Streams channel metadata")
	}
	return result.Data, nil
}

func (channel DataStreamChannel) matchesProject(projectID string) bool {
	return string(channel.ProjectID) == projectID && ValidID(string(channel.ID)) &&
		strings.TrimSpace(channel.Name) != "" &&
		(channel.RetentionHours == nil || *channel.RetentionHours >= 0)
}
