// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestInfrastructureDetailsThroughTerraform(t *testing.T) {
	const pool = "catalyst_job_pool"
	const cors = "catalyst_stratus_bucket_cors"
	const bucket = "catalyst_stratus_bucket"
	const versions = "catalyst_stratus_object_versions"
	const webClient = "catalyst_web_client_versions"
	const environments = "catalyst_project_environments"
	const pipeline = "catalyst_pipeline"
	const smartTrain = "catalyst_smarttrain_materials"
	const organizations = "catalyst_organizations"
	const channels = "catalyst_datastream_channels"
	const poolID = "9007199254740993123"
	const poolPath = "/baas/v1/project/123/job_scheduling/jobpool/" + poolID
	const corsPath = "/baas/v1/project/123/bucket/cors?bucket_name=my-bucket"
	const bucketPath = "/baas/v1/project/123/bucket?bucket_name=my-bucket"
	const versionsPath = "/baas/v1/project/123/bucket/objects/versions?bucket_name=my-bucket&max_versions=100&object_key=assets%2Fdata.json"
	const webClientPath = "/baas/v1/project/123/webapp/history"
	const environmentsPath = "/baas/v1/project/123/environment"
	const pipelinePath = "/baas/v1/project/123/pipeline/9007199254740993123"
	const smartTrainPath = "/baas/v1/project/123/convokraft/api/v1/bots/456/rag_training_data?type=DEVELOPED"
	const channelPath = "/baas/v1/project/123/datastreams/channel"
	poolConfig := map[string]any{"project_id": "123", "id": poolID}
	corsConfig := map[string]any{"project_id": "123", "bucket_name": "my-bucket"}
	versionsConfig := map[string]any{"project_id": "123", "bucket_name": "my-bucket", "object_key": "assets/data.json"}
	webClientConfig := map[string]any{"project_id": "123"}
	pipelineConfig := map[string]any{"project_id": "123", "id": "9007199254740993123"}
	smartTrainConfig := map[string]any{"project_id": "123", "bot_id": "456"}
	channelConfig := map[string]any{"project_id": "123", "channel_id": "9007199254740993123"}
	channelState := map[string]any{"id": "9007199254740993123", "name": "news", "control_type": 0, "retention_hours": 48}
	const channelResponse = `{"project_id":123,"id":9007199254740993123,"name":"news","control_type":0,"retention":"48","created_by":{"email_id":"private@example.com"}}`
	poolState := func(memory, count any) map[string]any {
		return map[string]any{"project_id": "123", "id": poolID, "name": "My Pool", "type": "Function",
			"capacity_memory_mb": memory, "capacity_count": count}
	}
	corsState := func(rules []any) map[string]any {
		return map[string]any{"project_id": "123", "bucket_name": "my-bucket", "rules": rules}
	}
	versionsState := func(items []any) map[string]any {
		return map[string]any{"project_id": "123", "bucket_name": "my-bucket", "object_key": "assets/data.json", "versions": items}
	}
	for _, tc := range []struct {
		name, typeName, route, body string
		config, state               map[string]any
		status                      int
	}{
		{"capacity", pool, poolPath, `{"status":"success","data":{"id":9007199254740993123,"name":"My Pool","type":"Function","capacity":{"memory":"256","number":3},"created_by":{"email_id":"private@example.com"}}}`, poolConfig, poolState(256, 3), 200},
		{"capacity omitted", pool, poolPath, `{"status":"success","data":{"id":"9007199254740993123","name":"My Pool","type":"Function"}}`, poolConfig, poolState(nil, nil), 200},
		{"versioned bucket", bucket, bucketPath, `{"status":"success","data":[{"bucket_name":"my-bucket","bucket_url":"https://my-bucket-development.zohostratus.in","bucket_meta":{"versioning":true}}]}`, corsConfig,
			map[string]any{"project_id": "123", "bucket_name": "my-bucket", "url": "https://my-bucket-development.zohostratus.in", "versioning_enabled": true}, 200},
		{"unexpected bucket endpoint", bucket, bucketPath, `{"status":"success","data":[{"bucket_name":"my-bucket","bucket_url":"https://elsewhere.example","bucket_meta":{"versioning":true}}]}`, corsConfig, nil, 200},
		{"different pool", pool, poolPath, `{"status":"success","data":{"id":"1","name":"My Pool","type":"Function"}}`, poolConfig, nil, 200},
		{"invalid capacity", pool, poolPath, `{"status":"success","data":{"id":"9007199254740993123","name":"My Pool","type":"Function","capacity":{"memory":"1.5"}}}`, poolConfig, nil, 200},
		{"sorted CORS rules", cors, corsPath, `{"status":"success","data":[{"domain":"https://b.example","allowed_methods":["PUT","GET"]},{"domain":"https://a.example","allowed_methods":[]}]}`, corsConfig, corsState([]any{
			map[string]any{"domain": "https://a.example", "allowed_methods": []string{}},
			map[string]any{"domain": "https://b.example", "allowed_methods": []string{"GET", "PUT"}},
		}), 200},
		{"empty CORS rules", cors, corsPath, `{"status":"success","data":[]}`, corsConfig, corsState([]any{}), 200},
		{"incomplete CORS rule", cors, corsPath, `{"status":"success","data":[{"domain":"https://a.example"}]}`, corsConfig, nil, 200},
		{"omitted CORS rules", cors, corsPath, `{"status":"success","data":null}`, corsConfig, nil, 200},
		{"permission denied", cors, corsPath, "private@example.com", corsConfig, nil, 403},
		{"invalid pool ID", pool, "", "", map[string]any{"project_id": "123", "id": "../pool"}, nil, 0},
		{"invalid bucket name", cors, "", "", map[string]any{"project_id": "123", "bucket_name": "my-bucket&x=1"}, nil, 0},
		{"version metadata", versions, versionsPath, `{"status":"success","data":{"key":"assets/data.json","truncated":false,"version":[{"version_id":"latest","is_latest":true,"size":"12","last_modified":"May 22, 2024 12:20 PM","etag":"opaque"},{"version_id":"previous","is_latest":false}]}}`, versionsConfig,
			versionsState([]any{
				map[string]any{"version_id": "latest", "is_latest": true, "size_bytes": 12, "last_modified": "May 22, 2024 12:20 PM", "etag": "opaque"},
				map[string]any{"version_id": "previous", "is_latest": false, "size_bytes": nil, "last_modified": nil, "etag": nil},
			}), 200},
		{"empty version listing", versions, versionsPath, `{"status":"success","data":{"key":"assets/data.json","truncated":false,"version":[]}}`, versionsConfig, versionsState([]any{}), 200},
		{"invalid version object key", versions, "", "", map[string]any{"project_id": "123", "bucket_name": "my-bucket", "object_key": "../forbidden"}, nil, 0},
		{"client history allowlist", webClient, webClientPath, `{"status":"success","data":[{"history_id":9007199254740993123,"app_version":"1.1.0","status":true,"download":"ignored"},{"history_id":"123","app_version":"1.0.0"}]}`, webClientConfig,
			map[string]any{"project_id": "123", "versions": []any{
				map[string]any{"history_id": "123", "version": "1.0.0", "live": nil},
				map[string]any{"history_id": "9007199254740993123", "version": "1.1.0", "live": true},
			}}, 200},
		{"empty client history", webClient, webClientPath, `{"status":"success","data":[]}`, webClientConfig,
			map[string]any{"project_id": "123", "versions": []any{}}, 200},
		{"duplicate history identity", webClient, webClientPath, `{"status":"success","data":[{"history_id":"123","app_version":"1.0.0"},{"history_id":123,"app_version":"1.1.0"}]}`, webClientConfig, nil, 200},
		{"denied client history", webClient, webClientPath, "private@example.com", webClientConfig, nil, 403},
		{"environment metadata", environments, environmentsPath, `{"status":"success","data":[{"env_zgid":9007199254740993123,"env_name":"Development","is_default":true,"env_type":0,"env_status":"Active","project_details":{"id":123},"api_key":"excluded"},{"env_zgid":"2","env_name":"Production"}]}`, webClientConfig,
			map[string]any{"project_id": "123", "environments": []any{
				map[string]any{"id": "2", "name": "Production", "status": nil, "type": nil, "is_default": nil},
				map[string]any{"id": "9007199254740993123", "name": "Development", "status": "Active", "type": 0, "is_default": true},
			}}, 200},
		{"environment in another project", environments, environmentsPath, `{"status":"success","data":[{"env_zgid":"2","env_name":"Production","project_details":{"id":"234"}}]}`, webClientConfig, nil, 200},
		{"pipeline metadata", pipeline, pipelinePath, `{"status":"success","data":{"pipeline_id":9007199254740993123,"name":"build","project_details":{"id":"123"},"pipeline_status":"Active","created_by":{"email_id":"private@example.com"},"mask_regex":["excluded"]}}`, pipelineConfig,
			map[string]any{"project_id": "123", "id": "9007199254740993123", "name": "build", "status": "Active"}, 200},
		{"wrong pipeline", pipeline, pipelinePath, `{"status":"success","data":{"pipeline_id":"2","name":"build","project_details":{"id":"123"}}}`, pipelineConfig, nil, 200},
		{"pipeline in another project", pipeline, pipelinePath, `{"status":"success","data":{"pipeline_id":"9007199254740993123","name":"build","project_details":{"id":"234"}}}`, pipelineConfig, nil, 200},
		{"SmartTrain metadata", smartTrain, smartTrainPath, `{"rag_training_data":[{"id":"9007199254740993123","title":"Documentation","source":"URL","status":"TRAINED","url":"https://example.com/?token=excluded","created_by":{"email":"private@example.com"}}]}`, smartTrainConfig,
			map[string]any{"project_id": "123", "bot_id": "456", "materials": []any{
				map[string]any{"id": "9007199254740993123", "title": "Documentation", "source": "URL", "status": "TRAINED"},
			}}, 200},
		{"no SmartTrain materials", smartTrain, smartTrainPath, `{"rag_training_data":[]}`, smartTrainConfig,
			map[string]any{"project_id": "123", "bot_id": "456", "materials": []any{}}, 200},
		{"malformed SmartTrain", smartTrain, smartTrainPath, `{"rag_training_data":null}`, smartTrainConfig, nil, 200},
		{"denied SmartTrain", smartTrain, smartTrainPath, "private@example.com", smartTrainConfig, nil, 403},
		{"invalid SmartTrain bot", smartTrain, "", "", map[string]any{"project_id": "123", "bot_id": "../456"}, nil, 0},
		{"organization identities", organizations, "/baas/v1/orgs", `{"status":"success","data":[{"id":9007199254740993123,"name":"Team","is_default":true,"url":"https://excluded.example"},{"id":"1","name":"Other"}]}`,
			map[string]any{}, map[string]any{"organizations": []any{
				map[string]any{"id": "1", "name": "Other", "is_default": nil},
				map[string]any{"id": "9007199254740993123", "name": "Team", "is_default": true},
			}}, 200},
		{"empty organizations", organizations, "/baas/v1/orgs", `{"status":"success","data":[]}`, map[string]any{}, map[string]any{"organizations": []any{}}, 200},
		{"organization access denied", organizations, "/baas/v1/orgs", "private@example.com", map[string]any{}, nil, 403},
		{"channel metadata", channels, channelPath + "/9007199254740993123", `{"status":"success","data":` + channelResponse + `}`, channelConfig,
			map[string]any{"project_id": "123", "channel_id": "9007199254740993123", "channels": []any{channelState}}, 200},
		{"channel inventory", channels, channelPath, `{"status":"success","data":[` + channelResponse + `,{"project_id":"123","id":"2","name":"alerts"}]}`, webClientConfig,
			map[string]any{"project_id": "123", "channel_id": nil, "channels": []any{
				map[string]any{"id": "2", "name": "alerts", "control_type": nil, "retention_hours": nil}, channelState,
			}}, 200},
		{"empty channel inventory", channels, channelPath, `{"status":"success","data":[]}`, webClientConfig,
			map[string]any{"project_id": "123", "channel_id": nil, "channels": []any{}}, 200},
		{"channel from another project", channels, channelPath, `{"status":"success","data":[{"project_id":"234","id":"2","name":"unexpected"}]}`, webClientConfig, nil, 200},
		{"wrong channel", channels, channelPath + "/9007199254740993123", `{"status":"success","data":{"project_id":"123","id":"2","name":"unexpected"}}`, channelConfig, nil, 200},
		{"denied channel inventory", channels, channelPath, "private@example.com", webClientConfig, nil, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvironment(t)
			calls := 0
			organizationID := "321"
			if tc.typeName == organizations {
				organizationID = ""
			}
			server := providerserver.NewProtocol6(&CatalystProvider{httpClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.route == "" || r.Method != http.MethodGet || r.URL.String() != "https://api.catalyst.zoho.in"+tc.route ||
					r.Header.Get("CATALYST-ORG") != organizationID || r.Header.Get("Environment") != "Development" {
					t.Fatalf("unexpected Catalyst request: %s %s", r.Method, r.URL)
				}
				if tc.typeName == smartTrain && (r.Header.Get("X-CATALYST-projectid") != "123" || r.Header.Get("X-CATALYST-Environment") != "Development") {
					t.Fatal("missing SmartTrain routing headers")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}})()
			configured, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{
				Config: jsonConfig(t, map[string]any{"access_token": "fixture", "data_center": "in", "organization_id": organizationID}),
			})
			if err != nil {
				t.Fatal(err)
			}
			checkDiagnostics(t, configured.Diagnostics)
			config := jsonConfig(t, tc.config)
			validation, err := server.ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{TypeName: tc.typeName, Config: config})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(validation.Diagnostics) != (tc.route == "") || calls != 0 {
				t.Fatalf("unexpected validation: %v, API calls: %d", validation.Diagnostics, calls)
			}
			read, err := server.ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{TypeName: tc.typeName, Config: config})
			if err != nil {
				t.Fatal(err)
			}
			if tc.route != "" && calls != 1 {
				t.Fatalf("expected a single read, got %d", calls)
			}
			if tc.state == nil {
				if !hasError(read.Diagnostics) {
					t.Fatal("expected a diagnostic")
				}
				for _, d := range read.Diagnostics {
					if strings.Contains(d.Detail, "private@example.com") {
						t.Fatal("diagnostic leaked an API response")
					}
				}
				return
			}
			checkDiagnostics(t, read.Diagnostics)
			schema, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if read.State == nil {
				t.Fatal("read returned no state")
			}
			stateType := schema.DataSourceSchemas[tc.typeName].ValueType()
			state, err := read.State.Unmarshal(stateType)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonConfig(t, tc.state).Unmarshal(stateType)
			if err != nil {
				t.Fatal(err)
			}
			if !state.Equal(expected) {
				t.Fatalf("expected %v, got %v", expected, state)
			}
		})
	}
}
