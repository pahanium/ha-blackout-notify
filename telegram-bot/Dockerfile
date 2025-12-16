# Multi-stage build для Go add-on
# Stage 1: Build Go binary
ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.18
FROM golang:1.21-alpine AS builder

# Встановлюємо необхідні пакети для збірки
RUN apk add --no-cache git ca-certificates tzdata

# Робоча директорія
WORKDIR /app

# Копіюємо go mod файли для кешування залежностей
COPY src/go.mod src/go.sum ./

# Завантажуємо залежності
RUN go mod download

# Копіюємо весь код
COPY src/ ./

# Збираємо бінарник
# CGO_ENABLED=0 для статичної збірки
# -ldflags для зменшення розміру
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /telegram-bot ./cmd/bot

# Stage 2: Final image
FROM ${BUILD_FROM}

# Мітки для Home Assistant
LABEL \
    io.hass.name="Telegram Bot" \
    io.hass.description="Telegram bot for Home Assistant" \
    io.hass.type="addon" \
    io.hass.version="0.1.0"

# Встановлюємо ca-certificates для HTTPS запитів
RUN apk add --no-cache ca-certificates tzdata

# Копіюємо бінарник з builder stage
COPY --from=builder /telegram-bot /app/telegram-bot

# Копіюємо скрипт запуску
COPY rootfs /

# Права на виконання
RUN chmod a+x /app/telegram-bot /run.sh

# Entry point
CMD ["/run.sh"]
