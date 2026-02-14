# Changelog

All notable changes to this project will be documented in this file.

## [0.4.8] - 2026-02-13

### Fixed
- 🐛 **Calendar API query parameter**: Added `?return_response=true` to calendar service calls
  - Fixed error: "Service call requires responses but caller did not ask for responses"
  - Calendar events now properly retrieved for Yasno schedule notifications
  - Affects tomorrow schedule announcements with planned outage times

## [0.4.7] - 2026-02-08

### Fixed
- 🐛 **Yasno sensor state reading**: Fixed incorrect attribute lookup for Yasno schedule sensors
  - Yasno sensors store schedule state in `sensor.state` (enum type), not in `attributes`
  - Changed `handleYasnoTodayChange()` to read from `newState.State` directly
  - Changed `handleYasnoTomorrowChange()` to read from `newState.State` directly
  - Removed unused `GetYasnoScheduleState()` method that was looking in wrong place
  - Fixes error: "attribute tomorrow_schedule_state not found or not a string"
  - Sensor state enum values: `schedule_applies`, `waiting_for_schedule`, `emergency_shutdowns`, `unknown`

## [0.4.6] - 2026-02-07

### Added
- 📅 **Yasno schedule monitoring and notifications**: Integration with Yasno power grid schedule sensors
  - Monitor `sensor.yasno_*_status_today` for emergency shutdown detection
  - Monitor `sensor.yasno_*_status_tomorrow` for daily schedule announcements
  - Automatic calendar event fetching via `calendar.get_events` service
  - Notifications show outage intervals in format: "🪫00:00 - 04:00"
  - Support for group name extraction from sensor attributes (e.g., "група 2.1")
  - Configurable via add-on UI: `yasno_today_sensor_id`, `yasno_tomorrow_sensor_id`, `yasno_calendar_id`
  - Messages in Ukrainian with date formatting: "завтра сб, 07.02"
  - Filter events with `description="Definite"` to show only confirmed outages

### Changed
- 🔕 **Quiet hours support**: Schedule notifications sent between 23:00-06:00 disable sound
  - Applies to Yasno tomorrow schedule announcements
  - User still receives notification but without disturbing sound/vibration
  - Regular power on/off notifications not affected
- ⏱️ **Enhanced debouncing**: Separate 30-second debounce for Yasno sensors
  - Prevents notification spam when sensors update frequently
  - Power monitoring keeps 5-second debounce for faster response

### Technical
- New package: `internal/homeassistant/calendar.go` with Calendar API client
  - `GetCalendarEvents()` method for fetching calendar events
  - Support for Home Assistant `calendar.get_events` service via POST
  - Response parsing for nested JSON structure
- New notification methods in `internal/notifications/service.go`:
  - `NotifyYasnoScheduleTomorrow()` - tomorrow schedule announcement
  - `NotifyYasnoEmergencyShutdown()` - emergency shutdown alert
  - `NotifyYasnoScheduleRestored()` - schedule restoration (currently logged only)
  - `GetYasnoScheduleState()` - extract state from entity attributes
  - `GetYasnoGroupName()` - parse group name from attributes
  - `GetYasnoCalendarEvents()` - wrapper for calendar API
  - `sendToAllChatsWithQuietHours()` - send with conditional notification sound
  - `formatYasnoDate()` - Ukrainian date formatting with day names
- Enhanced `internal/watcher/watcher.go`:
  - `registerYasnoHandlers()` - WebSocket handler registration
  - `handleYasnoTodayChange()` - today status change processing
  - `handleYasnoTomorrowChange()` - tomorrow status change processing
  - Separate state tracking for Yasno sensors
- Extended configuration in `internal/config/config.go`:
  - `YasnoTodaySensorID`, `YasnoTomorrowSensorID`, `YasnoCalendarID` fields
  - `IsYasnoMonitoringEnabled()` method
  - Environment variable parsing: `YASNO_TODAY_SENSOR_ID`, etc.
- Updated add-on configuration schema in `config.yaml`
- Updated startup script to pass Yasno environment variables
- Comprehensive test coverage:
  - `calendar_test.go` - Calendar API client tests with httptest
  - `config_test.go` - Added `TestIsYasnoMonitoringEnabled()`
  - All tests passing with new functionality

### Messages
- `MsgYasnoScheduleTomorrow` - "📅 *Графік відключень на %s%s*"
- `MsgYasnoNoOutagesTomorrow` - "📅 *Графік на %s%s*\n\n✅ Відключень не повинно бути 🌞"
- `MsgYasnoEmergencyShutdown` - "🚨 *Аварійні відключення!*"
- `MsgYasnoScheduleRestored` - "✅ *Графік відновлено*"
- `MsgYasnoOutageInterval` - "🪫%s - %s"

## [0.4.5] - 2026-02-04

### Fixed
- 🐛 **Schedule change notifications**: Fixed incorrect suppression logic when schedule is cancelled then updated
  - When schedule becomes `nil` (unavailable), system now preserves the last known scheduled time
  - This prevents losing track of when the planned event should have occurred
  - Fixes issue where notification suppression failed after schedule cancellation
  - Example scenario: Schedule at 18:30 → cancelled at 18:38 → new schedule at 19:08
    - Before: Lost 18:30 reference, sent notification for 06:00 (next day)
    - After: Keeps 18:30 reference, correctly suppresses notification (less than 60 min passed)
  - No longer sends confusing "schedule changed to nil" notifications to users

### Technical
- Added early return in `handleScheduleChange()` when `newTime == nil`
- Schedule cancellations no longer update `lastNextOnTime` / `lastNextOffTime` variables
- Added test `TestHandleScheduleChange_NilScheduleIgnored` to verify suppression logic works correctly
- Improved debug logging for schedule cancellations

## [0.4.4] - 2026-02-03

### Added
- 📊 **Power duration statistics tracking**: SQLite-based statistics for power on/off durations
  - Persistent storage in `/data/power_stats.db` (survives add-on restarts and updates)
  - Automatic duration calculation for each state change
  - `/stats` command to view statistics (today/week/month) with Ukrainian localization
  - Query methods for daily, weekly, and monthly aggregations
  - Support for current ongoing state duration display
  - Statistics database always enabled (no configuration needed)
  - Pure Go SQLite driver (`modernc.org/sqlite`) - no CGO dependencies required
  - Comprehensive test coverage for all stats functionality

### Changed
- 💬 **Enhanced power notifications with duration display**:
  - Power notifications now show duration of previous state (e.g., "Світла не було 2год 20хв")
  - Duration displayed only if > 60 seconds and < 24 hours to avoid clutter
  - Duration rounded to nearest minute for better readability
  - Compact duration format without spaces: "6год 25хв" instead of "6 год 25 хв"
  - 🕐 Duration icon changed to clock with white background for better visual clarity
  - All Ukrainian messages moved to `messages.go` constants for better maintainability
  - Icons moved directly into message constants
- ⚙️ **Bot enhancements**:
  - Updated `/help` command to include statistics usage
  - Bot can display power statistics via `/stats` command with Ukrainian localization
  - Enhanced shutdown process to properly close database connections
- 🐳 **Infrastructure updates**:
  - Updated Go base image to 1.24-alpine
  - Added `data:rw` volume mapping for statistics storage

### Fixed
- 🐛 **Duration tracking in notifications**: Fixed bug where duration was not shown in power notifications
  - Changed `RecordStateChange` to return previous state and duration
  - Watcher now correctly retrieves duration before recording new state
  - Duration was missing because `GetLastEventDuration` returned the newly recorded event instead of previous
- 🧹 **Message formatting**: Removed extra empty line after power status message
- ⏰ **Schedule time parsing**: Fixed "невідомо" for next-day schedule times
  - Added detection of time-only formats (15:04, 15:04:05)
  - Correctly converts parsed times to local timezone
  - Better handling of times that cross midnight boundary

### Technical
- New package: `internal/stats` with `db.go`, `recorder.go`, `query.go`
- Database schema with `power_events` table and optimized indexes
- Thread-safe state recording with proper timestamp handling
- Support for both RFC3339 and standard SQLite timestamp formats
- `RecordStateChange()` now returns `(previousState string, previousDuration int64, err error)`
- Updated all tests to handle new return signature
- Added `GetLastEventDuration()` method to stats Recorder
- Updated `NotifyPowerOn/Off` signatures to accept duration parameter
- Watcher now retrieves and passes previous state duration to notifications
- Simplified main.go initialization logic (always create statsDB/statsRecorder)
- Improved `getScheduledTime()` logic to track which format was matched
- Added edge case handling in `formatDuration()` for zero-minute durations
- Updated AGENTS.md with latest project structure and Go 1.24+ requirement

## [0.3.1] - 2026-01-04

### Fixed
- **Power state monitoring improvements**: Added logic to skip notifications when state transitions from `unknown` to `on` or `off`
  - Prevents false notifications on add-on startup or Home Assistant restart
  - Only logs state changes without sending Telegram messages for initial state detection
  - Real state changes (`on` → `off` or `off` → `on`) still trigger notifications as expected

### Added
- Comprehensive test coverage for watcher module with 11 test cases covering all state transition scenarios
- Tests for `normalizeState()`, `timesEqual()`, `formatTimePtr()` and state change handling
- Test coverage includes: unknown→on/off, on↔off transitions, debouncing, and multi-step scenarios

## [0.3.0] - 2024-12-18

### Security
- **Bot commands now disabled by default** - `allowed_chat_ids` must be explicitly configured to enable bot commands
- Empty `allowed_chat_ids` now denies all access instead of allowing everyone (breaking change for security)
- Added `IsBotCommandsEnabled()` check to prevent unauthorized command execution

### Changed
- Bot commands are only started if `allowed_chat_ids` is not empty
- Improved unauthorized access messages: different messages for disabled vs unauthorized
- Updated documentation to reflect secure-by-default behavior
- Consolidated documentation: removed CLAUDE.md and DEVELOPMENT.md, merged into README.md

### Added
- Created `.github/copilot-instructions.md` for AI assistant guidance
- Added tests for `IsBotCommandsEnabled()` function

### Documentation
- Translated DOCS.md to English (except Ukrainian notification examples)
- Updated README.md with all development and deployment information
- Clarified that bot commands require `allowed_chat_ids` configuration

## [0.2.5] - 2024-12-18

### Changed
- Updated notification messages: shorter and clearer
- Power on: "Світло є!" / Power off: "Світла немає"
- "Заживлення через..." / "Відключення через..." instead of verbose descriptions
- Added "_за даними Yasno_" footer in italics
- If next on/off time is unknown - show only the main message without schedule

### Fixed
- Suppress unnecessary schedule change notifications when old scheduled time has already passed

## [0.1.0] - 2024-XX-XX

### Added
- Initial release
- Basic Telegram bot functionality
- Home Assistant API integration
- Entity state queries
- Basic commands: /start, /status, /entities
