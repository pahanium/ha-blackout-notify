# Agent Guidelines for Blackout Notify

Home Assistant add-on in **Go 1.26** that watches power state through the HA WebSocket API and sends Telegram notifications about outages, restorations and Yasno outage schedules. Packaged as a multi-arch (amd64/aarch64/armv7) s6-overlay Docker image.

## Commands

Use the `Makefile` from the repo root (`make help` lists targets):

```bash
make test        # go test -race ./...
make lint        # gofmt check, go vet, golangci-lint v2 (same version/config as CI: src/.golangci.yml)
make check       # test + lint + govulncheck
make run         # go run ./cmd/bot with .env
make dev-up      # run in Docker via docker-compose.dev.yaml (Dockerfile.dev, no s6/bashio)
make docker ARCH=aarch64   # production add-on image via scripts/docker-build.sh (base image from build.yaml)
```

Single test (from `blackout-notify/src/`): `go test -v -run TestNormalizeState ./internal/watcher`.

### Local development and testing

- `.env` lives in the repo root (template `.env.example`); `main.go` loads the nearest `.env` from the current directory upwards and never overrides already-set env vars.
- `.env` uses a **separate dev/beta Telegram bot and channel**, not production. Two instances polling the same bot token get `409 Conflict`. Running locally while the beta add-on is up therefore needs its own token too.
- Local runs talk to the **live Home Assistant** (`HA_API_URL=http://<ha-ip>:8123/api`, long-lived token). This is intended: the bot only reads states, subscribes to events and calls `calendar.get_events`; it has no commands that change HA. Keep it read-only.
- `STATS_DB_PATH=./power_stats.db` for `make run`; `make dev-up` stores it in `.data/` (git-ignored).

## Architecture

```
cmd/bot/main.go          → wiring, signal handling
internal/config/         → env-var config
internal/homeassistant/  → REST client, calendar.get_events, WebSocket client with auto-reconnect
internal/watcher/        → reacts to HA state changes (power + schedule + Yasno sensors)
internal/notifications/  → message formatting and sending; all Ukrainian strings in messages.go
internal/stats/          → SQLite power_events table, /stats queries
internal/bot/            → Telegram command handlers (read-only: /status, /stats, /calendar, /chatid)
internal/logger/         → leveled logger (Debug/Info/Warn/Error/Fatal)
```

- **Config path**: add-on options in `blackout-notify/config.yaml` → exported as env vars by `rootfs/etc/services.d/blackout-notify/run` (bashio) → `config.Load()`. A new option must be added to `config.yaml` (both `options` and `schema`), the `run` script, `internal/config/config.go`, and `.env.example`.
- **Conditional wiring in `main.go`**:
  - Telegram command polling only if `allowed_chat_ids` is non-empty (`IsBotCommandsEnabled`); empty = notifications only, by design for security.
  - `notifications.Service` if power monitoring **or** `yasno_calendar_id` is set (`/calendar` needs it).
  - Watcher, WebSocket client and stats DB only if `watched_entity_id` is set (`IsPowerMonitoringEnabled`).
  - The bot receives optional deps via setters (`SetStatsProvider`, `SetNotificationService`), so they can be nil in handlers.
- **Event flow**: `watcher.Start` registers per-entity handlers on `WSClient.OnStateChange`, then blocks in `RunWithReconnect`.
  - Power entity: normalized to `on`/`off`/`unknown`, 5s debounce. Each change is recorded via `stats.Recorder.RecordStateChange` (returns previous state duration) and then notified. Transitions to or from `unknown` (startup, WebSocket reconnect) are neither recorded nor notified, so reconnects don't break outage durations.
  - Next on/off sensors: "schedule changed" notifications.
  - Yasno today/tomorrow status sensors (30s debounce): emergency-shutdown alert; tomorrow's schedule fetched from the HA calendar via `calendar.get_events?return_response=true`.
- **Notifications**: the pause `input_boolean` (`pause_entity_id`) suppresses every notification type. Tomorrow's schedule is sent silently during quiet hours (23:00–06:00, configured timezone).
- **Stats DB**: `modernc.org/sqlite` (pure Go, keeps `CGO_ENABLED=0`); `/data/power_stats.db` in the add-on, persists across updates.

## HA specifics

- Inside the add-on: `HA_API_URL=http://supervisor/core/api`, token from `SUPERVISOR_TOKEN` (needs `homeassistant_api: true`). WebSocket path there is `/core/websocket`, not `/api/websocket`; `WSClient.getWSURL` handles both.
- Auth is always `Authorization: Bearer <token>`.
- `rootfs/etc/services.d/blackout-notify/finish` halts the add-on with the service's exit code on failure (HA template), so Supervisor sees crashes.
- Removing an option from `config.yaml` is safe: Supervisor drops unknown saved options with a warning.

## Conventions

- Code, comments, docs and commits in English; user-facing Telegram messages in Ukrainian (`internal/notifications/messages.go`).
- Module path: `github.com/pahanium/ha-blackout-notify`. Import groups: stdlib, external, internal.
- Wrap errors with context: `fmt.Errorf("failed to get state: %w", err)`; don't discard errors with `_`.
- Log through `internal/logger`, not `log`/`fmt.Print`.
- Tests: standard `testing`, table-driven, `httptest` servers to mock HA; set env vars with `t.Setenv`.

## Branches and releases

- `beta` publishes as "Blackout Notify (Beta)" with slug `blackout_notify_beta`; `main` as "Blackout Notify" with slug `blackout_notify`. When syncing branches, keep each branch's `name`/`slug`/`description` in `config.yaml` and `repository.yaml`.
- Every release: bump `version` in `blackout-notify/config.yaml`, add an entry to `blackout-notify/CHANGELOG.md`, run `make check` and `make docker`.
- CI (`.github/workflows/build.yaml`) runs on push/PR to `main` and `beta`: tests, golangci-lint, image build for all arches; pushes to GHCR only on `v*` tags.

## Docs

- `README.md`: project overview, install, development.
- `blackout-notify/DOCS.md`: user docs shown in the HA add-on UI; update when options change.
