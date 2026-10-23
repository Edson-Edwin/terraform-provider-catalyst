// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ErrAppSailNotFound = errors.New("the Catalyst AppSail does not exist")
var errAppSailPending = errors.New("appsail metadata is temporarily unavailable")
var appsailName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,99}$`)

func ValidAppSailName(name string) bool { return appsailName.MatchString(name) }

type AppSailConfiguration struct {
	Command     string  `json:"startup_command"`
	Port        Integer `json:"port"`
	Auth        *bool   `json:"catalyst_auth"`
	Memory      Integer `json:"memory"`
	Environment struct {
		Variables map[string]string `json:"variables"`
	} `json:"environment"`
}
type AppSailDeployment struct {
	AppSailMetadata
	Configuration AppSailConfiguration `json:"configuration"`
}

func (c *Client) appsailError(ctx context.Context, res *http.Response, project, id string) error {
	var failure apiFailure
	if decode(res.Body, &failure) == nil && res.StatusCode == 404 && failure.Status == "failure" && failure.Data.Code == "INVALID_ID" {
		return c.confirmAppSailAbsent(ctx, project, id)
	}
	return failureError("Catalyst AppSail request", res.StatusCode, failure)
}

// Catalyst returns success/data:null for a deleted AppSail. Confirm parent access
// and absence from the complete listing before removing managed state.
func (c *Client) confirmAppSailAbsent(ctx context.Context, project, id string) error {
	if _, err := c.GetProject(ctx, project); err != nil {
		return err
	}
	// Upsert can temporarily hide both metadata and listing entries. Require
	// consecutive absent observations before interpreting this as deletion.
	for attempt := 0; attempt < 3; attempt++ {
		items, err := c.ListAppSails(ctx, project)
		if err != nil {
			return err
		}
		for _, item := range items {
			if string(item.ID) == id {
				return errAppSailPending
			}
		}
		if attempt < 2 {
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	return ErrAppSailNotFound
}
func readAppSailEventually[T any](ctx context.Context, c *Client, read func(context.Context) (*T, error)) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for {
		result, err := read(ctx)
		if !errors.Is(err, errAppSailPending) {
			return result, err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}

func (c *Client) readAppSailDeployment(ctx context.Context, res *http.Response, project, id string) (*AppSailDeployment, error) {
	if res.StatusCode != 200 && res.StatusCode != 201 {
		return nil, c.appsailError(ctx, res, project, id)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("invalid AppSail deployment response")
	}
	if id != "" && string(envelope.Data) == "null" {
		return nil, c.confirmAppSailAbsent(ctx, project, id)
	}
	var identity struct {
		ID ID `json:"id"`
	}
	if json.Unmarshal(envelope.Data, &identity) != nil || !ValidID(string(identity.ID)) || (id != "" && string(identity.ID) != id) {
		return nil, errors.New("invalid AppSail deployment identity")
	}
	result := &AppSailDeployment{AppSailMetadata: AppSailMetadata{ID: identity.ID}}
	if json.Unmarshal(envelope.Data, result) != nil || !validAppSail(result.AppSailMetadata) || !validAppSailConfiguration(result.Configuration) {
		return &AppSailDeployment{AppSailMetadata: AppSailMetadata{ID: identity.ID}}, errors.New("incomplete AppSail deployment metadata")
	}
	if result.Configuration.Environment.Variables == nil {
		result.Configuration.Environment.Variables = map[string]string{}
	}
	return result, nil
}
func (c *Client) GetAppSailDeployment(ctx context.Context, project, id string) (*AppSailDeployment, error) {
	return readAppSailEventually(ctx, c, func(ctx context.Context) (*AppSailDeployment, error) {
		return c.getAppSailDeploymentOnce(ctx, project, id)
	})
}
func (c *Client) getAppSailDeploymentOnce(ctx context.Context, project, id string) (*AppSailDeployment, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid AppSail ID")
	}
	path, err := appsailPath(project, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readAppSailDeployment(ctx, res, project, id)
}

// DeployAppSail serializes name checks and upserts per client, not across processes.
// Catalyst's name-keyed API has no established conditional-create primitive.
func (c *Client) DeployAppSail(ctx context.Context, project, id string, desired AppSailDeployment, archive []byte) (*AppSailDeployment, error) {
	path, err := appsailPath(project, "")
	if err != nil {
		return nil, err
	}
	if (id != "" && !ValidID(id)) || !ValidAppSailName(desired.Name) || desired.Stack != "node22" || int64(len(archive)) > MaxFileBytes || !validAppSailConfiguration(desired.Configuration) {
		return nil, errors.New("invalid or unsupported AppSail configuration")
	}
	select {
	case c.deploymentWrites <- struct{}{}:
		defer func() { <-c.deploymentWrites }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if id != "" {
		current, err := c.GetAppSail(ctx, project, id)
		if err != nil {
			return nil, err
		}
		if current.Name != desired.Name || current.Stack != desired.Stack {
			return nil, errors.New("appsail identity/configuration changed; refresh the Terraform plan")
		}
	}
	existing, err := c.ListAppSails(ctx, project)
	if err != nil {
		return nil, err
	}
	for _, item := range existing {
		if item.Name == desired.Name && string(item.ID) != id {
			return nil, errors.New("appsail name already exists; import it explicitly instead of overwriting it")
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	config, err := json.Marshal(map[string]any{"startup_command": desired.Configuration.Command, "port": desired.Configuration.Port, "catalyst_auth": desired.Configuration.Auth, "environment": desired.Configuration.Environment})
	if err != nil {
		return nil, errors.New("could not encode AppSail configuration")
	}
	for key, value := range map[string]string{"name": desired.Name, "stack": desired.Stack, "memory": fmt.Sprint(desired.Configuration.Memory), "configuration": string(config)} {
		if err = writer.WriteField(key, value); err != nil {
			return nil, errors.New("could not encode AppSail deployment")
		}
	}
	part, err := writer.CreateFormFile("code", "appsail.zip")
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(archive); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	res, err := c.requestContent(ctx, http.MethodPut, path+"/upsert", body.Bytes(), writer.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readAppSailDeployment(ctx, res, project, id)
}
func (c *Client) AppSailSHA256(ctx context.Context, project, id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid AppSail ID")
	}
	path, err := appsailPath(project, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		res, err := c.get(ctx, path+"/download")
		if err != nil {
			return "", err
		}
		if res.StatusCode == 404 {
			_ = res.Body.Close()
			// Artifact replicas can briefly return 404 while the AppSail still exists.
			// Read its metadata before retrying; permission loss never means absence.
			if _, err = c.GetAppSail(ctx, project, id); err != nil {
				return "", err
			}
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return "", err
			}
			continue
		}
		if res.StatusCode != 200 {
			err = c.appsailError(ctx, res, project, id)
			_ = res.Body.Close()
			return "", err
		}
		reader := bufio.NewReader(res.Body)
		signature, err := reader.Peek(4)
		if err != nil || !bytes.Equal(signature, []byte{'P', 'K', 3, 4}) {
			_ = res.Body.Close()
			return "", errors.New("appsail download did not return a ZIP artifact")
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(reader, MaxFileBytes+1))
		_ = res.Body.Close()
		if err != nil || size > MaxFileBytes {
			return "", errors.New("appsail archive could not be downloaded within the provider's 100 MB limit")
		}
		return hex.EncodeToString(hash.Sum(nil)), nil
	}
}

func (c *Client) DeleteAppSail(ctx context.Context, project, id string) error {
	if !ValidID(id) {
		return errors.New("invalid AppSail ID")
	}
	path, err := appsailPath(project, id)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return c.appsailError(ctx, res, project, id)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ID ID `json:"id"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || string(envelope.Data.ID) != id {
		return errors.New("the Catalyst did not confirm deletion of the requested AppSail")
	}
	return nil
}
func (c *Client) WaitAppSailDeleted(ctx context.Context, project, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := c.GetAppSail(ctx, project, id)
		if errors.Is(err, ErrAppSailNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

func validAppSailConfiguration(config AppSailConfiguration) bool {
	memory := config.Memory
	port := config.Port
	return (memory == 128 || memory == 256 || memory == 512 || memory == 1024 || memory == 2048) && (port == 80 || port == 443 || (port >= 1025 && port <= 65535)) && config.Auth != nil && strings.TrimSpace(config.Command) != "" && !strings.ContainsRune(config.Command, 0)
}

// WaitAppSailDeployment reconciles independently replicated configuration and code.
// It never retries a mutation and retains permission or malformed-response errors.
func (c *Client) WaitAppSailDeployment(ctx context.Context, project, id string, desired AppSailDeployment, hash string) (*AppSailDeployment, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stable := 0
	for {
		matched := false
		current, err := c.GetAppSailDeployment(ctx, project, id)
		if err != nil && !errors.Is(err, ErrAppSailNotFound) {
			return nil, err
		}
		if err == nil {
			actualHash, hashErr := c.AppSailSHA256(ctx, project, id)
			if hashErr != nil && !errors.Is(hashErr, ErrAppSailNotFound) {
				return nil, hashErr
			}
			a, b := current.Configuration, desired.Configuration
			if hashErr == nil && current.Name == desired.Name && current.Stack == desired.Stack && a.Memory == b.Memory && a.Command == b.Command && a.Port == b.Port && a.Auth != nil && b.Auth != nil && *a.Auth == *b.Auth && maps.Equal(a.Environment.Variables, b.Environment.Variables) && actualHash == hash {
				matched = true
				stable++
				if stable == 3 {
					return current, nil
				}
			}
		}
		if !matched {
			stable = 0
		}
		if err := c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}
