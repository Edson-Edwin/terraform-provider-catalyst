// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Execution is deliberately test-only: normal Terraform operations never invoke code.
func TestAccFunctionExecution(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("live execution is opt-in")
	}
	config, project, err := functionExecutionFixtureConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, stack := range []string{"node22", "node24"} {
		if !t.Run(stack, func(t *testing.T) {
			var nonce [12]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				t.Fatal(err)
			}
			name := "tfexec" + hex.EncodeToString(nonce[:])
			t.Log("Disposable Function fixture:", name)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			if err := runFunctionExecutionFixture(ctx, c, project, name, stack); err != nil {
				t.Fatal(err)
			}
		}) {
			return
		}
	}
}

func functionExecutionFixtureConfig(getenv func(string) string) (Config, string, error) {
	config := Config{DataCenter: getenv("CATALYST_DATA_CENTER"), Environment: getenv("CATALYST_ENVIRONMENT"), OrganizationID: getenv("CATALYST_ORGANIZATION_ID"), AccessToken: getenv("CATALYST_ACCESS_TOKEN"), ClientID: getenv("CATALYST_CLIENT_ID"), ClientSecret: getenv("CATALYST_CLIENT_SECRET"), RefreshToken: getenv("CATALYST_REFRESH_TOKEN"), RedirectURI: getenv("CATALYST_REDIRECT_URI")}
	project := getenv("CATALYST_TEST_PROJECT_ID")
	if getenv("CATALYST_TEST_ALLOW_FUNCTION_CREATE") != "1" || getenv("CATALYST_TEST_ALLOW_FUNCTION_INVOKE") != "1" {
		return config, project, errors.New("execution requires both CATALYST_TEST_ALLOW_FUNCTION_CREATE=1 and CATALYST_TEST_ALLOW_FUNCTION_INVOKE=1")
	}
	if _, ok := regions[config.DataCenter]; !ok || config.Environment != "Development" || !ValidID(config.OrganizationID) || !ValidID(project) {
		return config, project, errors.New("execution requires explicit supported data center, Development, organization and dedicated CATALYST_TEST_PROJECT_ID")
	}
	return config, project, nil
}

func executionFixtureArchive(name, stack, message string) ([]byte, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range []struct{ name, body string }{
		{"catalyst-config.json", fmt.Sprintf(`{"deployment":{"name":%q,"stack":%q,"type":"basicio"},"execution":{"main":"index.js"}}`, name, stack)},
		{"package.json", fmt.Sprintf(`{"name":%q,"main":"index.js","version":"1.0.0"}`, name)},
		{"index.js", fmt.Sprintf("module.exports = (context, basicIO) => { basicIO.write(%q); context.close(); };", message)},
	} {
		file, err := writer.Create(entry.name)
		if err != nil {
			return nil, err
		}
		if _, err = file.Write([]byte(entry.body)); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func runFunctionExecutionFixture(ctx context.Context, c *Client, project, name, stack string) (resultErr error) {
	baseline, err := c.ListFunctions(ctx, project)
	if err != nil {
		return err
	}
	original := make(map[ID]bool, len(baseline))
	for _, item := range baseline {
		original[item.ID] = true
	}
	ownedID := ""
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if ownedID != "" {
			current, err := c.GetFunction(cleanup, project, ownedID)
			if err == nil {
				if current.Name != name || current.Stack != stack || current.Type != "basicio" {
					resultErr = errors.Join(resultErr, errors.New("fixture identity changed; cleanup refused"))
					return
				}
				err = c.DeleteFunction(cleanup, project, ownedID)
				if err == nil {
					err = c.WaitFunctionDeleted(cleanup, project, ownedID)
				}
			}
			if err != nil && !errors.Is(err, ErrFunctionNotFound) {
				resultErr = errors.Join(resultErr, errors.New("fixture cleanup failed; inspect the generated name and exact ID"))
				return
			}
		}
		// Require stable baseline restoration, even after partial or ambiguous creation.
		stable := 0
		for stable < 3 {
			remaining, err := c.ListFunctions(cleanup, project)
			if err != nil {
				resultErr = errors.Join(resultErr, err)
				return
			}
			matches := len(remaining) == len(original)
			for _, item := range remaining {
				if !original[item.ID] {
					matches = false
				}
			}
			if matches {
				stable++
			} else {
				stable = 0
			}
			if stable < 3 {
				if err = c.wait(cleanup, 2*time.Second); err != nil {
					resultErr = errors.Join(resultErr, errors.New("fixture inventory not restored; inspect the generated fixture before retrying"))
					return
				}
			}
		}
	}()
	desired := FunctionDeployment{FunctionMetadata: FunctionMetadata{Name: name, Stack: stack, Type: "basicio"}}
	desired.Configuration.Memory = 128
	desired.Configuration.Environment.Variables = map[string]string{}
	for phase := 0; phase < 4; phase++ {
		message := name + "-first"
		if phase == 2 {
			message = name + "-updated"
		}
		archive, err := executionFixtureArchive(name, stack, message)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(archive)
		hash := hex.EncodeToString(digest[:])
		var deployed *FunctionDeployment
		if phase == 1 {
			desired.Configuration.Memory = 256
			desired.Configuration.Environment.Variables = map[string]string{"FIXTURE": "constant"}
			deployed, err = c.UpdateFunctionConfiguration(ctx, project, ownedID, desired, hash)
		} else {
			if phase == 3 {
				desired.Configuration.Memory = 128
			}
			desired.Configuration.Environment.Variables = map[string]string{}
			deployed, err = c.DeployFunction(ctx, project, ownedID, desired, archive)
		}
		if deployed != nil && ValidID(string(deployed.ID)) {
			if original[deployed.ID] {
				return errors.New("create returned a pre-existing ID; refusing invocation and cleanup")
			}
			ownedID = string(deployed.ID)
		}
		if err != nil {
			return err
		}
		if _, err = c.WaitFunctionDeployment(ctx, project, ownedID, desired, hash); err != nil {
			return err
		}
		if err = waitFunctionFixtureOutput(ctx, c, project, ownedID, hash, message); err != nil {
			return fmt.Errorf("execution phase %d: %w", phase, err)
		}
	}
	return nil
}

var errFixtureRuntimeStale = errors.New("runtime still returned the previous generated fixture output")

// Only a known prior constant response is retried. Arbitrary failures and
// ambiguous requests stop immediately; no application Function is invoked here.
func waitFunctionFixtureOutput(ctx context.Context, c *Client, project, id, hash, expected string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stable := 0
	for stable < 3 {
		err := executeFunctionFixture(ctx, c, project, id, hash, expected)
		if err == nil {
			stable++
		} else if errors.Is(err, errFixtureRuntimeStale) {
			stable = 0
		} else {
			return err
		}
		if stable < 3 {
			if err := c.wait(ctx, 2*time.Second); err != nil {
				return err
			}
		}
	}
	return nil
}

func executeFunctionFixture(ctx context.Context, c *Client, project, id, hash, expected string) error {
	actual, err := c.FunctionSHA256(ctx, project, id)
	if err != nil {
		return err
	}
	if actual != hash {
		return errors.New("fixture artifact changed; invocation refused")
	}
	path, err := functionPath(project, id)
	if err != nil {
		return err
	}
	// Ambiguous POST failures are not retried. Only generated constant-output code is invoked.
	response, err := c.request(ctx, http.MethodPost, path+"/execute", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("fixture invocation failed; response body withheld")
	}
	var output struct {
		Output string `json:"output"`
	}
	if decode(response.Body, &output) != nil {
		return errors.New("fixture runtime response was malformed; response body withheld")
	}
	if (strings.HasSuffix(expected, "-updated") && output.Output == strings.TrimSuffix(expected, "-updated")+"-first") ||
		(strings.HasSuffix(expected, "-first") && output.Output == strings.TrimSuffix(expected, "-first")+"-updated") {
		return errFixtureRuntimeStale
	}
	if output.Output != expected {
		return errors.New("fixture runtime output did not match a generated marker; response body withheld")
	}
	return nil
}
