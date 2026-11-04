package bot

// Access control messages
const (
	MsgCommandsDisabled = "⛔ Команди бота вимкнені. Щоб увімкнути, додайте свій chat ID в `allowed_chat_ids`."
	MsgAccessDenied     = "⛔ Доступ заборонено: вашого chat ID немає в списку дозволених."
)

// General command messages
const (
	MsgNotACommand    = "Надішліть команду. Список команд: /help"
	MsgUnknownCommand = "Невідома команда: /%s\nСписок команд: /help"
	MsgChatID         = "Ваш chat ID: `%d`"
	MsgError          = "❌ Помилка: %s"

	MsgStart = `🏠 *Вітаю!*

Я надсилаю сповіщення про відключення та відновлення світла, показую статистику і графік відключень.

Список команд: /help`

	MsgHelp = `📋 *Команди:*

/status - стан підключення до Home Assistant
/stats [today|week|month] - статистика світла за сьогодні, тиждень або місяць
/calendar [tomorrow] - графік відключень Yasno на сьогодні або завтра
/chatid - ваш chat ID

*Приклади:*
` + "`/stats week`" + `
` + "`/calendar tomorrow`"
)

// /status messages
const (
	MsgStatusOK          = "✅ *Home Assistant*\n\n🔗 Підключено"
	MsgStatusUnreachable = "❌ *Home Assistant*\n\n🔗 Немає підключення, деталі в журналі add-on'а"
)

// /stats messages
const (
	MsgStatsUnavailable = "❌ Статистика недоступна: не налаштовано `watched_entity_id`."
	MsgStatsUnknownArg  = "❌ Невідомий період: %s\n\nВикористовуйте: `today`, `week`, `month`"
	MsgStatsTitle       = "📊 *Статистика: %s*\n\n"
	MsgStatsToday       = "сьогодні"
	MsgStatsWeek        = "за тиждень"
	MsgStatsMonth       = "за місяць"
	MsgStatsOn          = "⚡ *Світло було:* %s\n"
	MsgStatsOnNoData    = "⚡ *Світло було:* немає даних\n"
	MsgStatsOff         = "🔌 *Світла не було:* %s\n"
	MsgStatsOffNoData   = "🔌 *Світла не було:* немає даних\n"
	MsgStatsAverage     = "   Середня тривалість: %s (%d разів)\n"
	MsgStatsCurrentOn   = "Світло є"
	MsgStatsCurrentOff  = "Світла немає"
	MsgStatsUsage       = "\n_Використання:_\n`/stats today` - за сьогодні\n`/stats week` - за тиждень\n`/stats month` - за місяць"
)

// /calendar messages
const (
	MsgCalendarUnavailable   = "❌ Графік недоступний: сервіс сповіщень не запущено."
	MsgCalendarNotConfigured = "❌ Графік недоступний: не налаштовано `yasno_calendar_id`."
	MsgCalendarUnknownArg    = "❌ Невідомий аргумент. Використовуйте:\n`/calendar` - графік на сьогодні\n`/calendar tomorrow` - графік на завтра"
)
