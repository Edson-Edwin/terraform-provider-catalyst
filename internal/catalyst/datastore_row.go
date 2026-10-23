// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ErrDataStoreRowNotFound = errors.New("the Data Store row is absent in a readable table")
var rowColumnName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var rowSystemColumns = map[string]bool{"ROWID": true, "CREATORID": true, "CREATEDTIME": true, "MODIFIEDTIME": true}

// ParseDataStoreValues accepts scalar configuration values, excluding identities
// and server-generated profiles. RawMessage retains exact integer precision.
func ParseDataStoreValues(value string) (map[string]json.RawMessage, error) {
	var values map[string]json.RawMessage
	if len(value) > 256*1024 || !uniqueJSONKeys(value) || json.Unmarshal([]byte(value), &values) != nil || len(values) == 0 {
		return nil, errors.New("values_json must be a nonempty JSON object within 256 KiB, without duplicate keys")
	}
	for name, raw := range values {
		if !rowColumnName.MatchString(name) || rowSystemColumns[strings.ToUpper(name)] {
			return nil, errors.New("use application column names; system columns cannot be managed")
		}
		var v any
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if d.Decode(&v) != nil {
			return nil, errors.New("invalid Data Store value")
		}
		switch v.(type) {
		case nil, string, bool, json.Number:
		default:
			return nil, errors.New("configuration rows in Data Store support string, number, boolean and null values only")
		}
	}
	return values, nil
}

func dataStoreRowPath(project, table string) (string, error) {
	if !ValidID(project) || !ValidID(table) {
		return "", errors.New("project and table IDs must be positive numeric strings")
	}
	return "/baas/v1/project/" + project + "/table/" + table + "/row", nil
}
func (c *Client) dataStoreRowCall(ctx context.Context, method, path string, body []byte) (json.RawMessage, error) {
	r, err := c.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, apiResponseError("Data Store row request", r)
	}
	var e struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(r.Body, &e) != nil || e.Status != "success" || len(e.Data) == 0 {
		return nil, errors.New("invalid Data Store row response")
	}
	return e.Data, nil
}

type DataStoreRow struct {
	ID     ID
	Values map[string]json.RawMessage
}

func parseDataStoreRow(raw json.RawMessage, id string) (*DataStoreRow, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("invalid Data Store row metadata")
	}
	var actual ID
	if json.Unmarshal(fields["ROWID"], &actual) != nil || !ValidID(string(actual)) || (id != "" && string(actual) != id) {
		return nil, errors.New("missing or mismatched Data Store row identity")
	}
	for name := range fields {
		if rowSystemColumns[strings.ToUpper(name)] {
			delete(fields, name)
		}
	}
	encoded, _ := json.Marshal(fields)
	values, err := ParseDataStoreValues(string(encoded))
	if err != nil {
		return &DataStoreRow{ID: actual}, err
	}
	return &DataStoreRow{ID: actual, Values: values}, nil
}
func (c *Client) GetDataStoreRow(ctx context.Context, project, table, id string) (*DataStoreRow, error) {
	base, err := dataStoreRowPath(project, table)
	if err != nil {
		return nil, err
	}
	if !ValidID(id) {
		return nil, errors.New("invalid Data Store row ID")
	}
	raw, err := c.dataStoreRowCall(ctx, http.MethodGet, base+"/"+id, nil)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		if _, err = c.GetDataStoreSchema(ctx, project, table); err != nil {
			return nil, err
		}
		return nil, ErrDataStoreRowNotFound
	}
	return parseDataStoreRow(raw, id)
}
func (c *Client) CreateDataStoreRow(ctx context.Context, project, table, valuesJSON string) (*DataStoreRow, error) {
	base, err := dataStoreRowPath(project, table)
	if err != nil {
		return nil, err
	}
	values, err := ParseDataStoreValues(valuesJSON)
	if err != nil {
		return nil, err
	}
	if _, err = c.GetDataStoreSchema(ctx, project, table); err != nil {
		return nil, err
	}
	body, _ := json.Marshal([]any{values})
	raw, err := c.dataStoreRowCall(ctx, http.MethodPost, base, body)
	if err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 {
		return nil, errors.New("the Data Store creation response omitted a unique row; inspect the table before retrying")
	}
	return parseDataStoreRow(rows[0], "")
}
func (c *Client) UpdateDataStoreRow(ctx context.Context, project, table, id, desired, previous string) error {
	base, err := dataStoreRowPath(project, table)
	if err != nil {
		return err
	}
	if !ValidID(id) {
		return errors.New("invalid Data Store row ID")
	}
	next, err := ParseDataStoreValues(desired)
	if err != nil {
		return err
	}
	prior, err := ParseDataStoreValues(previous)
	if err != nil {
		return err
	}
	if _, err = c.GetDataStoreRow(ctx, project, table, id); err != nil {
		return err
	}
	for name := range prior {
		if _, exists := next[name]; !exists {
			next[name] = json.RawMessage("null")
		}
	}
	verification, _ := json.Marshal(next)
	next["ROWID"], _ = json.Marshal(id)
	body, _ := json.Marshal([]any{next})
	raw, err := c.dataStoreRowCall(ctx, http.MethodPatch, base, body)
	if err != nil {
		return err
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 1 {
		return errors.New("the Data Store update did not confirm one row")
	}
	u, err := parseDataStoreRow(rows[0], id)
	if err != nil || u == nil {
		return errors.New("the Data Store update returned invalid row metadata")
	}
	_, err = c.WaitDataStoreRow(ctx, project, table, id, string(verification))
	return err
}

// DataStoreProjection exposes only the columns this resource owns. Omitted
// columns are not adopted; removal on update is handled by explicit null PATCH.
func DataStoreProjection(row *DataStoreRow, selection string) (string, error) {
	values := row.Values
	if selection != "" {
		chosen, err := ParseDataStoreValues(selection)
		if err != nil {
			return "", err
		}
		values = map[string]json.RawMessage{}
		for name := range chosen {
			v, exists := row.Values[name]
			if !exists {
				return "", errors.New("managed column is missing from the Data Store row")
			}
			values[name] = v
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", errors.New("could not encode Data Store row values")
	}
	return string(raw), nil
}
func (c *Client) WaitDataStoreRow(ctx context.Context, project, table, id, desired string) (*DataStoreRow, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	want, err := ParseDataStoreValues(desired)
	if err != nil {
		return nil, err
	}
	for {
		row, err := c.GetDataStoreRow(ctx, project, table, id)
		if err != nil {
			return nil, err
		}
		matched := true
		for name, raw := range want {
			v, ok := row.Values[name]
			if !ok || !dataStoreScalarEqual(raw, v) {
				matched = false
				break
			}
		}
		if matched {
			return row, nil
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}
func dataStoreScalarEqual(a, b json.RawMessage) bool {
	canonical := func(raw json.RawMessage) string {
		var v any
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if d.Decode(&v) != nil {
			return "invalid"
		}
		if n, ok := v.(json.Number); ok {
			return "number:" + canonicalNoSQLNumber(string(n))
		}
		r, _ := json.Marshal(v)
		return string(r)
	}
	return canonical(a) == canonical(b)
}

// DataStoreValuesEquivalent compares scalar JSON without rounding numbers and
// accepts equivalent numeric representations returned by Data Store.
func DataStoreValuesEquivalent(a, b string) bool {
	left, err := ParseDataStoreValues(a)
	if err != nil {
		return false
	}
	right, err := ParseDataStoreValues(b)
	if err != nil || len(left) != len(right) {
		return false
	}
	for name, v := range left {
		other, ok := right[name]
		if !ok || !dataStoreScalarEqual(v, other) {
			return false
		}
	}
	return true
}
func (c *Client) DeleteDataStoreRow(ctx context.Context, project, table, id string) error {
	base, err := dataStoreRowPath(project, table)
	if err != nil {
		return err
	}
	if !ValidID(id) {
		return errors.New("invalid Data Store row ID")
	}
	if _, err = c.GetDataStoreRow(ctx, project, table, id); errors.Is(err, ErrDataStoreRowNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err = c.dataStoreRowCall(ctx, http.MethodDelete, base+"/"+id, nil); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for stable := 0; stable < 3; {
		_, err = c.GetDataStoreRow(ctx, project, table, id)
		if errors.Is(err, ErrDataStoreRowNotFound) {
			stable++
		} else if err != nil {
			return err
		} else {
			stable = 0
		}
		if stable < 3 {
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	return nil
}
