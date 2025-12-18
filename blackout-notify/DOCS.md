# Blackout Notify Add-on Documentation

## Description

This add-on allows you to control Home Assistant via Telegram bot and receive automatic notifications about power status changes.

## Features

- 🤖 **Control via commands** - Manage Home Assistant devices through Telegram
- ⚡ **Power monitoring** - Automatic notifications when power goes on/off
- 📅 **Schedule information** - Shows next scheduled power on/off times
- ⏸️ **Pause notifications** - Temporarily disable alerts via Home Assistant

## Configuration

### Basic Settings

#### telegram_token (required)

Your Telegram bot token. Get it from [@BotFather](https://t.me/BotFather).

#### allowed_chat_ids (optional)

List of chat IDs allowed to use bot commands. **If empty - bot commands are disabled** (only notifications will work).

To find your chat ID:
1. Message [@userinfobot](https://t.me/userinfobot)
2. It will return your ID

**Security:** Leave empty if you only need power notifications without bot control commands.

#### log_level

Logging level: `debug`, `info`, `warn`, `error`. Default: `info`.

#### polling_interval

Entity state update interval (in seconds). Default: 30.

### Power Monitoring Settings

#### notification_chat_ids

List of chat IDs (channels or groups) for power notifications. Can be different from `allowed_chat_ids`.

For channels: use channel ID (starts with `-100`)

#### watched_entity_id

Entity ID of the power sensor to monitor.

Example: `binary_sensor.power_status`

#### next_on_sensor_id

Entity ID of sensor with next power on time.

Example: `sensor.next_power_on`

#### next_off_sensor_id

Entity ID of sensor with next power off time.

Example: `sensor.next_power_off`

#### pause_entity_id

Entity ID of `input_boolean` for temporary notification pause.

Default: `input_boolean.pause_power_notifications`

#### timezone

Timezone for time formatting.

Default: `Europe/Kyiv`

## Bot Commands

**Note:** Bot commands require `allowed_chat_ids` to be configured. If left empty, commands are disabled and only notifications work.

| Command | Description |
|---------|-------------|
| `/start` | Welcome message and command list |
| `/status` | Home Assistant status |
| `/entities` | List available entities |
| `/state <entity_id>` | Get entity state |
| `/turn_on <entity_id>` | Turn on entity |
| `/turn_off <entity_id>` | Turn off entity |
| `/chatid` | Show your chat ID |

## Notification Format

### Power restored
```
💡 *Світло повернулось!*

📅 Відключення через 2 год 15 хв (16:45)
за даними Yasno
```

### Power lost
```
🔌 *Світло вимкнено*

📅 Заживлення через 3 год 30 хв (18:00)
за даними Yasno
```

### Schedule changed
```
🔄 *Графік оновлено*

📅 Заживлення через 2 год 30 хв (18:00)
за даними Yasno
```

## Home Assistant Configuration

To enable power monitoring, create an `input_boolean` for notification pause:

```yaml
# configuration.yaml
input_boolean:
  pause_power_notifications:
    name: "Pause power notifications"
    icon: mdi:bell-off
```

See `ha-config-examples.yaml` for detailed configuration examples.

## Command Usage Examples

```
/state light.living_room
/turn_on switch.bedroom_fan
/entities sensor
```

## Security

⚠️ **Important**: 
- Bot commands are **disabled by default** (safe)
- Set `allowed_chat_ids` only if you need bot commands
- For public channels, use separate `notification_chat_ids`
- Only listed chat IDs can control Home Assistant

## Environment Variables

If you run the bot outside Home Assistant Add-on, use these variables:

```env
TELEGRAM_TOKEN=your_bot_token
HA_API_URL=http://homeassistant.local:8123/api
HA_TOKEN=your_long_lived_access_token
ALLOWED_CHAT_IDS=123456789
NOTIFICATION_CHAT_IDS=-1001234567890
WATCHED_ENTITY_ID=binary_sensor.power_status
NEXT_ON_SENSOR_ID=sensor.next_power_on
NEXT_OFF_SENSOR_ID=sensor.next_power_off
PAUSE_ENTITY_ID=input_boolean.pause_power_notifications
TIMEZONE=Europe/Kyiv
LOG_LEVEL=info
```
