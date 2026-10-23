// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Reject duplicate keys instead of silently accepting json.Unmarshal's last
// value, particularly for resource identity and nested typed attributes.
func uniqueJSONKeys(value string) bool {
	d := json.NewDecoder(strings.NewReader(value))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 100 {
			return errors.New("nesting limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON key")
				}
				seen[name] = true
				if err = walk(depth + 1); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim('}') {
				return errors.New("invalid JSON object")
			}
		case '[':
			for d.More() {
				if err = walk(depth + 1); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim(']') {
				return errors.New("invalid JSON list")
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		return nil
	}
	if walk(0) != nil {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
