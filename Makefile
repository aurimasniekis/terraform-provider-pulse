SHELL         := /usr/bin/env bash
GO            ?= go
BIN           := bin
VERSION       ?= dev
LDFLAGS       := -s -w -X main.version=$(VERSION)
TFPLUGINDOCS  ?= $(GO) run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0
GOLANGCI_LINT ?= golangci-lint

# Pulse source the acceptance tests run against (exported read-only from a local checkout).
PULSE_REPO    ?= ../pulse
PULSE_COMMIT  ?= 3b2712f

.DEFAULT_GOAL := help
.PHONY: help build install test testacc lint fmt vet docs docs-check pulse-src pulse-bin homelab clean

help: ## List targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build bin/terraform-provider-pulse
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/terraform-provider-pulse .

install: ## go install the provider (for dev_overrides)
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' .

test: ## Unit tests (client against httptest, schema and config validation)
	$(GO) test ./...

testacc: pulse-bin ## Acceptance tests against a real nats-server and `pulsed serve` started by TestMain
	TF_ACC=1 $(GO) test ./... -count=1 -timeout 30m $(TESTARGS)

vet: ## go vet
	$(GO) vet ./...

lint: ## golangci-lint
	$(GOLANGCI_LINT) run ./...

fmt: ## Format Go and Terraform sources
	$(GOLANGCI_LINT) fmt ./...
	terraform fmt -recursive examples

docs: ## Generate docs/ with tfplugindocs
	$(TFPLUGINDOCS) generate --provider-name pulse

docs-check: docs ## Fail when generated docs are out of date (needs git)
	$(TFPLUGINDOCS) validate --provider-name pulse
	git diff --exit-code -- docs

pulse-src: ## Export Pulse at PULSE_COMMIT into .pulse-src (read-only use of PULSE_REPO)
	rm -rf .pulse-src && mkdir -p .pulse-src
	git -C $(PULSE_REPO) archive $(PULSE_COMMIT) | tar -x -C .pulse-src

pulse-bin: ## Build bin/pulsed and bin/nats-server (the version Pulse pins) from .pulse-src
	@test -d .pulse-src || $(MAKE) pulse-src
	cd .pulse-src && CGO_ENABLED=0 $(GO) build -o ../$(BIN)/pulsed ./cmd/pulsed
	cd .pulse-src && CGO_ENABLED=0 $(GO) build -o ../$(BIN)/nats-server github.com/nats-io/nats-server/v2

homelab: build pulse-bin ## Run examples/homelab (init/plan/apply/destroy) against a throwaway pulsed
	scripts/verify-homelab.sh

clean: ## Remove build output
	rm -rf $(BIN) dist
