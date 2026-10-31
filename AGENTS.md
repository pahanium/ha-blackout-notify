# Agent Guidelines for Blackout Notify

Home Assistant add-on in **Go 1.24** that watches power state through the HA WebSocket API and sends Telegram notifications about outages, restorations and Yasno outage schedules. Packaged as a multi-arch (amd64/aarch64/armv7) s6-overlay Docker image.

## Commands

All Go commands run from `blackout-notify/src/`:

```bash
go test ./...                                          # all tests
go test -v -run TestNormalizeState ./internal/watcher  # single test
go test -v -race -coverprofile=coverage.txt ./...      # what CI runs
go fmt ./... && go vet ./...                           # required before commits; CI also runs golangci-lint
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot
```

From repo root: `./scripts/build.sh` (tidy + test + build), `./scripts/docker-build.sh amd64|aarch64|armv7`.

### Running locally

- `main.go` loads `../../.env` relative to the **current directory**, so run the binary from `src/` (`../bin/blackout-notify`) with `.env` in the repo root (template: `.env.example`).
- Local `.env` must use `HA_API_URL=http://<ha-ip>:8123/api` (not the supervisor URL) and `STATS_DB_PATH=./power_stats.db`.
- Alternative: `docker compose -f docker-compose.dev.yaml up --build` (uses `Dockerfile.dev`, no s6/bashio).

## Architecture

```
cmd/bot/main.go          → wiring, signal handling
internal/config/         → env-var config
internal/homeassistant/  → REST client, calendar.get_events, WebSocket client with auto-reconnect
internal/watcher/        → reacts to HA state changes (power + schedule + Yasno sensors)
internal/notifications/  → message formatting and sending; all Ukrainian strings in messages.go
internal/stats/          → SQLite power_events table, /stats queries
internal/bot/            → Telegram command handlers
internal/logger/         → leveled logger (Debug/Info/Warn/Error/Fatal)
```

- **Config path**: add-on options in `blackout-notify/config.yaml` → exported as env vars by `rootfs/etc/services.d/blackout-notify/run` (bashio) → `config.Load()`. A new option must be added to `config.yaml` (both `options` and `schema`), the `run` script, `internal/config/config.go`, and `.env.example`.
- **Conditional wiring in `main.go`**:
  - Telegram command polling only if `allowed_chat_ids` is non-empty (`IsBotCommandsEnabled`); empty = notifications only, by design for security.
  - `notifications.Service` if power monitoring **or** `yasno_calendar_id` is set (`/calendar` needs it).
  - Watcher, WebSocket client and stats DB only if `watched_entity_id` is set (`IsPowerMonitoringEnabled`).
  - The bot receives optional deps via setters (`SetStatsProvider`, `SetNotificationService`), so they can be nil in handlers.
- **Event flow**: `watcher.Start` registers per-entity handlers on `WSClient.OnStateChange`, then blocks in `RunWithReconnect`.
  - Power entity: normalized to `on`/`off`/`unknown`, 5s debounce. Each change is recorded via `stats.Recorder.RecordStateChange` (returns previous state duration) and then notified. Transitions from `unknown` are recorded but not notified, so startup is silent.
  - Next on/off sensors: "schedule changed" notifications.
  - Yasno today/tomorrow status sensors (30s debounce): emergency-shutdown alert; tomorrow's schedule fetched from the HA calendar via `calendar.get_events?return_response=true`.
- **Notifications**: the pause `input_boolean` (`pause_entity_id`) suppresses every notification type. Tomorrow's schedule is sent silently during quiet hours (23:00–06:00, configured timezone).
- **Stats DB**: `modernc.org/sqlite` (pure Go, keeps `CGO_ENABLED=0`); `/data/power_stats.db` in the add-on, persists across updates.

## HA specifics

- Inside the add-on: `HA_API_URL=http://supervisor/core/api`, token from `SUPERVISOR_TOKEN` (needs `homeassistant_api: true`). WebSocket path there is `/core/websocket`, not `/api/websocket`; `WSClient.getWSURL` handles both.
- Auth is always `Authorization: Bearer <token>`.

## Conventions

- Code, comments, docs and commits in English; user-facing Telegram messages in Ukrainian (`internal/notifications/messages.go`).
- Module path is the placeholder `github.com/yourusername/haaddon/telegram-bot`; keep it in imports. Import groups: stdlib, external, internal.
- Wrap errors with context: `fmt.Errorf("failed to get state: %w", err)`; don't discard errors with `_`.
- Log through `internal/logger`, not `log`/`fmt.Print`.
- Tests: standard `testing`, table-driven, `httptest` servers to mock HA; set env vars with `os.Setenv` + `defer os.Unsetenv`.

## Branches and releases

- `beta` publishes as "Blackout Notify (Beta)" with slug `blackout_notify_beta`; `main` as "Blackout Notify" with slug `blackout_notify`. When syncing branches, keep each branch's `name`/`slug`/`description` in `config.yaml` and `repository.yaml`.
- Every release: bump `version` in `blackout-notify/config.yaml`, add an entry to `blackout-notify/CHANGELOG.md`, make sure tests, `go vet` and the Docker build pass.
- CI (`.github/workflows/build.yaml`) runs on push/PR to `main` and `beta`: tests, golangci-lint, image build for all arches; pushes to GHCR only on `v*` tags.

## Docs

- `README.md`: project overview, install, development.
- `blackout-notify/DOCS.md`: user docs shown in the HA add-on UI; update when options change.
