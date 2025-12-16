package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Level represents logging level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var currentLevel = LevelInfo

var (
	debugLogger = log.New(os.Stdout, "[DEBUG] ", log.Ldate|log.Ltime)
	infoLogger  = log.New(os.Stdout, "[INFO]  ", log.Ldate|log.Ltime)
	warnLogger  = log.New(os.Stdout, "[WARN]  ", log.Ldate|log.Ltime)
	errorLogger = log.New(os.Stderr, "[ERROR] ", log.Ldate|log.Ltime)
	fatalLogger = log.New(os.Stderr, "[FATAL] ", log.Ldate|log.Ltime)
)

// SetLevel sets the logging level from string
func SetLevel(level string) {
	switch strings.ToLower(level) {
	case "debug":
		currentLevel = LevelDebug
	case "info":
		currentLevel = LevelInfo
	case "warn", "warning":
		currentLevel = LevelWarn
	case "error":
		currentLevel = LevelError
	default:
		currentLevel = LevelInfo
	}
}

// Debug logs debug message
func Debug(format string, v ...interface{}) {
	if currentLevel <= LevelDebug {
		debugLogger.Printf(format, v...)
	}
}

// Info logs info message
func Info(format string, v ...interface{}) {
	if currentLevel <= LevelInfo {
		infoLogger.Printf(format, v...)
	}
}

// Warn logs warning message
func Warn(format string, v ...interface{}) {
	if currentLevel <= LevelWarn {
		warnLogger.Printf(format, v...)
	}
}

// Error logs error message
func Error(format string, v ...interface{}) {
	if currentLevel <= LevelError {
		errorLogger.Printf(format, v...)
	}
}

// Fatal logs fatal message and exits the program
func Fatal(format string, v ...interface{}) {
	fatalLogger.Printf(format, v...)
	fmt.Println("Exiting due to fatal error")
	os.Exit(1)
}
