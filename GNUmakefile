.DEFAULT_GOAL := build

build:
	go build -o bin/terraform-provider-catalyst .

install:
	go install .

dev: build
	sh scripts/setup-dev.sh

verify: dev
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider providers schema -json > bin/provider-schema.json
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_project validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_projects validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/data-sources/catalyst_job_pools validate
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/resources/catalyst_webhook_cron validate
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
