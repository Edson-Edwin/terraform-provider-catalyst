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
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"regexp"
	"time"
)

var ErrFunctionNotFound = errors.New("the Catalyst Function does not exist")
var errFunctionPending = errors.New("function metadata is temporarily unavailable")
var functionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)

// Resource operations supply one budget for requests and all nested polling.
// Standalone client calls still have a bounded default.
func functionOperationContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, fallback)
}

func ValidFunctionName(name string) bool { return functionName.MatchString(name) }

type FunctionConfiguration struct {
	Memory      Integer `json:"memory"`
	Environment struct {
		Variables map[string]string `json:"variables"`
	} `json:"environment"`
}
type FunctionDeployment struct {
	FunctionMetadata
	Configuration FunctionConfiguration `json:"configuration"`
}

func (c *Client) functionError(ctx context.Context, res *http.Response, project, id string) error {
	var failure apiFailure
	if res.StatusCode == http.StatusNotFound && ValidID(id) {
		return c.confirmFunctionAbsent(ctx, project, id)
	}
	_ = decode(res.Body, &failure)
	return failureError("Catalyst Function request", res.StatusCode, failure)
}

// Catalyst returns success/data:null for a deleted Function. Confirm parent access
// and absence from the complete listing before removing managed state.
func (c *Client) confirmFunctionAbsent(ctx context.Context, project, id string) error {
	if _, err := c.GetProject(ctx, project); err != nil {
		return err
	}
	// Upsert can temporarily hide both metadata and listing entries. Require
	// consecutive absent observations before interpreting this as deletion.
	for attempt := 0; attempt < 3; attempt++ {
		items, err := c.ListFunctions(ctx, project)
		if err != nil {
			return err
		}
		for _, item := range items {
			if string(item.ID) == id {
				return errFunctionPending
			}
		}
		if attempt < 2 {
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	return ErrFunctionNotFound
}
func readFunctionEventually[T any](ctx context.Context, c *Client, read func(context.Context) (*T, error)) (*T, error) {
	ctx, cancel := functionOperationContext(ctx, time.Minute)
	defer cancel()
	for {
		result, err := read(ctx)
		if !errors.Is(err, errFunctionPending) {
			return result, err
		}
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
	}
}

func (c *Client) readDeployment(ctx context.Context, res *http.Response, project, id string) (*FunctionDeployment, error) {
	if res.StatusCode != 200 && res.StatusCode != 201 {
		return nil, c.functionError(ctx, res, project, id)
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" {
		return nil, errors.New("invalid Function deployment response")
	}
	if id != "" && string(envelope.Data) == "null" {
		return nil, c.confirmFunctionAbsent(ctx, project, id)
	}
	var identity struct {
		ID ID `json:"id"`
	}
	if json.Unmarshal(envelope.Data, &identity) != nil || !ValidID(string(identity.ID)) || (id != "" && string(identity.ID) != id) {
		return nil, errors.New("invalid Function deployment identity")
	}
	result := &FunctionDeployment{FunctionMetadata: FunctionMetadata{ID: identity.ID}}
	if json.Unmarshal(envelope.Data, result) != nil || !validFunction(result.FunctionMetadata) || (result.Configuration.Memory != 128 && result.Configuration.Memory != 256 && result.Configuration.Memory != 512) {
		return &FunctionDeployment{FunctionMetadata: FunctionMetadata{ID: identity.ID}}, errors.New("incomplete Function deployment metadata")
	}
	if result.Configuration.Environment.Variables == nil {
		result.Configuration.Environment.Variables = map[string]string{}
	}
	result.Type = normalizeFunctionType(result.Type)
	return result, nil
}
func (c *Client) GetFunctionDeployment(ctx context.Context, project, id string) (*FunctionDeployment, error) {
	return readFunctionEventually(ctx, c, func(ctx context.Context) (*FunctionDeployment, error) {
		return c.getFunctionDeploymentOnce(ctx, project, id)
	})
}
func (c *Client) getFunctionDeploymentOnce(ctx context.Context, project, id string) (*FunctionDeployment, error) {
	if !ValidID(id) {
		return nil, errors.New("invalid Function ID")
	}
	path, err := functionPath(project, id)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readDeployment(ctx, res, project, id)
}

// DeployFunction serializes name checks and upserts per client, not across processes.
// Catalyst's name-keyed API has no established conditional-create primitive.
func (c *Client) DeployFunction(ctx context.Context, project, id string, desired FunctionDeployment, archive []byte) (*FunctionDeployment, error) {
	return c.deployFunction(ctx, project, id, desired, archive, "")
}

// UpdateFunctionConfiguration follows the official CLI's code-free upsert.
// A matching existing ID and artifact are required; this is not atomic CAS.
func (c *Client) UpdateFunctionConfiguration(ctx context.Context, project, id string, desired FunctionDeployment, expectedHash string) (*FunctionDeployment, error) {
	decoded, err := hex.DecodeString(expectedHash)
	if !ValidID(id) || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != expectedHash {
		return nil, errors.New("configuration updates require an existing Function ID and SHA-256")
	}
	return c.deployFunction(ctx, project, id, desired, nil, expectedHash)
}

func (c *Client) deployFunction(ctx context.Context, project, id string, desired FunctionDeployment, archive []byte, expectedHash string) (*FunctionDeployment, error) {
	path, err := functionPath(project, "")
	if err != nil {
		return nil, err
	}
	if (id != "" && !ValidID(id)) || !ValidFunctionName(desired.Name) || !ValidFunctionStack(desired.Stack) || !ValidFunctionType(desired.Type) || int64(len(archive)) > MaxFileBytes || (desired.Configuration.Memory != 128 && desired.Configuration.Memory != 256 && desired.Configuration.Memory != 512) {
		return nil, errors.New("invalid or unsupported Function configuration")
	}
	select {
	case c.deploymentWrites <- struct{}{}:
		defer func() { <-c.deploymentWrites }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if id != "" {
		current, err := c.GetFunction(ctx, project, id)
		if err != nil {
			return nil, err
		}
		if current.Name != desired.Name || !ValidFunctionStack(current.Stack) || current.Type != desired.Type || (expectedHash != "" && current.Stack != desired.Stack) {
			return nil, errors.New("function identity/configuration changed; refresh the Terraform plan")
		}
	}
	existing, err := c.ListFunctions(ctx, project)
	if err != nil {
		return nil, err
	}
	for _, item := range existing {
		if item.Name == desired.Name && string(item.ID) != id {
			return nil, errors.New("function name already exists; import it explicitly instead of overwriting it")
		}
	}
	if expectedHash != "" {
		actualHash, err := c.FunctionSHA256(ctx, project, id)
		if err != nil {
			return nil, err
		}
		if actualHash != expectedHash {
			return nil, errors.New("function artifact changed since planning; refresh before updating configuration")
		}
	}
	if desired.Type == "advancedio" {
		path = "/baas/v1/project/" + project + "/server"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	config, err := json.Marshal(desired.Configuration)
	if err != nil {
		return nil, errors.New("could not encode Function configuration")
	}
	fields := map[string]string{"name": desired.Name, "stack": desired.Stack, "configuration": string(config)}
	if desired.Type != "advancedio" {
		fields["type"] = desired.Type
	}
	for key, value := range fields {
		if err = writer.WriteField(key, value); err != nil {
			return nil, errors.New("could not encode Function deployment")
		}
	}
	if expectedHash == "" {
		part, err := writer.CreateFormFile("code", "function.zip")
		if err != nil {
			return nil, err
		}
		if _, err = part.Write(archive); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	res, err := c.requestContent(ctx, http.MethodPut, path+"/upsert", body.Bytes(), writer.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return c.readDeployment(ctx, res, project, id)
}
func (c *Client) FunctionSHA256(ctx context.Context, project, id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("invalid Function ID")
	}
	path, err := functionPath(project, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := functionOperationContext(ctx, 2*time.Minute)
	defer cancel()
	for {
		res, err := c.get(ctx, path+"/download")
		if err != nil {
			return "", err
		}
		if res.StatusCode == 404 {
			_ = res.Body.Close()
			// Artifact replicas can briefly return 404 while the Function still exists.
			// Read its metadata before retrying; permission loss never means absence.
			if _, err = c.GetFunction(ctx, project, id); err != nil {
				return "", err
			}
			if err = c.wait(ctx, 2*time.Second); err != nil {
				return "", err
			}
			continue
		}
		if res.StatusCode != 200 {
			err = c.functionError(ctx, res, project, id)
			_ = res.Body.Close()
			return "", err
		}
		reader := bufio.NewReader(res.Body)
		signature, err := reader.Peek(4)
		if err != nil || !bytes.Equal(signature, []byte{'P', 'K', 3, 4}) {
			_ = res.Body.Close()
			return "", errors.New("function download did not return a ZIP artifact")
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(reader, MaxFileBytes+1))
		_ = res.Body.Close()
		if err != nil || size > MaxFileBytes {
			return "", errors.New("function archive could not be downloaded within the provider's 100 MB limit")
		}
		return hex.EncodeToString(hash.Sum(nil)), nil
	}
}

func (c *Client) DeleteFunction(ctx context.Context, project, id string) error {
	ctx, cancel := functionOperationContext(ctx, time.Minute)
	defer cancel()
	for {
		err := c.deleteFunctionOnce(ctx, project, id)
		if !errors.Is(err, errFunctionPending) {
			return err
		}
		// A definitive 404 can be transient while this exact immutable ID remains listed.
		// Repeating deletion of that ID is idempotent; ambiguous failures are not retried.
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}
func (c *Client) deleteFunctionOnce(ctx context.Context, project, id string) error {
	if !ValidID(id) {
		return errors.New("invalid Function ID")
	}
	path, err := functionPath(project, id)
	if err != nil {
		return err
	}
	metadata, err := c.GetFunction(ctx, project, id)
	if err != nil {
		return err
	}
	if metadata.Type == "advancedio" {
		path = "/baas/v1/project/" + project + "/server/" + id
	}
	res, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return c.functionError(ctx, res, project, id)
	}
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ID ID `json:"id"`
		} `json:"data"`
	}
	if decode(res.Body, &envelope) != nil || envelope.Status != "success" || string(envelope.Data.ID) != id {
		return errors.New("the Catalyst did not confirm deletion of the requested Function")
	}
	return nil
}

func matchesFunctionDeployment(a, b *FunctionDeployment) bool {
	return a != nil && b != nil && a.Name == b.Name && a.Stack == b.Stack && a.Type == b.Type && a.Configuration.Memory == b.Configuration.Memory && maps.Equal(a.Configuration.Environment.Variables, b.Configuration.Environment.Variables)
}

// ReadStableFunctionDeployment accepts an unchanged snapshot immediately. A
// changed snapshot must agree across three observations before replacing state.
// The saved snapshot is not a desired-state filter: stable external drift is returned.
func (c *Client) ReadStableFunctionDeployment(ctx context.Context, project, id string, saved FunctionDeployment, savedHash string) (*FunctionDeployment, string, error) {
	ctx, cancel := functionOperationContext(ctx, 2*time.Minute)
	defer cancel()
	var previous *FunctionDeployment
	previousHash := ""
	stable := 0
	for {
		current, err := c.GetFunctionDeployment(ctx, project, id)
		if err != nil {
			return nil, "", err
		}
		hash, err := c.FunctionSHA256(ctx, project, id)
		if err != nil {
			return nil, "", err
		}
		if savedHash != "" && hash == savedHash && matchesFunctionDeployment(current, &saved) {
			return current, hash, nil
		}
		if hash == previousHash && matchesFunctionDeployment(current, previous) {
			stable++
		} else {
			stable = 1
		}
		if stable == 3 {
			return current, hash, nil
		}
		previous, previousHash = current, hash
		if err = c.wait(ctx, 2*time.Second); err != nil {
			return nil, "", err
		}
	}
}

// WaitFunctionDeployment requires stable configuration and artifact observations after upsert.
func (c *Client) WaitFunctionDeployment(ctx context.Context, project, id string, desired FunctionDeployment, hash string) (*FunctionDeployment, error) {
	ctx, cancel := functionOperationContext(ctx, 2*time.Minute)
	defer cancel()
	stable := 0
	for {
		matched := false
		current, err := c.GetFunctionDeployment(ctx, project, id)
		if err != nil && !errors.Is(err, ErrFunctionNotFound) {
			return nil, err
		}
		if err == nil && matchesFunctionDeployment(current, &desired) {
			// Avoid downloading large archives while metadata is still stale.
			actualHash, hashErr := c.FunctionSHA256(ctx, project, id)
			if hashErr != nil && !errors.Is(hashErr, ErrFunctionNotFound) {
				return nil, hashErr
			}
			if hashErr == nil && actualHash == hash {
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
func (c *Client) WaitFunctionDeleted(ctx context.Context, project, id string) error {
	ctx, cancel := functionOperationContext(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := c.GetFunction(ctx, project, id)
		if errors.Is(err, ErrFunctionNotFound) {
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
