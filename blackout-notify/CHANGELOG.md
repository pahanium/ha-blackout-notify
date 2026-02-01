# Changelog

All notable changes to this project will be documented in this file.

## [0.4.3-beta] - 2026-02-01

### Fixed
- 🐛 **Duration tracking in notifications** - fixed bug where duration was not shown in power notifications
  - Changed `RecordStateChange` to return previous state and duration
  - Watcher now correctly retrieves duration before recording new state
  - Duration was missing because `GetLastEventDuration` returned the newly recorded event instead of previous
- 🧹 **Removed extra empty line** after power status message (was "Світло повернулось!\n\nСвітла не було X")
- ⏰ **Fixed "невідомо" for next-day schedule times** - improved time parsing to correctly handle schedule times after midnight
  - Added detection of time-only formats (15:04, 15:04:05)
  - Correctly converts parsed times to local timezone
  - Better handling of times that cross midnight boundary

### Technical
- `RecordStateChange()` now returns `(previousState string, previousDuration int64, err error)`
- Updated all tests to handle new return signature
- Improved `getScheduledTime()` logic to track which format was matched
- Added edge case handling in `formatDuration()` for zero-minute durations

## [0.4.2-beta] - 2026-02-01

### Changed
- 📊 **Statistics now always enabled** - removed `stats_enabled` option, statistics database is always initialized
- 💬 **Message improvements**:
  - All Ukrainian messages moved to `messages.go` constants for better maintainability
  - Power notifications now show duration of previous state (e.g., "Світла не було 2год 20хв")
  - Duration displayed only if > 60 seconds to avoid clutter
  - Duration rounded to nearest minute for better readability
  - Icons moved directly into message constants (removed separate icon constants)
  - Compact duration format without spaces: "6год 25хв" instead of "6 год 25 хв"

### Technical
- Added `GetLastEventDuration()` method to stats Recorder
- Updated `NotifyPowerOn/Off` signatures to accept duration parameter
- Watcher now retrieves and passes previous state duration to notifications
- Removed `StatsEnabled` config field - statistics always initialized
- Simplified main.go initialization logic (always create statsDB/statsRecorder)

## [0.4.0] - 2026-02-01

### Added
- **Power duration statistics**: SQLite-based tracking of power on/off durations
  - Persistent storage in `/data/power_stats.db` (survives add-on restarts and updates)
  - Automatic duration calculation for each state change
  - `/stats` command to view statistics (today/week/month) with Ukrainian localization
  - Query methods for daily, weekly, and monthly aggregations
  - Support for current ongoing state duration display
- **Statistics configuration**: `stats_enabled` option (default: true)
- **Data persistence**: Added `data:rw` volume mapping for statistics storage
- **Comprehensive test coverage**: Unit tests for all stats functionality using in-memory SQLite
- Pure Go SQLite driver (`modernc.org/sqlite`) - no CGO dependencies required

### Changed
- Watcher now optionally records state changes to statistics database
- Bot can display power statistics via `/stats` command
- Updated `/help` command to include statistics usage
- Enhanced shutdown process to properly close database connections

### Technical
- New package: `internal/stats` with `db.go`, `recorder.go`, `query.go`
- Database schema with `power_events` table and optimized indexes
- Thread-safe state recording with proper timestamp handling
- Support for both RFC3339 and standard SQLite timestamp formats

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
