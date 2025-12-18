# Blackout Notify - Home Assistant Add-on

[![GitHub Release](https://img.shields.io/github/v/release/pahanium/ha-blackout-notify?style=flat-square)](https://github.com/pahanium/ha-blackout-notify/releases)
[![License](https://img.shields.io/github/license/pahanium/ha-blackout-notify?style=flat-square)](LICENSE)

A Home Assistant add-on that monitors power grid status and sends notifications to Telegram when power outages occur or power is restored.

## Features

- ⚡ **Real-time power monitoring** - Instantly detects power outages and restoration
- 📱 **Telegram notifications** - Sends alerts to your Telegram bot/channel/group
- 📅 **Schedule tracking** - Shows next scheduled power on/off times
- 🔄 **Schedule change alerts** - Notifies when power schedule is updated
- ⏸️ **Pause notifications** - Temporarily disable alerts via Home Assistant UI
- 🤖 **Bot commands** - Control Home Assistant devices via Telegram

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

## Installation

1. Open Home Assistant
2. Go to **Settings** → **Add-ons** → **Add-on Store**
3. Click menu (⋮) → **Repositories**
4. Add: `https://github.com/pahanium/ha-blackout-notify`
5. Find "Blackout Notify" and click **Install**
6. Configure the add-on (see Configuration below)
7. Start the add-on

## Configuration

### Required Settings

| Option | Description |
|--------|-------------|
| `telegram_token` | Your Telegram bot token from [@BotFather](https://t.me/BotFather) |
| `allowed_chat_ids` | List of chat IDs allowed to use bot commands |

### Power Monitoring Settings

| Option | Description |
|--------|-------------|
| `notification_chat_ids` | Chat IDs for power notifications (can be channels) |
| `watched_entity_id` | Entity ID of power sensor (e.g., `binary_sensor.power`) |
| `next_on_sensor_id` | Entity ID of sensor with next power on time |
| `next_off_sensor_id` | Entity ID of sensor with next power off time |
| `pause_entity_id` | Entity ID of input_boolean to pause notifications |
| `timezone` | Timezone for time formatting (default: `Europe/Kyiv`) |

### Example Configuration

```yaml
telegram_token: "your_bot_token_from_botfather"
allowed_chat_ids:
  - 123456789
notification_chat_ids:
  - -1001234567890
watched_entity_id: "binary_sensor.power_status"
next_on_sensor_id: "sensor.next_power_on"
next_off_sensor_id: "sensor.next_power_off"
pause_entity_id: "input_boolean.pause_power_notifications"
timezone: "Europe/Kyiv"
log_level: "info"
```

## Home Assistant Setup

Create an `input_boolean` to control notification pause:

```yaml
# configuration.yaml
input_boolean:
  pause_power_notifications:
    name: "Pause power notifications"
    icon: mdi:bell-off
```

## Bot Commands

| Command | Description |
|---------|-------------|
| `/start` | Welcome message and command list |
| `/status` | Home Assistant connection status |
| `/state <entity_id>` | Get entity state |
| `/turn_on <entity_id>` | Turn on entity |
| `/turn_off <entity_id>` | Turn off entity |
| `/chatid` | Show your chat ID |

## License

MIT License - see [LICENSE](LICENSE) for details.
