// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

var ErrNoSQLItemNotFound = errors.New("the NoSQL configuration item is absent in a consistent table read")

const MaxNoSQLConfigBytes = 256 * 1024

var noSQLNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// Canonical scientific notation preserves arbitrary precision without expanding
// exponents into potentially huge decimal strings.
func canonicalNoSQLNumber(value string) string {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign = "-"
		value = value[1:]
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == 'e' || r == 'E' })
	whole, fraction, _ := strings.Cut(parts[0], ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}
	var power big.Int
	if len(parts) > 1 {
		power.SetString(parts[1], 10)
	}
	power.Sub(&power, big.NewInt(int64(len(fraction))))
	trimmed := strings.TrimRight(digits, "0")
	power.Add(&power, big.NewInt(int64(len(digits)-len(trimmed))))
	return sign + trimmed + "e" + power.String()
}

// ParseNoSQLItem validates Catalyst's typed JSON without converting numeric values
// to float64. Configuration items, including keys, remain in Terraform state.
func ParseNoSQLItem(value string) (map[string]json.RawMessage, error) {
	if len(value) > MaxNoSQLConfigBytes {
		return nil, errors.New("NoSQL configuration JSON exceeds the provider's 256 KiB limit")
	}
	var item map[string]json.RawMessage
	if !uniqueJSONKeys(value) || json.Unmarshal([]byte(value), &item) != nil || len(item) == 0 {
		return nil, errors.New("use a nonempty Catalyst typed JSON object")
	}
	for name, raw := range item {
		if name == "" || !validNoSQLAttribute(raw, 0) {
			return nil, errors.New("invalid NoSQL attribute name or typed JSON value")
		}
	}
	return item, nil
}
func validNoSQLAttribute(raw json.RawMessage, depth int) bool {
	if depth > 32 {
		return false
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || len(value) != 1 {
		return false
	}
	for kind, data := range value {
		switch kind {
		case "S", "N", "B":
			var text string
			if json.Unmarshal(data, &text) != nil {
				return false
			}
			if kind == "N" && !noSQLNumber.MatchString(text) {
				return false
			}
			if kind == "B" {
				if _, err := base64.StdEncoding.DecodeString(text); err != nil {
					return false
				}
			}
		case "BOOL", "NULL":
			var flag *bool
			if json.Unmarshal(data, &flag) != nil || flag == nil || (kind == "NULL" && !*flag) {
				return false
			}
		case "M":
			var m map[string]json.RawMessage
			if json.Unmarshal(data, &m) != nil || m == nil {
				return false
			}
			for key, v := range m {
				if key == "" || !validNoSQLAttribute(v, depth+1) {
					return false
				}
			}
		case "L":
			var list []json.RawMessage
			if json.Unmarshal(data, &list) != nil || list == nil {
				return false
			}
			for _, v := range list {
				if !validNoSQLAttribute(v, depth+1) {
					return false
				}
			}
		case "SS", "SN", "SB":
			var values []string
			if json.Unmarshal(data, &values) != nil || len(values) == 0 {
				return false
			}
			seen := map[string]bool{}
			for _, v := range values {
				if kind == "SN" && !noSQLNumber.MatchString(v) {
					return false
				}
				if kind == "SB" {
					decoded, err := base64.StdEncoding.DecodeString(v)
					if err != nil {
						return false
					}
					v = base64.StdEncoding.EncodeToString(decoded)
				}
				if kind == "SN" {
					v = canonicalNoSQLNumber(v)
				}
				if seen[v] {
					return false
				}
				seen[v] = true
			}
		default:
			return false
		}
	}
	return true
}
func CanonicalNoSQLItem(raw string) (string, error) {
	item, err := ParseNoSQLItem(raw)
	if err != nil {
		return "", err
	}
	var normalize func(json.RawMessage) json.RawMessage
	normalize = func(raw json.RawMessage) json.RawMessage {
		var attribute map[string]json.RawMessage
		_ = json.Unmarshal(raw, &attribute)
		for kind, value := range attribute {
			switch kind {
			case "N":
				var v string
				_ = json.Unmarshal(value, &v)
				attribute[kind], _ = json.Marshal(canonicalNoSQLNumber(v))
			case "B":
				var v string
				_ = json.Unmarshal(value, &v)
				decoded, _ := base64.StdEncoding.DecodeString(v)
				attribute[kind], _ = json.Marshal(base64.StdEncoding.EncodeToString(decoded))
			case "SS", "SN", "SB":
				var values []string
				_ = json.Unmarshal(value, &values)
				for i, v := range values {
					if kind == "SN" {
						values[i] = canonicalNoSQLNumber(v)
					}
					if kind == "SB" {
						decoded, _ := base64.StdEncoding.DecodeString(v)
						values[i] = base64.StdEncoding.EncodeToString(decoded)
					}
				}
				slices.Sort(values)
				attribute[kind], _ = json.Marshal(values)
			case "L":
				var values []json.RawMessage
				_ = json.Unmarshal(value, &values)
				for i := range values {
					values[i] = normalize(values[i])
				}
				attribute[kind], _ = json.Marshal(values)
			case "M":
				var values map[string]json.RawMessage
				_ = json.Unmarshal(value, &values)
				for key, v := range values {
					values[key] = normalize(v)
				}
				attribute[kind], _ = json.Marshal(values)
			}
		}
		result, _ := json.Marshal(attribute)
		return result
	}
	for key, value := range item {
		item[key] = normalize(value)
	}
	encoded, _ := json.Marshal(item)
	return string(encoded), nil
}
func validateNoSQLKeys(table *NoSQLTable, keys map[string]json.RawMessage) error {
	expected := []NoSQLKey{table.Partition}
	if table.Sort != nil {
		expected = append(expected, *table.Sort)
	}
	if len(keys) != len(expected) {
		return errors.New("keys_json must contain exactly the table partition and optional sort key")
	}
	for _, key := range expected {
		var attribute map[string]json.RawMessage
		if json.Unmarshal(keys[key.Name], &attribute) != nil || len(attribute) != 1 || attribute[key.Type] == nil {
			return errors.New("key attributes must match the table names and S/N/B types")
		}
		var value string
		if json.Unmarshal(attribute[key.Type], &value) != nil || value == "" {
			return errors.New("key attributes must have nonempty values")
		}
	}
	return nil
}
func noSQLValuesEqual(a, b json.RawMessage) bool {
	x, errA := CanonicalNoSQLItem(`{"v":` + string(a) + `}`)
	y, errB := CanonicalNoSQLItem(`{"v":` + string(b) + `}`)
	return errA == nil && errB == nil && x == y
}

type noSQLEntry struct {
	Status string                     `json:"status"`
	Item   map[string]json.RawMessage `json:"item"`
}
type noSQLResult struct {
	Size   *Integer     `json:"size"`
	Get    []noSQLEntry `json:"get"`
	Create []noSQLEntry `json:"create"`
	Update []noSQLEntry `json:"update"`
	Delete []noSQLEntry `json:"delete"`
}

// The public API returns BOOL values as strings, including inside maps/lists,
// although requests use JSON booleans. Keep that wire difference out of state.
func normalizeNoSQLReadAttribute(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeNoSQLReadDepth(raw, 0)
}
func normalizeNoSQLReadDepth(raw json.RawMessage, depth int) (json.RawMessage, error) {
	if depth > 32 {
		return nil, errors.New("NoSQL response exceeds the supported nesting depth")
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || len(value) != 1 {
		return nil, errors.New("invalid NoSQL read attribute")
	}
	for kind, data := range value {
		switch kind {
		case "BOOL", "NULL":
			var text string
			if json.Unmarshal(data, &text) == nil {
				if text != "true" && text != "false" {
					return nil, errors.New("invalid NoSQL boolean response")
				}
				value[kind] = json.RawMessage(text)
			}
		case "M":
			var children map[string]json.RawMessage
			if json.Unmarshal(data, &children) != nil || children == nil {
				return nil, errors.New("invalid NoSQL map response")
			}
			for key, child := range children {
				normalized, err := normalizeNoSQLReadDepth(child, depth+1)
				if err != nil {
					return nil, err
				}
				children[key] = normalized
			}
			value[kind], _ = json.Marshal(children)
		case "L":
			var children []json.RawMessage
			if json.Unmarshal(data, &children) != nil || children == nil {
				return nil, errors.New("invalid NoSQL list response")
			}
			for i, child := range children {
				normalized, err := normalizeNoSQLReadDepth(child, depth+1)
				if err != nil {
					return nil, err
				}
				children[i] = normalized
			}
			value[kind], _ = json.Marshal(children)
		}
	}
	result, _ := json.Marshal(value)
	return result, nil
}

func (c *Client) noSQLItemRequest(ctx context.Context, project, table, method, suffix string, payload any) (*noSQLResult, error) {
	if !ValidID(table) {
		return nil, errors.New("invalid NoSQL table ID")
	}
	base, err := inventoryPath(project, "nosqltable")
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("could not encode NoSQL request")
	}
	res, err := c.request(ctx, method, base+"/"+table+"/item"+suffix, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, apiResponseError("NoSQL item request", res)
	}
	var envelope struct {
		Status string       `json:"status"`
		Data   *noSQLResult `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || envelope.Data == nil || envelope.Data.Size == nil {
		return nil, errors.New("incomplete NoSQL operation response")
	}
	return envelope.Data, nil
}
func (c *Client) fetchNoSQLItem(ctx context.Context, project, table, keyJSON string) (map[string]json.RawMessage, error) {
	keys, err := ParseNoSQLItem(keyJSON)
	if err != nil {
		return nil, err
	}
	metadata, err := c.GetNoSQLTable(ctx, project, table)
	if err != nil {
		return nil, err
	}
	if metadata.Status != "ONLINE" {
		return nil, errors.New("NoSQL table must be ONLINE")
	}
	if err = validateNoSQLKeys(metadata, keys); err != nil {
		return nil, err
	}
	result, err := c.noSQLItemRequest(ctx, project, table, http.MethodPost, "/fetch", map[string]any{"keys": []any{keys}, "consistent_read": true})
	if err != nil {
		return nil, err
	}
	if *result.Size == 0 && len(result.Get) == 0 {
		return nil, ErrNoSQLItemNotFound
	}
	// Catalyst reports item size in bytes, not the number of returned items.
	if *result.Size <= 0 || len(result.Get) != 1 || len(result.Get[0].Item) == 0 {
		return nil, errors.New("NoSQL read omitted or duplicated the requested item")
	}
	item := result.Get[0].Item
	for key, value := range keys {
		if !noSQLValuesEqual(item[key], value) {
			return nil, errors.New("NoSQL read returned mismatched key attributes")
		}
	}
	return item, nil
}
func (c *Client) ReadNoSQLItem(ctx context.Context, project, table, keyJSON string) (string, error) {
	item, err := c.fetchNoSQLItem(ctx, project, table, keyJSON)
	if err != nil {
		return "", err
	}
	for key, value := range item {
		normalized, err := normalizeNoSQLReadAttribute(value)
		if err != nil {
			return "", err
		}
		item[key] = normalized
	}
	raw, _ := json.Marshal(item)
	return CanonicalNoSQLItem(string(raw))
}

// WriteNoSQLItem checks per-item status, not only HTTP 200. Creation uses an
// atomic condition that fails for an existing item with the same partition key.
func (c *Client) WriteNoSQLItem(ctx context.Context, project, table, keyJSON, itemJSON string, creating bool) (bool, error) {
	keys, err := ParseNoSQLItem(keyJSON)
	if err != nil {
		return false, err
	}
	item, err := ParseNoSQLItem(itemJSON)
	if err != nil {
		return false, err
	}
	metadata, err := c.GetNoSQLTable(ctx, project, table)
	if err != nil {
		return false, err
	}
	if metadata.Status != "ONLINE" {
		return false, errors.New("NoSQL table must be ONLINE")
	}
	if err = validateNoSQLKeys(metadata, keys); err != nil {
		return false, err
	}
	for key, value := range keys {
		if !noSQLValuesEqual(item[key], value) {
			return false, errors.New("item_json must contain the exact configured primary key values")
		}
	}
	var result *noSQLResult
	var entries []noSQLEntry
	if creating {
		_, err = c.ReadNoSQLItem(ctx, project, table, keyJSON)
		if err == nil {
			return false, errors.New("NoSQL item already exists; import it explicitly")
		}
		if !errors.Is(err, ErrNoSQLItemNotFound) {
			return false, err
		}
		condition := map[string]any{"attribute": []string{metadata.Partition.Name}, "operator": "not_equals", "value": keys[metadata.Partition.Name]}
		result, err = c.noSQLItemRequest(ctx, project, table, http.MethodPost, "", []any{map[string]any{"item": item, "condition": condition}})
		if err == nil {
			entries = result.Create
		}
	} else {
		current, readErr := c.ReadNoSQLItem(ctx, project, table, keyJSON)
		if readErr != nil {
			return false, readErr
		}
		previous, _ := ParseNoSQLItem(current)
		operations := []any{}
		for key, value := range item {
			if _, primary := keys[key]; !primary {
				operations = append(operations, map[string]any{"operation_type": "PUT", "attribute_path": []string{key}, "update_value": value})
			}
		}
		for key := range previous {
			if _, primary := keys[key]; primary {
				continue
			}
			if _, present := item[key]; !present {
				operations = append(operations, map[string]any{"operation_type": "DELETE", "attribute_path": []string{key}})
			}
		}
		if len(operations) == 0 {
			return true, nil
		}
		result, err = c.noSQLItemRequest(ctx, project, table, http.MethodPut, "", []any{map[string]any{"keys": keys, "update_attributes": operations, "return": "NEW"}})
		if err == nil {
			entries = result.Update
		}
	}
	if err != nil {
		return false, err
	}
	if len(entries) != 1 || entries[0].Status != "Success" {
		return false, errors.New("NoSQL item write was not confirmed; a condition may have failed")
	}
	return true, nil
}
func (c *Client) DeleteNoSQLItem(ctx context.Context, project, table, keyJSON string) error {
	_, err := c.fetchNoSQLItem(ctx, project, table, keyJSON)
	if errors.Is(err, ErrNoSQLItemNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	keys, _ := ParseNoSQLItem(keyJSON)
	result, err := c.noSQLItemRequest(ctx, project, table, http.MethodDelete, "", []any{map[string]any{"keys": keys}})
	if err != nil {
		return err
	}
	if len(result.Delete) != 1 || result.Delete[0].Status != "Success" {
		return errors.New("NoSQL item deletion was not confirmed")
	}
	return c.WaitNoSQLItem(ctx, project, table, keyJSON, "", true)
}
func (c *Client) WaitNoSQLItem(ctx context.Context, project, table, keys, want string, deleted bool) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	expected := ""
	if !deleted {
		var err error
		expected, err = CanonicalNoSQLItem(want)
		if err != nil {
			return err
		}
	}
	stable := 0
	for {
		actual, err := c.ReadNoSQLItem(ctx, project, table, keys)
		match := deleted && errors.Is(err, ErrNoSQLItemNotFound) || !deleted && err == nil && actual == expected
		if err != nil && !errors.Is(err, ErrNoSQLItemNotFound) {
			return err
		}
		if match {
			stable++
		} else {
			stable = 0
		}
		if stable == 3 {
			return nil
		}
		if err = c.wait(ctx, time.Second); err != nil {
			return err
		}
	}
}
func NoSQLItemIdentity(project, table, keys string) (string, error) {
	canonical, err := CanonicalNoSQLItem(keys)
	if err != nil || !ValidID(project) || !ValidID(table) {
		return "", errors.New("invalid NoSQL item identity")
	}
	return project + "/" + table + "/" + base64.RawURLEncoding.EncodeToString([]byte(canonical)), nil
}
func ParseNoSQLItemIdentity(id string) (string, string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || !ValidID(parts[0]) || !ValidID(parts[1]) || len(parts[2]) > MaxNoSQLConfigBytes*2 {
		return "", "", "", errors.New("use project_id/table_id/base64url(keys_json) for import")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", "", "", errors.New("invalid encoded NoSQL key")
	}
	canonical, err := CanonicalNoSQLItem(string(raw))
	if err != nil {
		return "", "", "", err
	}
	return parts[0], parts[1], canonical, nil
}
