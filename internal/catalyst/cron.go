// SPDX-License-Identifier: MPL-2.0

package catalyst

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
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
	Timezone        string    `json:"timezone,omitempty"`
	RepetitionType  string    `json:"repetition_type,omitempty"`
	Hour            *Integer  `json:"hour,omitempty"`
	Minute          *Integer  `json:"minute,omitempty"`
	Second          *Integer  `json:"second,omitempty"`
	Days            []Integer `json:"days,omitempty"`
	Months          []Integer `json:"months,omitempty"`
	WeeksOfMonth    []Integer `json:"weeks_of_month,omitempty"`
	WeekDays        []Integer `json:"week_day,omitempty"`
	TimeOfExecution *Integer  `json:"time_of_execution,omitempty"`
}
type CronRetry struct {
	Count    Integer `json:"number_of_retries"`
	Interval Integer `json:"retry_interval,omitempty"`
}
type CronJob struct {
	Name          string            `json:"job_name"`
	PoolID        ID                `json:"jobpool_id"`
	TargetType    string            `json:"target_type"`
	SourceType    string            `json:"source_type"`
	URL           string            `json:"url"`
	Method        string            `json:"request_method"`
	Headers       map[string]string `json:"headers"`
	Params        map[string]string `json:"params"`
	Body          string            `json:"request_body"`
	Retry         CronRetry         `json:"job_config"`
	TargetID      ID                `json:"target_id,omitempty"`
	TargetDetails *struct {
		ID ID `json:"id"`
	} `json:"target_details,omitempty"`
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
	// Catalyst's wall-clock timezone catalog uses GMT for zero-offset UTC.
	// The short UTC identifier is valid in Go but rejected by the live API.
	if cron.Schedule.Timezone == "UTC" {
		cron.Schedule.Timezone = "GMT"
	}
	job := map[string]any{"job_name": cron.Job.Name, "jobpool_id": cron.Job.PoolID, "target_type": cron.Job.TargetType, "source_type": cron.Job.SourceType, "params": cron.Job.Params, "job_config": cron.Job.Retry}
	switch cron.Job.TargetType {
	case "Webhook":
		job["url"], job["request_method"], job["headers"], job["request_body"] = cron.Job.URL, cron.Job.Method, cron.Job.Headers, cron.Job.Body
	case "Function", "AppSail":
		if !ValidID(string(cron.Job.TargetID)) {
			return nil, errors.New("the scheduled target requires a positive numeric target ID")
		}
		job["target_id"] = cron.Job.TargetID
		if cron.Job.TargetType == "AppSail" {
			job["url"], job["request_method"], job["headers"], job["request_body"] = cron.Job.URL, cron.Job.Method, cron.Job.Headers, cron.Job.Body
		}
	default:
		return nil, errors.New("unsupported cron target type")
	}

	// Marshal only supported request fields, excluding the response-only cron_detail.
	payload := struct {
		ID            ID             `json:"id,omitempty"`
		Name          string         `json:"cron_name"`
		Description   string         `json:"description"`
		Type          string         `json:"cron_type"`
		ExecutionType string         `json:"cron_execution_type"`
		Expression    string         `json:"cron_expression,omitempty"`
		Enabled       *bool          `json:"cron_status"`
		EndTime       Integer        `json:"end_time,omitempty"`
		Schedule      CronSchedule   `json:"job_detail"`
		Job           map[string]any `json:"job_meta"`
	}{cron.ID, cron.Name, cron.Description, cron.Type, cron.ExecutionType, cron.Expression, cron.Enabled, cron.EndTime, cron.Schedule, job}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("could not encode cron request")
	}
	res, err := c.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readCron(ctx, res, projectID, id, cron.Job.TargetType)
}
func (c *Client) GetCron(ctx context.Context, projectID, id string) (*Cron, error) {
	return c.getCronTarget(ctx, projectID, id, "Webhook")
}
func (c *Client) GetFunctionCron(ctx context.Context, projectID, id string) (*Cron, error) {
	return c.getCronTarget(ctx, projectID, id, "Function")
}
func (c *Client) getCronTarget(ctx context.Context, projectID, id, target string) (*Cron, error) {
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
	return c.readCron(ctx, res, projectID, id, target)
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

// WaitCronDeleted confirms absence while preserving errors such as lost project access.
// The caller supplies an operation deadline; the wait also has a two-minute upper bound.
func (c *Client) WaitCronDeleted(ctx context.Context, projectID, id string) error {
	return c.waitCronTargetDeleted(ctx, projectID, id, "Webhook")
}
func (c *Client) WaitFunctionCronDeleted(ctx context.Context, projectID, id string) error {
	return c.waitCronTargetDeleted(ctx, projectID, id, "Function")
}
func (c *Client) waitCronTargetDeleted(ctx context.Context, projectID, id, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := c.getCronTarget(ctx, projectID, id, target)
		if errors.Is(err, ErrCronNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

func (c *Client) cronError(ctx context.Context, res *http.Response, projectID string) error {
	var failure apiFailure
	if decode(res.Body, &failure) == nil && res.StatusCode == http.StatusNotFound && failure.Status == "failure" && failure.Data.Code == "INVALID_ID" {
		// Missing parent access must never silently remove a managed cron from state.
		if _, err := c.GetProject(ctx, projectID); err != nil {
			return err
		}
		return ErrCronNotFound
	}
	return failureError("Catalyst cron request", res.StatusCode, failure)
}
func (c *Client) readCron(ctx context.Context, res *http.Response, projectID, id, target string) (*Cron, error) {
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return nil, c.cronError(ctx, res, projectID)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := decode(res.Body, &envelope); err != nil {
		return nil, errors.New("the Catalyst API returned an invalid cron response")
	}
	if envelope.Status != "success" {
		return nil, errors.New("the Catalyst API did not return a successful cron response")
	}
	// Decode identity independently: malformed metadata must not lose a created ID.
	var identity struct {
		ID ID `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &identity); err != nil || !ValidID(string(identity.ID)) || (id != "" && string(identity.ID) != id) {
		return nil, errors.New("the Catalyst API returned a missing or mismatched cron ID")
	}
	cron := &Cron{}
	if err := json.Unmarshal(envelope.Data, cron); err != nil {
		return &Cron{ID: identity.ID}, errors.New("the Catalyst API returned invalid cron metadata; the cron ID was retained for recovery")
	}
	// One-time responses use job_detail in the documented REST contract.
	if cron.Type == "OneTime" && cron.Detail.Timezone == "" {
		cron.Detail = cron.Schedule
	}
	if cron.Type == "Periodic" {
		if cron.Detail.Hour == nil && cron.Schedule.Hour != nil {
			cron.Detail = cron.Schedule
		}
		if cron.Detail.Timezone == "" {
			cron.Detail.Timezone = "UTC"
		}
	}
	// Return the valid ID alongside metadata errors so Create can retain it for recovery.
	supported := cron.Type == "CronExpression" || cron.Type == "Calendar" || cron.Type == "OneTime" || cron.Type == "Periodic"
	if !supported || cron.ExecutionType != "pre-defined" || cron.Job.TargetType != target {
		return cron, errors.New("cron type or target does not match the managed resource")
	}
	if cron.Name == "" || (cron.Type == "CronExpression" && cron.Expression == "") || cron.Enabled == nil || cron.Detail.Timezone == "" || cron.Job.Name == "" || !ValidID(string(cron.Job.PoolID)) {
		return cron, errors.New("the Catalyst API returned incomplete cron metadata")
	}
	if target == "Webhook" && (cron.Job.URL == "" || cron.Job.Method == "") {
		return cron, errors.New("the Catalyst API returned incomplete webhook metadata")
	}
	if cron.Type == "Periodic" {
		if _, err := PeriodicScheduleSeconds(cron.Detail); err != nil {
			return cron, err
		}
	}
	if cron.Type == "Calendar" {
		if err := ValidCalendarSchedule(cron.Detail); err != nil {
			return cron, err
		}
	}
	if cron.Type == "OneTime" && (cron.Detail.TimeOfExecution == nil || *cron.Detail.TimeOfExecution <= 0) {
		return cron, errors.New("incomplete one-time execution timestamp")
	}
	if target == "Function" {
		if cron.Job.TargetDetails != nil {
			if cron.Job.TargetID != "" && cron.Job.TargetID != cron.Job.TargetDetails.ID {
				return cron, errors.New("mismatched Function target identities")
			}
			cron.Job.TargetID = cron.Job.TargetDetails.ID
		}
		if !ValidID(string(cron.Job.TargetID)) {
			return cron, errors.New("the Catalyst API returned incomplete Function target metadata")
		}
	}
	return cron, nil
}
