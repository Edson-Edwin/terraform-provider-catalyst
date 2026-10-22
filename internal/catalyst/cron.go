// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// ErrCronNotFound means the cron is absent and access to its parent was verified.
var ErrCronNotFound = errors.New("catalyst cron does not exist")

// Integer accepts numbers or decimal strings used inconsistently by the API.
type Integer int64

func (i *Integer) UnmarshalJSON(raw []byte) error {
	var s string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("invalid integer")
		}
	} else {
		s = string(raw)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return errors.New("invalid integer")
	}
	*i = Integer(n)
	return nil
}

type CronSchedule struct {
	Timezone string `json:"timezone"`
}
type CronRetry struct {
	Count    Integer `json:"number_of_retries"`
	Interval Integer `json:"retry_interval,omitempty"`
}
type CronJob struct {
	Name       string            `json:"job_name"`
	PoolID     ID                `json:"jobpool_id"`
	TargetType string            `json:"target_type"`
	SourceType string            `json:"source_type"`
	URL        string            `json:"url"`
	Method     string            `json:"request_method"`
	Headers    map[string]string `json:"headers"`
	Params     map[string]string `json:"params"`
	Body       string            `json:"request_body"`
	Retry      CronRetry         `json:"job_config"`
}

// Cron is the supported pre-defined CronExpression webhook configuration.
// Profile details and other API fields are deliberately not retained.
type Cron struct {
	ID            ID           `json:"id,omitempty"`
	Name          string       `json:"cron_name"`
	Description   string       `json:"description"`
	Type          string       `json:"cron_type"`
	ExecutionType string       `json:"cron_execution_type"`
	Expression    string       `json:"cron_expression"`
	Enabled       *bool        `json:"cron_status"`
	EndTime       Integer      `json:"end_time"`
	Schedule      CronSchedule `json:"job_detail"`
	Detail        CronSchedule `json:"cron_detail,omitempty"`
	Job           CronJob      `json:"job_meta"`
}

func cronPath(projectID, id string) (string, error) {
	if !ValidID(projectID) || (id != "" && !ValidID(id)) {
		return "", errors.New("project and cron IDs must be positive integers written as strings")
	}
	path := "/baas/v1/project/" + projectID + "/job_scheduling/cron"
	if id != "" {
		path += "/" + id
	}
	return path, nil
}
func (c *Client) CreateCron(ctx context.Context, projectID string, cron Cron) (*Cron, error) {
	cron.ID = ""
	return c.writeCron(ctx, http.MethodPost, projectID, "", cron)
}
func (c *Client) UpdateCron(ctx context.Context, projectID, id string, cron Cron) (*Cron, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid cron ID")
	}
	cron.ID = ID(id)
	return c.writeCron(ctx, http.MethodPut, projectID, id, cron)
}
func (c *Client) writeCron(ctx context.Context, method, projectID, id string, cron Cron) (*Cron, error) {
	path, err := cronPath(projectID, id)
	if err != nil {
		return nil, err
	}
	// Marshal only supported request fields, excluding the response-only cron_detail.
	payload := struct {
		ID            ID           `json:"id,omitempty"`
		Name          string       `json:"cron_name"`
		Description   string       `json:"description"`
		Type          string       `json:"cron_type"`
		ExecutionType string       `json:"cron_execution_type"`
		Expression    string       `json:"cron_expression"`
		Enabled       *bool        `json:"cron_status"`
		EndTime       Integer      `json:"end_time"`
		Schedule      CronSchedule `json:"job_detail"`
		Job           CronJob      `json:"job_meta"`
	}{cron.ID, cron.Name, cron.Description, cron.Type, cron.ExecutionType, cron.Expression, cron.Enabled, cron.EndTime, cron.Schedule, cron.Job}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("could not encode cron request")
	}
	res, err := c.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readCron(ctx, res, projectID, id)
}
func (c *Client) GetCron(ctx context.Context, projectID, id string) (*Cron, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid cron ID")
	}
	path, err := cronPath(projectID, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readCron(ctx, res, projectID, id)
}
func (c *Client) DeleteCron(ctx context.Context, projectID, id string) error {
	if !ValidID(id) {
		return errors.New("invalid cron ID")
	}
	path, err := cronPath(projectID, id)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return c.cronError(ctx, res, projectID)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ID ID `json:"id"`
		} `json:"data"`
	}
	if err := decode(res.Body, &envelope); err != nil || envelope.Status != "success" || string(envelope.Data.ID) != id {
		return errors.New("the Catalyst API did not confirm deletion of the requested cron")
	}
	return nil
}
func (c *Client) cronError(ctx context.Context, res *http.Response, projectID string) error {
	if res.StatusCode == http.StatusNotFound {
		var failure struct {
			Status string `json:"status"`
			Data   struct {
				Code string `json:"error_code"`
			} `json:"data"`
		}
		if decode(res.Body, &failure) == nil && failure.Status == "failure" && failure.Data.Code == "INVALID_ID" {
			// Missing parent access must never silently remove a managed cron from state.
			if _, err := c.GetProject(ctx, projectID); err != nil {
				return err
			}
			return ErrCronNotFound
		}
	}
	return statusError("Catalyst cron request", res.StatusCode)
}
func (c *Client) readCron(ctx context.Context, res *http.Response, projectID, id string) (*Cron, error) {
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, c.cronError(ctx, res, projectID)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   *Cron  `json:"data"`
	}
	if err := decode(res.Body, &envelope); err != nil {
		return nil, errors.New("the Catalyst API returned an invalid cron response")
	}
	cron := envelope.Data
	if envelope.Status != "success" || cron == nil {
		return nil, errors.New("the Catalyst API did not return a successful cron response")
	}
	if !ValidID(string(cron.ID)) || (id != "" && string(cron.ID) != id) {
		return nil, errors.New("the Catalyst API returned a missing or mismatched cron ID")
	}
	// Return the valid ID alongside metadata errors so Create can retain it for recovery.
	if cron.Type != "CronExpression" || cron.ExecutionType != "pre-defined" || cron.Job.TargetType != "Webhook" {
		return cron, errors.New("only pre-defined CronExpression crons targeting Webhook are supported")
	}
	if cron.Name == "" || cron.Expression == "" || cron.Enabled == nil || cron.Detail.Timezone == "" || cron.Job.Name == "" || !ValidID(string(cron.Job.PoolID)) || cron.Job.URL == "" || cron.Job.Method == "" {
		return cron, errors.New("the Catalyst API returned incomplete cron metadata")
	}
	return cron, nil
}
