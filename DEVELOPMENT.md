# Development and Deployment Guide

## Project Structure

```
ha-blackout-notify/
├── repository.yaml          # Add-on repository metadata
├── README.md                 # Repository description
├── CLAUDE.md                 # AI assistant guidelines
├── docker-compose.dev.yaml   # Local development
├── .env.example              # Environment variables template
├── scripts/
│   ├── build.sh              # Go build without Docker
│   └── docker-build.sh       # Docker image build
└── blackout-notify/          # Add-on directory
    ├── config.yaml           # HA add-on configuration
    ├── Dockerfile            # Multi-stage build
    ├── DOCS.md               # User documentation
    ├── CHANGELOG.md          # Version history
    ├── rootfs/
    │   └── run.sh            # Entry point (with bashio)
    └── src/                  # Go source code
        ├── go.mod
        ├── go.sum
        ├── cmd/bot/main.go
        └── internal/
            ├── bot/
            ├── config/
            ├── homeassistant/
            ├── notifications/
            ├── watcher/
            └── logger/
```

---

## 🧪 Local Testing

### Option 1: Test Go Code Directly

Fastest approach for development:

```bash
# 1. Install Go 1.21+
# 2. Navigate to source directory
cd blackout-notify/src

# 3. Download dependencies
go mod tidy

# 4. Run tests
go test -v ./...

# 5. Build binary
go build -o ../bin/blackout-notify ./cmd/bot

# 6. Set environment variables and run
export TELEGRAM_TOKEN="your_telegram_token"
export HA_API_URL="http://your-home-assistant:8123/api"
export HA_TOKEN="your_long_lived_access_token"
export LOG_LEVEL="debug"

../bin/blackout-notify
```

### Option 2: Docker Compose (Recommended)

```bash
# 1. Copy .env.example to .env
cp .env.example .env

# 2. Fill in your values in .env
nano .env

# 3. Start
docker compose -f docker-compose.dev.yaml up --build

# 4. Stop
docker compose -f docker-compose.dev.yaml down
```

### Option 3: Local Docker Image Build

```bash
# Build for amd64
./scripts/docker-build.sh amd64

# Or for ARM (e.g., Raspberry Pi)
./scripts/docker-build.sh aarch64

# Run
docker run --rm \
  -e TELEGRAM_TOKEN="xxx" \
  -e HA_API_URL="http://192.168.1.100:8123/api" \
  -e HA_TOKEN="xxx" \
  local/blackout-notify:latest
```

---

## 🔑 How to Get Tokens

### Telegram Bot Token
1. Message [@BotFather](https://t.me/BotFather) on Telegram
2. Use the `/newbot` command
3. Give your bot a name and username
4. Copy the token

### Home Assistant Long-Lived Access Token
1. Open Home Assistant
2. Go to: **Profile** (bottom left)
3. Scroll to **Long-Lived Access Tokens**
4. Click **Create Token**
5. Give it a name and copy the token

### Chat ID
1. Message [@userinfobot](https://t.me/userinfobot)
2. It will return your Chat ID

---

## 🚀 Deployment to Home Assistant Server

### Method 1: Local Repository (for development)

Simplest approach for testing on a real HA instance:

```bash
# 1. On the Home Assistant server (where Supervisor is running)
# Copy the entire repository to /addons
scp -r ha-blackout-notify/ user@ha-server:/addons/

# Or create a symbolic link
# SSH to HA server:
cd /addons
ln -s /path/to/your/ha-blackout-notify .
```

**In Home Assistant:**
1. Go to: **Settings** → **Add-ons** → **Add-on Store**
2. Click menu (⋮) → **Check for updates**
3. In menu (⋮) → **Repositories** ensure local add-ons are enabled
4. Find "Blackout Notify" in Local add-ons
5. Install and configure

### Method 2: GitHub Repository (Recommended for production)

```bash
# 1. Create a repository on GitHub
git init
git add .
git commit -m "Initial commit"
git remote add origin https://github.com/yourusername/haaddon.git
git push -u origin main
```

**In Home Assistant:**
1. Go to: **Settings** → **Add-ons** → **Add-on Store**
2. Menu (⋮) → **Repositories**
3. Add URL: `https://github.com/yourusername/haaddon`
4. Click **Add**
5. Find and install "Telegram Bot"

### Method 3: Custom Docker Registry (advanced)

For more control over images:

```bash
# 1. Build and push image
docker build -t your-registry.com/telegram-bot:1.0.0 ./telegram-bot
docker push your-registry.com/telegram-bot:1.0.0

# 2. Edit config.yaml
# image: your-registry.com/telegram-bot-{arch}
```

---

## 🔄 CI/CD Pipeline (GitHub Actions)

The project includes `.github/workflows/build.yaml` which:
- Runs tests on every push/PR
- Lints code with golangci-lint
- Builds Docker images for multiple architectures
- Pushes images to GHCR on tag releases

---

## 📋 Pre-deployment Checklist

- [ ] Telegram token works (bot responds to /start)
- [ ] HA token has API access
- [ ] `allowed_chat_ids` configured (security!)
- [ ] Tests pass: `go test ./...`
- [ ] Docker image builds without errors
- [ ] config.yaml has correct version
- [ ] CHANGELOG.md updated

---

## 🐛 Troubleshooting

### Add-on doesn't appear in HA
- Check logs: **Settings** → **System** → **Logs**
- Restart Supervisor: **Settings** → **System** → **Restart Supervisor**

### Bot doesn't respond
```bash
# Check add-on logs in HA UI
# or via SSH:
docker logs addon_local_telegram_bot
```

### HA API connection error
- In add-on, use `http://supervisor/core/api`
- For local development use external address
- Verify token is valid

### Unauthorized in Telegram
- Check `allowed_chat_ids` in settings
- Use /chatid to find your ID
