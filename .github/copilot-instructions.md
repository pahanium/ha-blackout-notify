# Copilot Instructions for Blackout Notify

## Project Overview

Home Assistant add-on written in **Go** that monitors power grid status via HA WebSocket API and sends Telegram notifications for power outages/restorations. Packaged as a multi-arch Docker container using HA's s6-overlay base images.

## Architecture

```
cmd/bot/main.go          → Entry point: loads config, wires components, handles signals
internal/config/         → Environment-based configuration (HA passes options as env vars)
internal/bot/            → Telegram bot command handler
internal/homeassistant/  → REST client + WebSocket client for HA API
internal/watcher/        → Power state monitoring with debouncing
internal/notifications/  → Notification formatting and delivery
internal/logger/         → Simple leveled logging
```

**Data flow**: `rootfs/run.sh` (bashio) → env vars → `config.Load()` → `main.go` creates `haClient`, `wsClient`, `bot`, `watcher`, `notifSvc` → watcher subscribes to HA WebSocket events → triggers notifications

## Key Patterns

### Configuration
All config via environment variables parsed in `internal/config/config.go`. Inside HA add-on container:
- Use `http://supervisor/core/api` as HA API URL
- `SUPERVISOR_TOKEN` is auto-injected by HA when `homeassistant_api: true`
- Chat IDs parsed from comma/space-separated strings or JSON arrays
- **Security**: Bot commands disabled by default (empty `allowed_chat_ids`)

### Home Assistant Integration
- **REST Client** (`client.go`): Entity state queries, service calls
- **WebSocket Client** (`websocket.go`): Real-time state change subscriptions with auto-reconnect
- For Supervisor API, WebSocket path is `/core/websocket`, not `/api/websocket`

### Error Handling
Always wrap errors with context:
```go
return fmt.Errorf("failed to connect to HA: %w", err)
```

### Language Convention
- **All code, comments, documentation in English**
- **Exception**: User-facing Telegram messages are in Ukrainian (see `notifications/service.go`)

## Build & Test Commands

```bash
cd blackout-notify/src

# Tests
go test -v ./...
go test -v -race -coverprofile=coverage.txt ./...

# Build
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot

# Docker build (from repo root)
./scripts/docker-build.sh amd64    # or aarch64, armv7
```

## Local Development

```bash
# Option 1: Go directly
export TELEGRAM_TOKEN="xxx" HA_API_URL="http://192.168.x.x:8123/api" HA_TOKEN="xxx" LOG_LEVEL="debug"
go run ./cmd/bot

# Option 2: Docker Compose
cp .env.example .env && nano .env
docker compose -f docker-compose.dev.yaml up --build
```

## HA Add-on Specifics

- `config.yaml`: Add-on metadata, options schema, version
- `rootfs/etc/services.d/blackout-notify/run`: s6-overlay entry point using bashio
- `Dockerfile`: Multi-stage build (golang:1.21-alpine → HA base image)

When updating add-on:
1. Bump `version` in `blackout-notify/config.yaml`
2. Update `CHANGELOG.md`
3. Run pre-deployment checklist (see README.md)

## Testing Patterns

Tests use standard Go testing with `httptest` for mocking HA API (see `client_test.go`). Set env vars before test runs:
```go
os.Setenv("TELEGRAM_TOKEN", "test_token")
defer os.Unsetenv("TELEGRAM_TOKEN")
```

## Common Issues

- **Duplicate package declarations**: Each `.go` file must have exactly ONE `package` declaration
- **go.sum mismatch**: Delete `go.sum`, run `go mod tidy`
- **WebSocket auth fails**: Check if using Supervisor URL vs external URL (different auth paths)
- **Add-on not appearing**: Restart HA Supervisor, check logs in Settings → System → Logs

## Documentation Structure

- `README.md` - Complete project documentation (overview, installation, development, troubleshooting)
- `blackout-notify/DOCS.md` - User-facing add-on documentation (shown in HA UI)
- `blackout-notify/CHANGELOG.md` - Version history
- `.github/copilot-instructions.md` - This file (AI assistant guidelines)