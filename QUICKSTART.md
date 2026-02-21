# Quick Start Guide - Local Development

## Prerequisites
- Go 1.24+ installed
- Home Assistant running and accessible
- Telegram Bot Token from @BotFather

## Steps

### 1. Clone and Setup
```bash
cd /path/to/haaddon
cp .env.example .env
```

### 2. Configure .env
Edit `.env` file and set:
```bash
TELEGRAM_TOKEN=your_bot_token_from_botfather
HA_API_URL=http://192.168.X.X:8123/api  # Your HA IP
HA_TOKEN=your_ha_long_lived_token
STATS_DB_PATH=./power_stats.db  # Local path
WATCHED_ENTITY_ID=binary_sensor.power  # Your power sensor
# ... other settings
```

### 3. Build and Run
```bash
cd blackout-notify/src
go build -o ../bin/blackout-notify ./cmd/bot
../bin/blackout-notify
```

Or use the helper script:
```bash
./scripts/run-local.sh
```

## What Happens
1. Bot loads `.env` from repository root automatically
2. Connects to Home Assistant
3. Starts monitoring your power sensor
4. Sends Telegram notifications on power changes

## Troubleshooting

### Error: "dial tcp: lookup supervisor"
- You're using `http://supervisor/core/api` - change to your HA IP in `.env`
- Example: `HA_API_URL=http://192.168.9.99:8123/api`

### Error: ".env not found"
- Make sure `.env` is in repository root (not in `src/`)
- Run bot from `blackout-notify/src` directory

### Error: "Failed to connect to Home Assistant"
- Check HA_API_URL is correct and accessible
- Verify HA_TOKEN is valid (create in HA Profile → Long-Lived Access Tokens)

## Testing
```bash
cd blackout-notify/src
go test ./...
```

## Development
```bash
# Format code
go fmt ./...

# Check for issues
go vet ./...

# Run with debug logging
# Edit .env: LOG_LEVEL=debug
```
