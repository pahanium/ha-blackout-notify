# Project Instructions for AI Assistant

This document contains guidelines for AI assistants working on this Home Assistant add-on project.

## Project Overview

This is a **Home Assistant Add-on** called **Blackout Notify** that monitors power grid status and sends Telegram notifications when power outages occur or power is restored. The add-on is written in **Go** and packaged as a Docker container.

## Project Structure

```
ha-blackout-notify/
├── .github/workflows/       # CI/CD pipelines
├── scripts/                 # Build scripts
├── blackout-notify/         # Main add-on directory
│   ├── config.yaml          # HA add-on configuration
│   ├── Dockerfile           # Multi-stage Docker build
│   ├── DOCS.md              # User documentation
│   ├── CHANGELOG.md         # Version history
│   ├── rootfs/
│   │   └── run.sh           # Container entry point (uses bashio)
│   └── src/                 # Go source code
│       ├── go.mod
│       ├── go.sum
│       ├── cmd/bot/main.go  # Application entry point
│       └── internal/
│           ├── bot/         # Telegram bot logic
│           ├── config/      # Configuration handling
│           ├── homeassistant/ # HA API client (REST + WebSocket)
│           ├── notifications/ # Notification formatting and sending
│           ├── watcher/     # Power state monitoring
│           └── logger/      # Logging utilities
├── docker-compose.dev.yaml  # Local development
├── .env.example             # Environment variables template
├── repository.yaml          # HA add-on repository metadata
├── README.md                # Repository overview
└── DEVELOPMENT.md           # Development guide
```

## Code Guidelines

### Language
- **All code comments must be in English**
- **All documentation must be in English**
- Use clear, concise English for variable names and function names
- User-facing notification messages can be in Ukrainian

### Go Conventions
- Follow standard Go project layout
- Each file must have exactly ONE `package` declaration at the top
- Use `internal/` for private packages
- Keep `cmd/` for application entry points
- Write tests with `_test.go` suffix in the same package

### Package Declaration Rules
**CRITICAL**: Each Go file must start with exactly one package declaration:
```go
package main  // correct

// NOT:
package main
package main  // WRONG - duplicate declaration
```

### Error Handling
- Always wrap errors with context: `fmt.Errorf("failed to X: %w", err)`
- Use logger package for logging, not fmt.Println

### Configuration
- All configuration comes from environment variables
- Use `internal/config` package to load and validate config
- Required env vars: `TELEGRAM_TOKEN`, `HA_TOKEN`
- Optional env vars: `HA_API_URL`, `LOG_LEVEL`, `POLLING_INTERVAL`, `ALLOWED_CHAT_IDS`

## Build & Test Commands

```bash
# Navigate to source directory
cd telegram-bot/src

# Download dependencies
go mod tidy

# Run tests
go test ./...

# Run tests with coverage
go test -v -race -coverprofile=coverage.txt ./...

# Build binary
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/telegram-bot ./cmd/bot

# Build Docker image locally
cd .. && docker build --build-arg BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.18 -t local/telegram-bot .
```

## Home Assistant Add-on Specifics

### API Access
- Inside the add-on container, use `http://supervisor/core/api` as HA API URL
- The `SUPERVISOR_TOKEN` env var is automatically provided by HA Supervisor
- Set `homeassistant_api: true` in config.yaml to enable API access

### Entry Point
- `rootfs/run.sh` uses bashio for reading HA add-on options
- The script sets environment variables and launches the Go binary

### Configuration Schema
- User options are defined in `config.yaml` under `options` and `schema`
- Options are read via bashio in `run.sh` and passed as env vars

## Testing Locally

### Without Home Assistant
```bash
export TELEGRAM_TOKEN="your_token"
export HA_API_URL="http://your-ha:8123/api"
export HA_TOKEN="your_long_lived_token"
export LOG_LEVEL="debug"
./telegram-bot/bin/telegram-bot
```

### With Docker Compose
```bash
cp .env.example .env
# Edit .env with your values
docker compose -f docker-compose.dev.yaml up --build
```

## Common Issues

### Duplicate package declarations
If you see errors like `expected ';', found package`, check that each .go file has only ONE package declaration.

### go.sum checksum mismatch
Delete go.sum and run `go mod tidy` to regenerate.

### Module not found errors
Run `go mod tidy` to ensure all dependencies are downloaded.

## Adding New Features

1. Create feature branch
2. Add/modify code in `internal/` packages
3. Update tests
4. Run `go test ./...`
5. Update CHANGELOG.md
6. Update version in config.yaml
7. Create pull request
