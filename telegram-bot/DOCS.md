# Telegram Bot Add-on Documentation

## Опис

Цей add-on дозволяє керувати Home Assistant через Telegram бота та отримувати сповіщення.

## Конфігурація

### telegram_token (обов'язково)
Токен вашого Telegram бота. Отримайте його у [@BotFather](https://t.me/BotFather).

### allowed_chat_ids (рекомендовано)
Список chat ID, яким дозволено використовувати бота. Якщо порожній - бот доступний всім (небезпечно!).

Щоб дізнатися свій chat ID:
1. Напишіть боту [@userinfobot](https://t.me/userinfobot)
2. Він поверне ваш ID

### log_level
Рівень логування: `debug`, `info`, `warn`, `error`. За замовчуванням: `info`.

### polling_interval
Інтервал оновлення стану entities (в секундах). За замовчуванням: 30.

## Команди бота

| Команда | Опис |
|---------|------|
| `/start` | Привітання та список команд |
| `/status` | Статус Home Assistant |
| `/entities` | Список доступних entities |
| `/state <entity_id>` | Стан конкретної entity |
| `/turn_on <entity_id>` | Увімкнути entity |
| `/turn_off <entity_id>` | Вимкнути entity |

## Приклади використання

```
/state light.living_room
/turn_on switch.bedroom_fan
/entities sensor
```

## Безпека

⚠️ **Важливо**: Завжди вказуйте `allowed_chat_ids` для обмеження доступу до бота!
