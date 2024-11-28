package log

import (
	"fmt"
	"strings"
)

// A Level is a logging priority. Higher levels are more important.
type Level int

const (
	// LevelDebug logs are typically voluminous, and are usually disabled in
	// production.
	LevelDebug Level = iota
	// LevelInfo is the default logging priority.
	LevelInfo
	// LevelWarn logs are more important than Info, but don't need individual
	// human review.
	LevelWarn
	// LevelError logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	LevelError
	// LevelPanic logs a message, then panics.
	LevelPanic
	// LevelFatal logs a message, then calls os.Exit(1).
	LevelFatal
)

var levelMap = map[string]Level{
	"debug": LevelDebug,
	"info":  LevelInfo,
	"warn":  LevelWarn,
	"error": LevelError,
	"panic": LevelPanic,
	"fatal": LevelFatal,
}

// UnmarshalText Unmarshal the text.
func (lvl *Level) UnmarshalText(text []byte) error {
	level, ok := levelMap[strings.ToLower(string(text))]
	if !ok {
		return fmt.Errorf("not support log level: %v", string(text))
	}
	*lvl = level
	return nil
}
