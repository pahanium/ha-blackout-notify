# Multi-stage build for Go add-on
# Stage 1: Build Go binary
ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.18
FROM golang:1.21-alpine AS builder

# Install required packages for build
RUN apk add --no-cache git ca-certificates tzdata

# Working directory
WORKDIR /app

# Copy go mod files for dependency caching
COPY src/go.mod src/go.sum ./

# Download dependencies
RUN go mod download

# Copy all code
COPY src/ ./

# Build binary
# CGO_ENABLED=0 for static build
# -ldflags to reduce size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /blackout-notify ./cmd/bot

# Stage 2: Final image
FROM ${BUILD_FROM}

# Labels for Home Assistant
LABEL \
    io.hass.name="Blackout Notify" \
    io.hass.description="Power grid monitoring with Telegram notifications" \
    io.hass.type="addon" \
    io.hass.version="0.1.0"

# Install ca-certificates for HTTPS requests
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder stage
COPY --from=builder /blackout-notify /app/blackout-notify

# Copy s6-overlay service files
COPY rootfs /

# Execution permissions
RUN chmod a+x /app/blackout-notify \
    && chmod a+x /etc/services.d/blackout-notify/run \
    && chmod a+x /etc/services.d/blackout-notify/finish
