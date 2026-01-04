# Changelog

All notable changes to this project will be documented in this file.

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
