.DEFAULT_GOAL := build

build:
	go build -o bin/terraform-provider-catalyst .

install:
	go install .

dev: build
	sh scripts/setup-dev.sh

verify: dev
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

testacc-discovery:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccDiscoveryDataSources$$' ./internal/provider

testacc-cron:
	TF_ACC=1 go test -race -count=1 -v -timeout=5m -run '^TestAccWebhookCronResource$$' ./internal/provider

.PHONY: build install dev verify lint generate fmt test testacc testacc-discovery testacc-cron

# Dependency checks report differences without rewriting the module files.
deps-check:
	go mod tidy -diff
	cd tools && go mod tidy -diff

check: deps-check test-release-tools
	test -z "$$(gofmt -l main.go internal tools/tools.go)"
	terraform fmt -check -recursive examples/
	go vet ./...
	$(MAKE) test verify

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

testacc-appsail:
	TF_ACC=1 go test -race -count=1 -timeout=5m -run '^TestAccAppSail$$' ./internal/provider

.PHONY: testacc-appsail

testacc-gateway:
	TF_ACC=1 go test -race -count=1 -timeout=15m -run '^TestAccGatewayResources$$' ./internal/provider

.PHONY: testacc-gateway
