// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// DataStoreSchema deliberately excludes default values, creator profiles and rows.
// Public metadata APIs do not establish a table/column provisioning contract.
type DataStoreSchema struct {
	ID      ID                `json:"table_id"`
	Name    string            `json:"table_name"`
	Columns []DataStoreColumn `json:"columns"`
}
type DataStoreColumn struct {
	ID            ID       `json:"column_id"`
	TableID       ID       `json:"table_id"`
	Name          string   `json:"column_name"`
	Type          string   `json:"data_type"`
	Mandatory     *bool    `json:"is_mandatory"`
	Unique        *bool    `json:"is_unique"`
	SearchIndexed *bool    `json:"search_index_enabled"`
	MaxLength     *Integer `json:"max_length"`
}

func (c *Client) GetDataStoreSchema(ctx context.Context, project, table string) (*DataStoreSchema, error) {
	if !ValidID(table) {
		return nil, errors.New("data store table ID must be a positive numeric string")
	}
	base, err := inventoryPath(project, "table")
	if err != nil {
		return nil, err
	}
	base += "/" + table
	read := func(suffix string, target any) error {
		res, err := c.get(ctx, base+suffix)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return apiResponseError("Data Store schema read", res)
		}
		var e struct {
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if decode(res.Body, &e) != nil || e.Status != "success" || len(e.Data) == 0 || string(e.Data) == "null" || json.Unmarshal(e.Data, target) != nil {
			return errors.New("incomplete Data Store schema response")
		}
		return nil
	}
	var result DataStoreSchema
	if err = read("", &result); err != nil {
		return nil, err
	}
	if string(result.ID) != table || strings.TrimSpace(result.Name) == "" {
		return nil, errors.New("mismatched Data Store table metadata")
	}
	if err = read("/column", &result.Columns); err != nil {
		return nil, err
	}
	if result.Columns == nil {
		return nil, errors.New("data store schema omitted columns")
	}
	ids, names := map[ID]bool{}, map[string]bool{}
	for _, col := range result.Columns {
		if !ValidID(string(col.ID)) || col.TableID != result.ID || col.Name == "" || col.Type == "" || col.Mandatory == nil || col.Unique == nil || col.SearchIndexed == nil || ids[col.ID] || names[col.Name] {
			return nil, errors.New("incomplete or duplicate Data Store column metadata")
		}
		ids[col.ID], names[col.Name] = true, true
	}
	slices.SortFunc(result.Columns, func(a, b DataStoreColumn) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return &result, nil
}
