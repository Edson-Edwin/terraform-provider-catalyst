// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAccAppSailCronContract(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_APPSAIL_CRON_CREATE") != "1" {
		t.Skip("requires explicit isolated AppSail scheduling contract opt-in")
	}
	project, pool := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_APPSAIL_POOL_ID")
	if !ValidID(project) || !ValidID(pool) || os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development project and AppSail pool")
	}
	c, err := New(Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pools, err := c.ListJobPools(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pools {
		if string(p.ID) == pool && p.Type == "AppSail" {
			found = true
		}
	}
	if !found {
		t.Fatal("requires a verified AppSail test pool")
	}
	baseline, err := c.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for key, value := range map[string]string{"index.js": "require('http').createServer((q,s)=>s.end('fixture')).listen(process.env.X_ZOHO_CATALYST_LISTEN_PORT || 9000);", "package.json": `{"name":"fixture","version":"1.0.0","main":"index.js"}`} {
		f, err := writer.Create(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	appID := ""
	cronIDs := []string{}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Minute)
		defer cancel()
		for _, id := range cronIDs {
			if err := c.DeleteCron(ctx, project, id); err != nil && !errors.Is(err, ErrCronNotFound) {
				t.Error(err)
			} else if err = c.waitCronTargetDeleted(ctx, project, id, "AppSail"); err != nil {
				t.Error(err)
			}
		}
		if appID != "" {
			if err := c.DeleteAppSail(ctx, project, appID); err != nil && !errors.Is(err, ErrAppSailNotFound) {
				t.Error(err)
			} else if err = c.WaitAppSailDeleted(ctx, project, appID); err != nil {
				t.Error(err)
			}
		}
		current, err := c.ListAppSails(ctx, project)
		if err != nil || len(current) != len(baseline) {
			t.Error("AppSail baseline not restored")
		}
	})
	auth := true
	desired := AppSailDeployment{AppSailMetadata: AppSailMetadata{Name: name, Stack: "node22"}, Configuration: AppSailConfiguration{Command: "node index.js", Port: 9000, Auth: &auth, Memory: 128}}
	desired.Configuration.Environment.Variables = map[string]string{}
	deployed, err := c.DeployAppSail(t.Context(), project, "", desired, archive.Bytes())
	if deployed != nil {
		appID = string(deployed.ID)
		t.Log("Created isolated AppSail fixture ID:", appID)
	}
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(archive.Bytes())
	if _, err = c.WaitAppSailDeployment(t.Context(), project, appID, desired, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	app, err := c.GetAppSail(t.Context(), project, appID)
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(app.URL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		t.Fatal("AppSail did not return a canonical HTTPS origin")
	}
	t.Log("AppSail target origin obtained from authenticated metadata; URL is not logged")
	disabled := false
	for i, path := range []string{"", "/health"} {
		value := Cron{Name: name + strconv.Itoa(i), Type: "CronExpression", ExecutionType: "pre-defined", Expression: "0 0 * * *", Enabled: &disabled, EndTime: 1893456000, Schedule: CronSchedule{Timezone: "GMT"}, Job: CronJob{Name: "terraform_job", PoolID: ID(pool), TargetType: "AppSail", TargetID: ID(appID), SourceType: "Cron", URL: strings.TrimRight(app.URL, "/") + path, Method: "get", Headers: map[string]string{}, Params: map[string]string{}}}
		created, err := c.CreateCron(t.Context(), project, value)
		if created != nil && ValidID(string(created.ID)) {
			cronIDs = append(cronIDs, string(created.ID))
		}
		if err != nil {
			t.Fatalf("create route fixture %d: %v", i, err)
		}
		current, err := c.GetAppSailCron(t.Context(), project, string(created.ID))
		if err != nil {
			t.Fatal(err)
		}
		if current.Job.TargetID != ID(appID) || current.Job.TargetType != "AppSail" || current.Job.Method != "get" {
			t.Fatal("AppSail schedule target identity changed")
		}
		if current.Job.URL == strings.TrimRight(app.URL, "/")+path {
			t.Log("AppSail cron preserves the verified absolute target URL")
		} else {
			route, parseErr := url.Parse(current.Job.URL)
			if parseErr != nil || route.Scheme != base.Scheme || route.Host != base.Host || route.Path != path {
				t.Fatal("unexpected AppSail route normalization")
			}
			t.Log("AppSail cron resolves the route against the verified target origin")
		}
	}
	t.Log("AppSail root and /health scheduling probes passed without invocation")
}
