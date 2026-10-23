// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const fullStackFixtureSource = `const http=require('http'),catalyst=require('zcatalyst-sdk-node');
http.createServer(async(req,res)=>{
 res.setHeader('Cache-Control','no-store');
 if(req.url==='/'){res.setHeader('Content-Type','text/html');res.end('<!doctype html><title>Terraform Catalyst fixture</title><h1>Website fixture</h1>');return;}
 if(req.url!=='/api/health'||req.headers['x-fixture-key']!==process.env.FIXTURE_KEY){res.statusCode=403;res.end();return;}
 let stage='initialize',row,table,result;
 try{
  const app=catalyst.initialize(req,{scope:'admin'});
  stage='nosql';const found=await app.nosql().table(process.env.NOSQL_TABLE_ID).fetchItem({keys:[{pk:{S:process.env.CONFIG_PK},sk:{S:'site'}}],consistent_read:true});
  if(!found.get||found.get.length!==1||found.get[0].item.get('value')!=='first')throw Error();
  stage='stratus';const buckets=await app.stratus().listBuckets(),bucket=buckets.find(b=>b.getName()===process.env.STRATUS_BUCKET);if(!bucket)throw Error();
  const stream=await bucket.getObject(process.env.STRATUS_KEY);let content='';for await(const chunk of stream){content+=chunk.toString();if(content.length>64)throw Error();}if(content!=='fixture')throw Error();
  stage='datastore_metadata';table=await app.datastore().getTableDetails(process.env.DATASTORE_TABLE_ID);if(table.toJSON().table_name!=='TerraformProviderDataStoreTests')throw Error();
  stage='datastore_config';if((await table.getRow(process.env.DATASTORE_CONFIG_ROW_ID)).message!==process.env.CONFIG_PK+'config')throw Error();
  stage='datastore_create';row=await table.insertRow({message:process.env.CONFIG_PK});
  stage='datastore_read';if((await table.getRow(row.ROWID)).message!==process.env.CONFIG_PK)throw Error();
  stage='datastore_update';await table.updateRow({ROWID:row.ROWID,message:process.env.CONFIG_PK+'updated'});if((await table.getRow(row.ROWID)).message!==process.env.CONFIG_PK+'updated')throw Error();
  result={ok:true,nosql:true,stratus:true,datastore:true};
 }catch(_){result={ok:false,stage};}
 finally{if(row&&table){try{await table.deleteRow(row.ROWID);}catch(_){result={ok:false,stage:'datastore_cleanup'};}}}
 res.setHeader('Content-Type','application/json');res.statusCode=result.ok?200:503;res.end(JSON.stringify(result));
}).listen(process.env.X_ZOHO_CATALYST_LISTEN_PORT||9000,'0.0.0.0');
`

func fullStackFixtureArchive(t *testing.T, modules string) string {
	t.Helper()
	var pkg struct{ Name, Version string }
	raw, err := os.ReadFile(filepath.Join(modules, "zcatalyst-sdk-node", "package.json"))
	if err != nil || json.Unmarshal(raw, &pkg) != nil || pkg.Name != "zcatalyst-sdk-node" || pkg.Version != "3.4.0" {
		t.Fatal("requires official pinned zcatalyst-sdk-node 3.4.0 fixture dependencies")
	}
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	put := func(name string, body []byte) error {
		w, err := writer.Create(name)
		if err == nil {
			_, err = w.Write(body)
		}
		return err
	}
	for name, body := range map[string]string{"index.js": fullStackFixtureSource, "package.json": `{"name":"terraform-website-fixture","private":true,"main":"index.js"}`} {
		if err = put(name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	err = filepath.WalkDir(modules, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("fixture dependencies must contain only regular files")
		}
		rel, err := filepath.Rel(modules, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return put("node_modules/"+filepath.ToSlash(rel), body)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = validateAppSailArchive(content.Bytes(), "node24"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "website.zip")
	if err = os.WriteFile(target, content.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestAccFullStackWebsite(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" || os.Getenv("CATALYST_TEST_ALLOW_FULLSTACK_CREATE") != "1" {
		t.Skip("requires explicit isolated full-stack application opt-in")
	}
	if os.Getenv("CATALYST_DATA_CENTER") != "in" || os.Getenv("CATALYST_ENVIRONMENT") != "Development" {
		t.Fatal("requires isolated India Development")
	}
	project, ds, nosql, bucket := os.Getenv("CATALYST_TEST_PROJECT_ID"), os.Getenv("CATALYST_TEST_DATASTORE_TABLE_ID"), os.Getenv("CATALYST_TEST_NOSQL_TABLE_ID"), os.Getenv("CATALYST_TEST_BUCKET")
	client, err := catalyst.New(catalyst.Config{AccessToken: os.Getenv("CATALYST_ACCESS_TOKEN"), OrganizationID: os.Getenv("CATALYST_ORGANIZATION_ID"), DataCenter: "in", Environment: "Development"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := client.GetDataStoreSchema(t.Context(), project, ds)
	if err != nil || schema.Name != "TerraformProviderDataStoreTests" {
		t.Fatal("requires dedicated Data Store fixture table")
	}
	meta, err := client.GetNoSQLTable(t.Context(), project, nosql)
	if err != nil || meta.Name != "TerraformProviderNoSQLTests" {
		t.Fatal("requires dedicated NoSQL fixture table")
	}
	if _, err = client.GetStratusBucket(t.Context(), project, bucket); err != nil {
		t.Fatal(err)
	}
	baseline, err := client.ListAppSails(t.Context(), project)
	if err != nil {
		t.Fatal(err)
	}
	registerAppSailBaselineCheck(t, client, project, baseline)
	name := "tfacc" + strconv.FormatInt(time.Now().UnixNano(), 36)
	key := name + "/asset.txt"
	var secret [32]byte
	if _, err = rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	fixtureKey := hex.EncodeToString(secret[:])
	source := fullStackFixtureArchive(t, os.Getenv("CATALYST_TEST_WEBSITE_NODE_MODULES"))
	asset := filepath.Join(t.TempDir(), "asset.txt")
	if err = os.WriteFile(asset, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupFullStackDataStoreRows(t, project, ds, name) })
	app := strings.Replace(appsailConfig(project, name, source, false), "catalyst_auth = true", "catalyst_auth = false", 1)
	app = strings.Replace(app, "environment_variables = {}", fmt.Sprintf(`environment_variables = { FIXTURE_KEY=%q, CONFIG_PK=%q, DATASTORE_TABLE_ID=%q, DATASTORE_CONFIG_ROW_ID=catalyst_datastore_row.test.row_id, NOSQL_TABLE_ID=%q, STRATUS_BUCKET=%q, STRATUS_KEY=%q }`, fixtureKey, name, ds, nosql, bucket, key), 1)
	app = strings.Replace(app, `resource "catalyst_appsail" "test" {`, `resource "catalyst_appsail" "test" {
 stack="node24"
 depends_on=[catalyst_nosql_item.test,catalyst_stratus_object.test]`, 1)
	rowConfig := strings.TrimPrefix(dataStoreRowConfig(project, ds, `{"message":"`+name+`config"}`), "provider \"catalyst\" {}\n")
	config := noSQLItemConfig(project, nosql, name, false) + rowConfig + "\n" + strings.TrimPrefix(stratusObjectConfig(project, bucket, key, asset, "text/plain"), "provider \"catalyst\" { data_center = \"in\" }\n") + strings.TrimPrefix(app, "provider \"catalyst\" {}\n")
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"catalyst": providerserver.NewProtocol6WithError(New("test")())}, CheckDestroy: func(state *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		keys, _ := json.Marshal(map[string]any{"pk": map[string]string{"S": name}, "sk": map[string]string{"S": "site"}})
		if _, err := client.ReadNoSQLItem(ctx, project, nosql, string(keys)); !errors.Is(err, catalyst.ErrNoSQLItemNotFound) {
			return errors.New("full-stack NoSQL fixture absence unverified")
		}
		if _, err := client.GetStratusObject(ctx, project, bucket, key); !errors.Is(err, catalyst.ErrStratusObjectNotFound) {
			return errors.New("full-stack asset absence unverified")
		}
		if _, err := client.GetDataStoreSchema(ctx, project, ds); err != nil {
			return err
		}
		if row := state.RootModule().Resources["catalyst_datastore_row.test"]; row != nil {
			if _, err := client.GetDataStoreRow(ctx, project, ds, row.Primary.Attributes["row_id"]); !errors.Is(err, catalyst.ErrDataStoreRowNotFound) {
				return errors.New("full-stack managed Data Store row absence unverified")
			}
		}
		return nil
	}, Steps: []resource.TestStep{{Config: config, Check: func(state *terraform.State) error {
		endpoint, err := url.Parse(state.RootModule().Resources["catalyst_appsail.test"].Primary.Attributes["url"])
		if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Port() != "" || !strings.HasSuffix(endpoint.Hostname(), ".catalystappsail.in") {
			return errors.New("unexpected fixture application origin")
		}
		endpoint.Path = "/api/health"
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
		defer cancel()
		hc := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		lastStage := "startup"
		for {
			req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
			if err != nil {
				return errors.New("could not build fixture request")
			}
			req.Header.Set("x-fixture-key", fixtureKey)
			res, err := hc.Do(req)
			if err == nil {
				var result struct {
					OK, NoSQL, Stratus, DataStore bool
					Stage                         string
				}
				decodeErr := json.NewDecoder(io.LimitReader(res.Body, 2048)).Decode(&result)
				_ = res.Body.Close()
				if decodeErr == nil {
					switch result.Stage {
					case "initialize", "nosql", "stratus", "datastore_metadata", "datastore_config", "datastore_create", "datastore_read", "datastore_update", "datastore_cleanup":
						lastStage = result.Stage
					}
					if res.StatusCode == 200 && result.OK && result.NoSQL && result.Stratus && result.DataStore {
						frontend := *endpoint
						frontend.Path = "/"
						frontReq, buildErr := http.NewRequestWithContext(ctx, "GET", frontend.String(), nil)
						if buildErr != nil {
							return errors.New("frontend request construction failed")
						}
						frontRes, frontErr := hc.Do(frontReq)
						if frontErr == nil {
							body, readErr := io.ReadAll(io.LimitReader(frontRes.Body, 1024))
							_ = frontRes.Body.Close()
							if readErr == nil && frontRes.StatusCode == 200 && strings.Contains(string(body), "<title>Terraform Catalyst fixture</title>") {
								t.Log("Frontend and AppSail backend verified NoSQL configuration, private Stratus content and Data Store create/read/update/delete")
								return nil
							}
						}
					}
				}
			}
			timer := time.NewTimer(3 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("full-stack fixture health did not converge; stage=%s", lastStage)
			case <-timer.C:
			}
		}
	}}, {Config: config, PlanOnly: true}}})
}

// The application normally deletes its row in finally. Independently recover
// only rows with this generated fixture's exact message, without reading logs.
func cleanupFullStackDataStoreRows(t *testing.T, project, table, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	call := func(method, path string, payload any) (json.RawMessage, error) {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, "https://api.catalyst.zoho.in/baas/v1/project/"+project+path, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("fixture cleanup request construction failed")
		}
		req.Header.Set("Authorization", "Zoho-oauthtoken "+os.Getenv("CATALYST_ACCESS_TOKEN"))
		req.Header.Set("CATALYST-ORG", os.Getenv("CATALYST_ORGANIZATION_ID"))
		req.Header.Set("Environment", "Development")
		req.Header.Set("Content-Type", "application/json")
		hc := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := hc.Do(req)
		if err != nil {
			return nil, errors.New("fixture row cleanup request failed")
		}
		defer res.Body.Close()
		var e struct {
			Status string
			Data   json.RawMessage
		}
		if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(&e) != nil || e.Status != "success" {
			return nil, errors.New("fixture row cleanup response invalid")
		}
		return e.Data, nil
	}
	query := "SELECT ROWID, message FROM TerraformProviderDataStoreTests WHERE message = '" + name + "' OR message = '" + name + "updated' OR message = '" + name + "config'"
	raw, err := call("POST", "/query", map[string]string{"query": query})
	if err != nil {
		t.Error(err)
		return
	}
	var rows []map[string]struct {
		ID      catalyst.ID `json:"ROWID"`
		Message string      `json:"message"`
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) > 10 {
		t.Error("fixture row cleanup query incomplete")
		return
	}
	for _, wrapper := range rows {
		row, ok := wrapper["TerraformProviderDataStoreTests"]
		if !ok || !catalyst.ValidID(string(row.ID)) || (row.Message != name && row.Message != name+"updated" && row.Message != name+"config") {
			t.Error("fixture cleanup refused foreign row")
			return
		}
		if _, err = call("DELETE", "/table/"+table+"/row/"+string(row.ID), nil); err != nil {
			t.Error(err)
			return
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		raw, err = call("POST", "/query", map[string]string{"query": query})
		var remaining []json.RawMessage
		if err != nil || json.Unmarshal(raw, &remaining) != nil || len(remaining) != 0 {
			t.Errorf("application row absence unverified: %v", err)
			return
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				t.Error("row cleanup confirmation timed out")
				return
			case <-timer.C:
			}
		}
	}
	t.Log("Application row cleanup verified; recovered row count:", len(rows))
}
