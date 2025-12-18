# Blackout Notify - Home Assistant Add-on

🔌 Telegram bot for monitoring power grid status in Home Assistant with automatic notifications about power outages and restorations.

## Features

- ⚡ **Real-time monitoring** - WebSocket-based power state tracking
- 📱 **Telegram notifications** - Instant alerts about power changes
- 📅 **Schedule information** - Shows next planned power on/off times
- ⏸️ **Notification pause** - Temporarily disable alerts via HA input_boolean
- 🔒 **Security** - Chat ID whitelisting for access control

## Quick Start

### Installation

**Method 1: GitHub Repository (Recommended)**

1. In Home Assistant, go to **Settings** → **Add-ons** → **Add-on Store**
2. Click menu (⋮) → **Repositories**
3. Add: `https://github.com/pahanium/ha-blackout-notify`
4. Find "Blackout Notify" in the store and click **Install**

**Method 2: Local Development**

```bash
# Copy repository to HA server
scp -r ha-blackout-notify/ user@ha-server:/addons/

# Or create symbolic link on HA server
cd /addons
ln -s /path/to/ha-blackout-notify .
```

Then in HA: **Settings** → **Add-ons** → menu (⋮) → **Check for updates**

### Configuration

```yaml
telegram_token: "YOUR_BOT_TOKEN"           # Required: Get from @BotFather
allowed_chat_ids: "123456789,987654321"    # Optional: Leave empty to disable bot commands
notification_chat_ids: "-1001234567890"    # Chat/channel for notifications
watched_entity_id: "binary_sensor.power"   # Entity to monitor
next_on_sensor_id: "sensor.next_power_on"  # Optional: Next power on time
next_off_sensor_id: "sensor.next_power_off" # Optional: Next power off time
log_level: "info"                          # debug/info/warn/error
```

**Note:** If you only need power notifications without bot commands, leave `allowed_chat_ids` empty.

### Getting Tokens

**Telegram Bot Token:**
1. Message [@BotFather](https://t.me/BotFather)
2. Send `/newbot` command
3. Follow instructions and copy the token

**Your Chat ID:**
1. Message [@userinfobot](https://t.me/userinfobot)
2. Copy the ID it returns

**Home Assistant Token:**
1. Go to **Profile** (bottom left in HA)
2. Scroll to **Long-Lived Access Tokens**
3. Click **Create Token** and copy it

## Bot Commands

**Note:** Bot commands are only available when `allowed_chat_ids` is configured. If left empty, only notifications will work.

| Command | Description |
|---------|-------------|
| `/start` | Welcome message and command list |
| `/status` | Home Assistant status |
| `/entities` | List available entities |
| `/state <entity_id>` | Get entity state |
| `/turn_on <entity_id>` | Turn on entity |
| `/turn_off <entity_id>` | Turn off entity |
| `/chatid` | Show your chat ID |

## Notification Examples

**Power restored:**
```
💡 *Світло повернулось!*

📅 Відключення через 2 год 15 хв (16:45)
за даними Yasno
```

**Power outage:**
```
🔌 *Світло вимкнено*

📅 Заживлення через 3 год 30 хв (18:00)
за даними Yasno
```

**Schedule changed:**
```
🔄 *Графік оновлено*

📅 Заживлення через 2 год 30 хв (18:00)
за даними Yasno
```

## Local Development

### Option 1: Direct Go Build

```bash
cd blackout-notify/src

# Install dependencies
go mod tidy

# Run tests
go test -v ./...

# Build
go build -o ../bin/blackout-notify ./cmd/bot

# Run with environment variables
export TELEGRAM_TOKEN="your_token"
export HA_API_URL="http://192.168.1.100:8123/api"
export HA_TOKEN="your_ha_token"
export LOG_LEVEL="debug"

../bin/blackout-notify
```

### Option 2: Docker Compose (Recommended)

```bash
# Create environment file
cp .env.example .env
nano .env  # Fill in your values

# Start
docker compose -f docker-compose.dev.yaml up --build

# Stop
docker compose -f docker-compose.dev.yaml down
```

### Option 3: Local Docker Build

```bash
# Build for amd64
./scripts/docker-build.sh amd64

# Or for ARM (Raspberry Pi)
./scripts/docker-build.sh aarch64

# Run
docker run --rm \
  -e TELEGRAM_TOKEN="xxx" \
  -e HA_API_URL="http://192.168.1.100:8123/api" \
  -e HA_TOKEN="xxx" \
  local/blackout-notify:latest
```

## Home Assistant Configuration

Create input_boolean for notification pause control:

```yaml
# configuration.yaml
input_boolean:
  pause_power_notifications:
    name: "Pause power notifications"
    icon: mdi:bell-off
```

See `ha-config-examples.yaml` for complete examples.

## Troubleshooting

### Add-on doesn't appear
- Check **Settings** → **System** → **Logs**
- Restart Supervisor: **Settings** → **System** → **Restart Supervisor**

### Bot doesn't respond
```bash
# Check logs in HA UI or via SSH:
docker logs addon_local_blackout_notify
```

### HA API connection error
- Inside add-on: use `http://supervisor/core/api`
- For local dev: use external address like `http://192.168.1.100:8123/api`
- Verify token is valid

### Unauthorized in Telegram
- Check `allowed_chat_ids` in configuration
- Use `/chatid` command to find your ID

## Development

### Testing

```bash
cd blackout-notify/src

# Run all tests
go test ./...

# With verbose output
go test -v ./...

# With race detector and coverage
go test -v -race -coverprofile=coverage.txt ./...
```

### Building

```bash
# Build optimized binary
cd blackout-notify/src
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot

# Build Docker image for specific architecture
cd ..
./scripts/docker-build.sh amd64    # or aarch64, armv7
```

### Pre-deployment Checklist

- [ ] Telegram token works (bot responds to `/start`)
- [ ] HA token has API access
- [ ] `allowed_chat_ids` configured (security!)
- [ ] Tests pass: `go test ./...`
- [ ] Docker image builds without errors
- [ ] `config.yaml` has correct version
- [ ] `CHANGELOG.md` updated

## Project Structure

```
ha-blackout-notify/
├── .github/
│   ├── copilot-instructions.md   # AI assistant guidelines
│   └── workflows/                # CI/CD pipelines
├── blackout-notify/              # Add-on directory
│   ├── config.yaml               # HA add-on configuration
│   ├── Dockerfile                # Multi-stage build
│   ├── DOCS.md                   # User documentation
│   ├── CHANGELOG.md              # Version history
│   ├── rootfs/
│   │   └── run.sh                # Entry point (bashio)
│   └── src/                      # Go source code
│       ├── cmd/bot/main.go       # Application entry
│       └── internal/
│           ├── bot/              # Telegram bot logic
│           ├── config/           # Configuration
│           ├── homeassistant/    # HA API (REST + WebSocket)
│           ├── notifications/    # Notification service
│           ├── watcher/          # Power state monitoring
│           └── logger/           # Logging
├── scripts/
│   ├── build.sh                  # Go build script
│   └── docker-build.sh           # Docker build script
├── docker-compose.dev.yaml       # Local development
└── .env.example                  # Environment template
```

## CI/CD

GitHub Actions workflow (`.github/workflows/build.yaml`) automatically:
- Runs tests on every push/PR
- Lints code with golangci-lint
- Builds Docker images for multiple architectures (amd64, aarch64, armv7)
- Pushes images to GHCR on tag releases

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes and add tests
4. Run `go test ./...` to verify
5. Update `CHANGELOG.md`
6. Bump version in `config.yaml`
7. Create a pull request

## License

MIT License - see LICENSE file for details

## Support

- 🐛 [Issues](https://github.com/pahanium/ha-blackout-notify/issues)
- 💬 [Discussions](https://github.com/pahanium/ha-blackout-notify/discussions)