# Agent Guidelines for Blackout Notify

Home Assistant add-on written in **Go** that monitors power grid status via HA WebSocket API and sends Telegram notifications for power outages/restorations. Packaged as a multi-arch Docker container.

## Build & Test Commands

### Working Directory
All Go commands must be run from `blackout-notify/src/`:
```bash
cd blackout-notify/src
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test -v ./internal/config
go test -v ./internal/homeassistant
go test -v ./internal/watcher

# Run with race detector and coverage
go test -v -race -coverprofile=coverage.txt ./...

# Run single test function
go test -v -run TestIsChatAllowed ./internal/config
```

### Building
```bash
# Standard build
go build -o ../bin/blackout-notify ./cmd/bot

# Optimized build (production)
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot

# Docker build for specific architecture (from repo root)
./scripts/docker-build.sh amd64    # or aarch64, armv7
```

### Dependencies
```bash
# Update dependencies
go mod tidy

# Add new dependency
go get github.com/package/name

# If go.sum is corrupted
rm go.sum && go mod tidy
```

### Running Locally
```bash
# Direct Go execution
export TELEGRAM_TOKEN="xxx"
export HA_API_URL="http://192.168.1.100:8123/api"
export HA_TOKEN="xxx"
export LOG_LEVEL="debug"
go run ./cmd/bot

# Using Docker Compose (from repo root)
cp .env.example .env && nano .env
docker compose -f docker-compose.dev.yaml up --build
```

## Project Architecture

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

## Code Style Guidelines

### Language Convention
- **All code, comments, documentation in English**
- **Exception**: User-facing Telegram messages are in Ukrainian (see `notifications/service.go`)

### Imports Organization
Group imports in this order (separated by blank lines):
1. Standard library packages
2. External dependencies
3. Internal packages

```go
import (
    "context"
    "fmt"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

    "github.com/yourusername/haaddon/telegram-bot/internal/config"
    "github.com/yourusername/haaddon/telegram-bot/internal/logger"
)
```

### Package Declarations
- Each `.go` file must have **exactly ONE** `package` declaration at the top
- Package name matches directory name (e.g., `internal/config/` → `package config`)

### Naming Conventions
- **Exported** (public): PascalCase - `NewClient`, `GetState`, `TelegramToken`
- **Unexported** (private): camelCase - `setAuthHeader`, `getDomain`, `isPaused`
- **Constants**: PascalCase or SCREAMING_SNAKE_CASE for enums - `IconPowerOn`, `LevelDebug`
- **Interfaces**: Name with -er suffix when possible - `Logger`, `Watcher`

### Error Handling
Always wrap errors with context using `fmt.Errorf` and `%w`:
```go
// Good
return fmt.Errorf("failed to connect to HA: %w", err)
return fmt.Errorf("failed to parse time '%s': %w", entity.State, parseErr)

// Bad
return err
return fmt.Errorf("error: %v", err)
```

Check errors immediately after function calls:
```go
// Good
entity, err := c.haClient.GetState(ctx, entityID)
if err != nil {
    return fmt.Errorf("failed to get state: %w", err)
}

// Bad
entity, _ := c.haClient.GetState(ctx, entityID)
```

### Types and Structs
- Use struct tags for JSON marshaling: `` `json:"entity_id"` ``
- Define structs near their usage or in dedicated types file
- Initialize structs with explicit field names:
```go
// Good
client := &Client{
    baseURL: url,
    token: token,
    httpClient: &http.Client{
        Timeout: 30 * time.Second,
    },
}

// Acceptable for small structs
return &Service{bot, cfg, haClient, loc}
```

### Context Usage
- Always pass `context.Context` as the first parameter
- Use `context.Background()` for top-level operations
- Use `context.WithCancel()` for graceful shutdown
- Check `ctx.Done()` in long-running operations

### Configuration
- All config via environment variables parsed in `internal/config/config.go`
- Inside HA add-on container: use `http://supervisor/core/api` as HA API URL
- `SUPERVISOR_TOKEN` is auto-injected by HA when `homeassistant_api: true`
- Chat IDs parsed from comma/space-separated strings or JSON arrays
- **Security**: Bot commands disabled by default (empty `allowed_chat_ids`)

### Home Assistant Integration
- **REST Client** (`client.go`): Entity state queries, service calls
- **WebSocket Client** (`websocket.go`): Real-time state change subscriptions with auto-reconnect
- For Supervisor API, WebSocket path is `/core/websocket`, not `/api/websocket`
- Always use Bearer token authentication: `Authorization: Bearer <token>`

### Logging
Use the `internal/logger` package with appropriate levels:
```go
logger.Debug("Config loaded: HA URL=%s, Polling=%ds", cfg.HAApiURL, cfg.PollingInterval)
logger.Info("Successfully connected to Home Assistant")
logger.Warn("Failed to get next off time: %v", err)
logger.Error("Bot error: %v", err)
logger.Fatal("Failed to load config: %v", err) // Exits program
```

### Testing Patterns
- Tests use standard Go testing with `httptest` for mocking HA API
- Set env vars before test runs:
```go
os.Setenv("TELEGRAM_TOKEN", "test_token")
defer os.Unsetenv("TELEGRAM_TOKEN")
```
- Use table-driven tests for multiple scenarios
- Name test cases descriptively

## Common Issues & Solutions

### Duplicate package declarations
Each `.go` file must have exactly ONE `package` declaration. Check for:
- Multiple package lines
- Package declaration in comments

### go.sum mismatch
```bash
rm go.sum
go mod tidy
```

### WebSocket auth fails
- Check if using Supervisor URL (`http://supervisor/core/api`) vs external URL
- Different auth paths for different environments
- Verify token is valid and has API access

### Add-on not appearing in HA
1. Check logs: **Settings** → **System** → **Logs**
2. Restart Supervisor: **Settings** → **System** → **Restart Supervisor**
3. Verify `config.yaml` syntax is valid

## Home Assistant Add-on Specifics

### File Structure
- `config.yaml`: Add-on metadata, options schema, version
- `rootfs/etc/services.d/blackout-notify/run`: s6-overlay entry point using bashio
- `Dockerfile`: Multi-stage build (golang:1.21-alpine → HA base image)

### Updating Add-on
When making changes:
1. Bump `version` in `blackout-notify/config.yaml`
2. Update `blackout-notify/CHANGELOG.md`
3. Run pre-deployment checklist (see below)
4. Test locally before pushing

### Pre-deployment Checklist
- [ ] Tests pass: `go test ./...`
- [ ] Docker image builds: `./scripts/docker-build.sh amd64`
- [ ] `config.yaml` has correct version
- [ ] `CHANGELOG.md` updated
- [ ] Telegram token works (bot responds to `/start`)
- [ ] HA token has API access
- [ ] `allowed_chat_ids` configured (security!)

## Documentation Structure

- `README.md` - Complete project documentation (overview, installation, development, troubleshooting)
- `blackout-notify/DOCS.md` - User-facing add-on documentation (shown in HA UI)
- `blackout-notify/CHANGELOG.md` - Version history
- `.github/copilot-instructions.md` - AI assistant guidelines
- `AGENTS.md` - This file (agent-specific development guidelines)
