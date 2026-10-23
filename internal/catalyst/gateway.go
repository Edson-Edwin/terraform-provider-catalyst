// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"time"
)

type GatewayStatus struct {
	Enabled   *bool `json:"status"`
	Scheduled *bool `json:"scheduled"`
}
type GatewayThrottle struct {
	Overall map[string]json.RawMessage `json:"overall"`
	IP      map[string]json.RawMessage `json:"ip"`
}
type GatewayRule struct {
	unsupported    bool
	ID             ID              `json:"api_id,omitempty"`
	Name           string          `json:"name"`
	Source         string          `json:"source_endpoint"`
	Endpoint       string          `json:"target_endpoint"`
	Target         string          `json:"target"`
	TargetID       ID              `json:"target_id,omitempty"`
	Method         string          `json:"method"`
	Authentication []string        `json:"authentication,omitempty"`
	Throttling     GatewayThrottle `json:"throttling"`
	System         bool            `json:"system,omitempty"`
}

func gatewayPath(project string) (string, error) {
	if !ValidID(project) {
		return "", errors.New("project ID must be a positive numeric string")
	}
	return "/baas/v1/project/" + project + "/api-gateway", nil
}
func gatewayData(res *http.Response, target any) error {
	if res.StatusCode != http.StatusOK {
		return apiResponseError("Catalyst API Gateway request", res)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, target) != nil {
		return errors.New("invalid API Gateway response")
	}
	return nil
}
func (c *Client) GetGateway(ctx context.Context, project string) (*GatewayStatus, error) {
	path, err := gatewayPath(project)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var result GatewayStatus
	if err = gatewayData(res, &result); err != nil {
		return nil, err
	}
	if result.Enabled == nil || result.Scheduled == nil {
		return nil, errors.New("incomplete API Gateway status")
	}
	return &result, nil
}
func (c *Client) WaitGatewayIdle(ctx context.Context, project string) (*GatewayStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for {
		state, err := c.GetGateway(ctx, project)
		if err != nil {
			return nil, err
		}
		if !*state.Scheduled {
			return state, nil
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}
func (c *Client) SetGatewayEnabled(ctx context.Context, project string, enabled bool) error {
	select {
	case c.gatewayWrites <- struct{}{}:
		defer func() { <-c.gatewayWrites }()
	case <-ctx.Done():
		return ctx.Err()
	}
	state, err := c.WaitGatewayIdle(ctx, project)
	if err != nil {
		return err
	}
	if *state.Enabled == enabled {
		return nil
	}
	path, err := gatewayPath(project)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(struct {
		Enabled bool `json:"status"`
	}{enabled})
	res, err := c.request(ctx, http.MethodPut, path, payload)
	if err != nil {
		return err
	}
	var response json.RawMessage
	err = gatewayData(res, &response)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		state, err = c.GetGateway(ctx, project)
		if err != nil {
			return err
		}
		if *state.Enabled == enabled && !*state.Scheduled {
			return nil
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

// ListGatewayRules is read-only. A disabled Gateway rejects this endpoint; that is never an empty list.
func (c *Client) ListGatewayRules(ctx context.Context, project string) ([]GatewayRule, error) {
	path, err := gatewayPath(project)
	if err != nil {
		return nil, err
	}
	rules := []GatewayRule{}
	ids, names := map[ID]bool{}, map[string]bool{}
	// The documented service limit is 1000; an extra page detects truncated or repeating pagination.
	for start := 1; start <= 1201; start += 200 {
		page, err := getList[GatewayRule](ctx, c, fmt.Sprintf("%s/api?start=%d&num_of_apis=200&enhance=true", path, start))
		if err != nil {
			return nil, err
		}
		if len(page) > 200 {
			return nil, errors.New("invalid API Gateway page size")
		}
		for _, rule := range page {
			if !ValidID(string(rule.ID)) || strings.TrimSpace(rule.Name) == "" || rule.Source == "" || rule.Endpoint == "" || rule.Target == "" || rule.Method == "" || ids[rule.ID] || names[rule.Name] {
				return nil, errors.New("incomplete or duplicate API Gateway rule metadata")
			}
			ids[rule.ID], names[rule.Name] = true, true
			if rule.Authentication == nil {
				rule.Authentication = []string{}
			}
			slices.Sort(rule.Authentication)
			if rule.Target == "default" && !rule.System && strings.HasPrefix(rule.Endpoint, "/app/") {
				rule.Target = "client"
			}
			rules = append(rules, rule)
		}
		if len(rules) > 1000 {
			return nil, errors.New("API Gateway exceeded the documented 1000-rule limit")
		}
		if len(page) < 200 {
			slices.SortFunc(rules, func(a, b GatewayRule) int { return strings.Compare(a.Name, b.Name) })
			return rules, nil
		}
	}
	return nil, errors.New("API Gateway pagination did not terminate")
}

// SupportedGatewayRule deliberately rejects policies that the Terraform schema cannot represent.
func SupportedGatewayRule(rule GatewayRule) bool {
	if rule.unsupported || strings.TrimSpace(rule.Name) == "" || rule.Name == "Login Redirect" || rule.System || !strings.HasPrefix(rule.Source, "/") || strings.ContainsAny(rule.Source, "\r\n?#") || strings.ContainsAny(rule.Endpoint, "\r\n?#") || len(rule.Throttling.Overall) > 0 || len(rule.Throttling.IP) > 0 {
		return false
	}
	if !slices.Contains([]string{"GET", "POST", "PUT", "DELETE", "PATCH", "ANY"}, rule.Method) {
		return false
	}
	seen := map[string]bool{}
	for _, method := range rule.Authentication {
		if !slices.Contains([]string{"OAuth", "APIKey", "CatalystUserManagement"}, method) || seen[method] {
			return false
		}
		seen[method] = true
	}
	switch rule.Target {
	case "basicio":
		return ValidID(string(rule.TargetID)) && strings.HasPrefix(rule.Endpoint, "/server/") && strings.HasSuffix(rule.Endpoint, "/execute")
	case "advancedio":
		return ValidID(string(rule.TargetID)) && strings.HasPrefix(rule.Endpoint, "/server/") && len(strings.TrimPrefix(rule.Endpoint, "/server/")) > 0
	case "client":
		return rule.TargetID == "" && strings.HasPrefix(rule.Endpoint, "/app/") && rule.Method == "GET" && len(rule.Authentication) == 0
	default:
		return false
	}
}
func gatewayRulesEqual(a, b []GatewayRule) bool {
	if len(a) != len(b) {
		return false
	}
	expected := make(map[string]GatewayRule, len(b))
	for _, r := range b {
		expected[r.Name] = r
	}
	for _, r := range a {
		e, ok := expected[r.Name]
		if !ok || !SupportedGatewayRule(r) || r.Source != e.Source || r.Endpoint != e.Endpoint || r.Target != e.Target || r.TargetID != e.TargetID || r.Method != e.Method || !slices.Equal(r.Authentication, e.Authentication) {
			return false
		}
	}
	return true
}

// SubmitGatewayRules owns the entire user ruleset. Callers must retain the project ID before writing.
// Creation requires an empty ruleset. Independent states/external tools must never share ownership.
func (c *Client) SubmitGatewayRules(ctx context.Context, project string, desired []GatewayRule, creating bool, beforeWrite func() error) error {
	path, err := gatewayPath(project)
	if err != nil {
		return err
	}
	if desired == nil || len(desired) > 1000 {
		return errors.New("provide a complete ruleset with at most 1000 rules")
	}
	names, routes := map[string]bool{}, map[string]map[string]bool{}
	for _, rule := range desired {
		if !SupportedGatewayRule(rule) || names[rule.Name] {
			return errors.New("unsupported or duplicate API Gateway rule")
		}
		names[rule.Name] = true
		if routes[rule.Source] == nil {
			routes[rule.Source] = map[string]bool{}
		}
		methods := routes[rule.Source]
		if methods[rule.Method] || methods["ANY"] || (rule.Method == "ANY" && len(methods) > 0) {
			return errors.New("overlapping API Gateway request method and source")
		}
		methods[rule.Method] = true
	}
	select {
	case c.gatewayWrites <- struct{}{}:
		defer func() { <-c.gatewayWrites }()
	case <-ctx.Done():
		return ctx.Err()
	}
	state, err := c.WaitGatewayIdle(ctx, project)
	if err != nil {
		return err
	}
	if !*state.Enabled {
		return errors.New("enable API Gateway before managing its ruleset; disabled status is not resource absence")
	}
	current, err := c.ListGatewayRules(ctx, project)
	if err != nil {
		return err
	}
	if creating && len(current) > 0 {
		return errors.New("API Gateway already has rules; import the complete ruleset explicitly")
	}
	for _, rule := range current {
		if !SupportedGatewayRule(rule) {
			return errors.New("API Gateway contains a target, authentication or throttling policy unsupported by this ruleset resource; no changes were submitted")
		}
	}
	if beforeWrite != nil {
		if err = beforeWrite(); err != nil {
			return err
		}
	}
	if gatewayRulesEqual(current, desired) {
		return nil
	}
	// Exclude server metadata; this matches the official CLI's configuration format.
	outgoing := slices.Clone(desired)
	for i := range outgoing {
		outgoing[i].ID = ""
		outgoing[i].Throttling = GatewayThrottle{Overall: map[string]json.RawMessage{}, IP: map[string]json.RawMessage{}}
	}
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return errors.New("could not encode API Gateway rules")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("config_file", "rules.json")
	if err != nil {
		return err
	}
	if _, err = part.Write(payload); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	res, err := c.requestContent(ctx, http.MethodPut, path+"/api", body.Bytes(), writer.FormDataContentType())
	if err != nil {
		return err
	}
	var accepted json.RawMessage
	err = gatewayData(res, &accepted)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	stable := 0
	sawScheduled := false
	for {
		state, err = c.GetGateway(ctx, project)
		if err != nil {
			return err
		}
		if !*state.Enabled {
			return errors.New("API Gateway was disabled during ruleset deployment")
		}
		if *state.Scheduled {
			sawScheduled = true
			stable = 0
		} else {
			if sawScheduled {
				if err = c.gatewayJobSucceeded(ctx, path); err != nil {
					return err
				}
			}
			observed, err := c.ListGatewayRules(ctx, project)
			if err != nil {
				return err
			}
			if gatewayRulesEqual(observed, desired) {
				stable++
				if stable == 3 {
					return nil
				}
			} else {
				stable = 0
			}
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

type gatewayJobOperation struct {
	Success *Integer `json:"success_count"`
	Fail    *Integer `json:"fail_count"`
}

func (c *Client) gatewayJobSucceeded(ctx context.Context, path string) error {
	res, err := c.get(ctx, path+"/schedule?prev=true")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var report struct {
		State                  *Integer `json:"state"`
		Create, Update, Delete *gatewayJobOperation
	}
	if err = gatewayData(res, &report); err != nil {
		return err
	}
	if report.State == nil || *report.State != 4 {
		return errors.New("API Gateway migration did not finish successfully; state is retained")
	}
	seen := false
	for _, op := range []*gatewayJobOperation{report.Create, report.Update, report.Delete} {
		if op == nil {
			continue
		}
		seen = true
		if op.Success == nil || op.Fail == nil || *op.Success < 0 || *op.Fail < 0 {
			return errors.New("incomplete API Gateway migration counts; state is retained")
		}
		if *op.Fail != 0 {
			return errors.New("API Gateway migration rejected one or more rules; state is retained")
		}
	}
	if !seen {
		return errors.New("API Gateway migration operations are missing; state is retained")
	}
	return nil
}

func (r *GatewayRule) UnmarshalJSON(raw []byte) error {
	type wire GatewayRule
	var result wire
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	allowed := []string{"api_id", "name", "source_endpoint", "target_endpoint", "target", "target_id", "method", "authentication", "throttling", "system", "created_by", "created_time", "modified_by", "modified_time"}
	for key := range fields {
		if !slices.Contains(allowed, key) {
			result.unsupported = true
		}
	}
	if throttle, ok := fields["throttling"]; ok {
		var policies map[string]json.RawMessage
		if err := json.Unmarshal(throttle, &policies); err != nil {
			return err
		}
		for key := range policies {
			if key != "overall" && key != "ip" {
				result.unsupported = true
			}
		}
	}
	*r = GatewayRule(result)
	return nil
}
