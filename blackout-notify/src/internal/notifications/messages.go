package notifications

// Power state change messages
const (
	MsgPowerOn  = "💡 *Світло повернулось!*"
	MsgPowerOff = "🔌 *Світло вимкнено*"
)

// Duration messages (format string with %s for duration)
const (
	MsgWasOn  = "Світло було %s"    // "Світло було 6год 25хв"
	MsgWasOff = "Світла не було %s" // "Світла не було 2год 20хв"
)

// Schedule messages (format strings)
const (
	MsgScheduleUpdate = "🔄 *Графік оновлено*"
	MsgScheduleOnIn   = "📅 Заживлення через *%s* (%s)"  // через 4год 30хв (18:30)
	MsgScheduleOffIn  = "📅 Відключення через *%s* (%s)" // через 2год 15хв (14:45)
	MsgScheduleSource = "_за даними Yasno_"
)
