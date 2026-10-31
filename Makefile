SRC     := blackout-notify/src
ARCH    ?= aarch64
LINT    := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
VULN    := golang.org/x/vuln/cmd/govulncheck@latest
COMPOSE := docker compose -f docker-compose.dev.yaml

.PHONY: help test lint vuln check build run docker dev-up dev-down dev-logs

help: ## Show available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-10s %s\n", $$1, $$2}'

test: ## Run tests with race detector
	cd $(SRC) && go test -race ./...

lint: ## gofmt check, go vet, golangci-lint (same version as CI)
	@cd $(SRC) && test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: gofmt -w $(SRC)" && exit 1)
	cd $(SRC) && go vet ./... && go run $(LINT) run ./...

vuln: ## Scan for known vulnerabilities
	cd $(SRC) && go run $(VULN) ./...

check: test lint vuln ## Everything CI checks, plus govulncheck

build: ## Build binary into blackout-notify/bin/
	cd $(SRC) && CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot

run: ## Run locally with .env from repo root
	cd $(SRC) && go run ./cmd/bot

docker: ## Build production add-on image (ARCH=aarch64|amd64|armv7)
	./scripts/docker-build.sh $(ARCH)

dev-up: ## Run in Docker with .env (stats DB kept in .data/)
	$(COMPOSE) up --build

dev-down: ## Stop Docker dev environment
	$(COMPOSE) down

dev-logs: ## Follow Docker dev logs
	$(COMPOSE) logs -f
