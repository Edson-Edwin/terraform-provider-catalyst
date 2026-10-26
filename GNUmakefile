.DEFAULT_GOAL := build

build:
	go build -o bin/terraform-provider-catalyst .

install:
	go install .

dev: build
	sh scripts/setup-dev.sh

verify: dev
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider_guardrails validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/all_services validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_datastore_row validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/gateway_advanced validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_auth_user_role validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_auth_user validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/full_stack validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_nosql_item validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_nosql_table validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_nosql_tables validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_datastore_table validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_project validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_appsail_cron validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/webhook_calendar validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/function_periodic validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/function_calendar validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/function_onetime validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_function_cron validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/background_function validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_stratus_object validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/advancedio validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_api_gateway validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_api_gateway_ruleset validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_appsail validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_function validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_functions validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider providers schema -json > bin/provider-schema.json
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_project validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_projects validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_job_pools validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_datastore_tables validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_filestore_folders validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_cache_segments validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_stratus_buckets validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_webhook_cron validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_filestore_file validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider plan -input=false -lock=false

lint:
	golangci-lint run

generate:
	cd tools && go generate -tags generate ./...

fmt:
	gofmt -w main.go internal tools/tools.go
	terraform fmt -recursive examples/

test:
	TF_ACC=0 go test -race -count=1 ./...

testacc:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccProjectDataSource$$' ./internal/provider

# Full disposable project lifecycle; explicit write opt-in and account eligibility required.
testacc-project:
	TF_ACC=1 go test -race -count=1 -v -timeout=20m -run '^TestAccProjectResource$$' ./internal/provider

test-project:
	TF_ACC=0 go test -race -count=1 -timeout=5m -run '^TestProject' ./internal/catalyst ./internal/provider

.PHONY: testacc-project test-project

testacc-discovery:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccDiscoveryDataSources$$' ./internal/provider

# Read-only regional smoke tests; credentials are supplied independently per DC.
testacc-regional-auth:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccRegionalAuthentication$$' ./internal/catalyst

# A complete gate cannot silently pass with a subset of regions or auth modes.
testacc-regional-auth-all:
	CATALYST_TEST_REGIONS=all $(MAKE) testacc-regional-auth

.PHONY: testacc-regional-auth testacc-regional-auth-all

# Public TLS handshakes only: no credentials, HTTP API calls or cloud mutations.
testacc-regional-tls:
	TF_ACC=1 CATALYST_TEST_REGIONAL_TLS=1 go test -race -count=1 -v -timeout=1m -run '^TestAccRegionalTLS$$' ./internal/catalyst

.PHONY: testacc-regional-tls

# Read metadata and signed content from existing objects; no object writes.
testacc-regional-stratus:
	TF_ACC=1 go test -race -count=1 -v -timeout=20m -run '^TestAccRegionalStratusRead$$' ./internal/catalyst

# Requires global and per-fixture write opt-ins; creates only disposable keys.
testacc-regional-stratus-lifecycle:
	TF_ACC=1 go test -race -count=1 -v -timeout=30m -run '^TestAccRegionalStratusLifecycle$$' ./internal/catalyst

# Complete read gate: all DCs, both environments, both authentication modes.
testacc-regional-all:
	CATALYST_TEST_REGIONS=all CATALYST_TEST_ENVIRONMENTS=all TF_ACC=1 go test -race -count=1 -v -timeout=30m -run '^TestAccRegional(Authentication|StratusRead)$$' ./internal/catalyst

testacc-regional-stratus-lifecycle-all:
	CATALYST_TEST_REGIONS=all CATALYST_TEST_ENVIRONMENTS=all $(MAKE) testacc-regional-stratus-lifecycle

.PHONY: testacc-regional-stratus testacc-regional-stratus-lifecycle testacc-regional-all testacc-regional-stratus-lifecycle-all

testacc-cron:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccWebhookCronResource$$' ./internal/provider

.PHONY: build install dev verify lint generate fmt test testacc testacc-discovery testacc-cron

# Dependency checks report differences without rewriting the module files.
deps-check:
	go mod tidy -diff
	cd tools && go mod tidy -diff

check: deps-check test-release-tools
	python3 -m unittest discover -s scripts -p 'test_*.py' -v
	test -z "$$(gofmt -l main.go internal tools/tools.go)"
	terraform fmt -check -recursive examples/
	go vet ./...
	$(MAKE) test verify
	python3 scripts/try-provider.py check

# Offline packaging: never signs or publishes. Requires GoReleaser v2.18.3.
release-check: deps-check test-release-tools
	goreleaser check
	goreleaser release --snapshot --clean --skip=sign --parallelism 2
	go run ./internal/cmd/provider-tools verify-release -snapshot dist

.PHONY: deps-check check release-check

test-release-tools:
	go test -race -count=1 ./internal/releasetools

.PHONY: test-release-tools

# Live writes are gated independently from read-only discovery.
testacc-inventory:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccInventoryDataSources$$' ./internal/provider

testacc-file:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccFilestoreFile$$' ./internal/provider

.PHONY: testacc-inventory testacc-file

coverage-status:
	go run ./internal/cmd/provider-tools coverage-status

.PHONY: coverage-status

testacc-function:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccFunction$$' ./internal/provider

.PHONY: testacc-function

testacc-function-execution:
	TF_ACC=1 go test -race -count=1 -timeout=20m -run '^TestAccFunctionExecution$$' ./internal/catalyst

.PHONY: testacc-function-execution

testacc-appsail:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccAppSail$$' ./internal/provider

.PHONY: testacc-appsail

testacc-gateway:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccGatewayResources$$' ./internal/provider

.PHONY: testacc-gateway

testacc-function-variants:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccFunctionVariants$$' ./internal/provider

.PHONY: testacc-function-variants

testacc-stratus-object:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccStratusObject$$' ./internal/provider

.PHONY: testacc-stratus-object

testacc-function-background:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccFunctionBackgroundVariants$$' ./internal/provider

.PHONY: testacc-function-background

testacc-function-cron:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccFunctionCron$$' ./internal/provider

.PHONY: testacc-function-cron

testacc-function-cron-schedules:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccFunctionCronSchedules$$' ./internal/provider

.PHONY: testacc-function-cron-schedules

testacc-webhook-cron-schedules:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccWebhookCronSchedules$$' ./internal/provider

.PHONY: testacc-webhook-cron-schedules

# Disabled schedules and temporary AppSail are limited to an isolated test project.
testacc-appsail-cron:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccAppSailCron(DefaultTimezone)?$$' ./internal/provider

.PHONY: testacc-appsail-cron

# Explicit live opt-ins and existing isolated prerequisites are required.
testacc-nosql-item:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccNoSQLItem$$' ./internal/provider

testacc-database-schema:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccDatabaseTableDataSources$$' ./internal/provider

.PHONY: testacc-nosql-item testacc-database-schema

testacc-full-stack:
	TF_ACC=1 go test -race -count=1 -timeout=10m -run '^TestAccFullStackWebsite$$' ./internal/provider

.PHONY: testacc-full-stack

testacc-appsail-languages:
	TF_ACC=1 go test -race -count=1 -timeout=30m -run '^TestAccAppSailManagedLanguages$$' ./internal/provider

testacc-appsail-health:
	TF_ACC=1 go test -race -count=1 -timeout=20m -run '^TestAccAppSailWebsiteHealth$$' ./internal/provider

.PHONY: testacc-appsail-languages testacc-appsail-health

# Uses an existing disposable end-user and roles; never sends invitations.
testacc-auth-user-role:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccAuthUserRole$$' ./internal/provider

.PHONY: testacc-auth-user-role

testacc-datastore-row:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccDataStoreRow$$' ./internal/provider

testacc-stratus-multipart:
	TF_ACC=1 go test -race -count=1 -timeout=10m -run '^TestAccStratusMultipart$$' ./internal/provider

.PHONY: testacc-datastore-row testacc-stratus-multipart
