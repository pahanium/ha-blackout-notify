#!/usr/bin/with-contenv bashio
# shellcheck shell=bash
# ==============================================================================
# Home Assistant Add-on: Telegram Bot
# Runs the Telegram bot
# ==============================================================================

# Читаємо конфігурацію з Home Assistant
export TELEGRAM_TOKEN=$(bashio::config 'telegram_token')
export ALLOWED_CHAT_IDS=$(bashio::config 'allowed_chat_ids')
export LOG_LEVEL=$(bashio::config 'log_level')
export POLLING_INTERVAL=$(bashio::config 'polling_interval')

# Home Assistant API URL та токен
# SUPERVISOR_TOKEN автоматично доступний в add-on контейнері
export HA_API_URL="http://supervisor/core/api"
export HA_TOKEN="${SUPERVISOR_TOKEN}"

# Логуємо старт
bashio::log.info "Starting Telegram Bot..."
bashio::log.info "Log level: ${LOG_LEVEL}"

# Перевіряємо чи встановлений токен
if bashio::var.is_empty "${TELEGRAM_TOKEN}"; then
    bashio::log.fatal "Telegram token is not configured!"
    bashio::exit.nok
fi

# Запускаємо бота
exec /app/telegram-bot
