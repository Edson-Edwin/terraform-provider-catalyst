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
	TF_CLI_CONFIG_FILE="$(CURDIR)/.terraformrc.local" terraform -chdir=examples/provider plan -input=false -lock=false

lint:
	golangci-lint run

generate:
	cd tools && go generate -tags generate ./...

fmt:
	gofmt -w main.go internal/provider tools/tools.go
	terraform fmt -recursive examples/

test:
	go test -race -count=1 ./...

.PHONY: build install dev verify lint generate fmt test
