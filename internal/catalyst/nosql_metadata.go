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

// NoSQL metadata follows the public SDK getTable/getAllTable endpoints.
// Provisioning tables/indexes is not inferred from these read endpoints.
type NoSQLKey struct {
	Name string `json:"column_name"`
	Type string `json:"data_type"`
}
type NoSQLIndex struct {
	ID     ID     `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type NoSQLTable struct {
	ID           ID           `json:"id"`
	Name         string       `json:"name"`
	Status       string       `json:"status"`
	Partition    NoSQLKey     `json:"partition_key"`
	Sort         *NoSQLKey    `json:"sort_key,omitempty"`
	TTLEnabled   *bool        `json:"ttl_enabled"`
	TTLAttribute string       `json:"ttl_attribute"`
	Additional   []NoSQLKey   `json:"additional_sort_keys"`
	Local        []NoSQLIndex `json:"local_index"`
	Global       []NoSQLIndex `json:"global_index"`
}

func validNoSQLKey(k NoSQLKey) bool {
	return strings.TrimSpace(k.Name) != "" && (k.Type == "S" || k.Type == "N" || k.Type == "B")
}
func validNoSQLTable(t NoSQLTable) bool {
	if !ValidID(string(t.ID)) || t.Name == "" || !validNoSQLKey(t.Partition) || t.TTLEnabled == nil {
		return false
	}
	switch t.Status {
	case "CREATING", "ONLINE", "OFFLINE", "DELETED":
	default:
		return false
	}
	if t.Sort != nil && (!validNoSQLKey(*t.Sort) || t.Sort.Name == t.Partition.Name) {
		return false
	}
	return true
}
func (c *Client) GetNoSQLTable(ctx context.Context, project, id string) (*NoSQLTable, error) {
	if !ValidID(id) {
		return nil, errors.New("NoSQL table ID must be a positive numeric string")
	}
	base, err := inventoryPath(project, "nosqltable")
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, base+"/"+id)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, apiResponseError("NoSQL table read", res)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("invalid NoSQL table response")
	}
	var table NoSQLTable
	if json.Unmarshal(envelope.Data, &table) != nil || !validNoSQLTable(table) || string(table.ID) != id {
		return nil, errors.New("incomplete or mismatched NoSQL table metadata")
	}
	keys := map[string]bool{table.Partition.Name: true}
	if table.Sort != nil {
		keys[table.Sort.Name] = true
	}
	for _, key := range table.Additional {
		if !validNoSQLKey(key) || keys[key.Name] {
			return nil, errors.New("invalid or duplicate NoSQL key metadata")
		}
		keys[key.Name] = true
	}
	seen := map[ID]bool{}
	for _, indexes := range [][]NoSQLIndex{table.Local, table.Global} {
		for _, index := range indexes {
			if !ValidID(string(index.ID)) || index.Name == "" || index.Status == "" || seen[index.ID] {
				return nil, errors.New("invalid or duplicate NoSQL index metadata")
			}
			seen[index.ID] = true
		}
	}
	for _, indexes := range [][]NoSQLIndex{table.Local, table.Global} {
		slices.SortFunc(indexes, func(a, b NoSQLIndex) int { return strings.Compare(string(a.ID), string(b.ID)) })
	}
	slices.SortFunc(table.Additional, func(a, b NoSQLKey) int { return strings.Compare(a.Name, b.Name) })
	return &table, nil
}
func (c *Client) ListNoSQLTables(ctx context.Context, project string) ([]NamedResource, error) {
	return c.listNamed(ctx, project, "nosqltable", "id", "name")
}
