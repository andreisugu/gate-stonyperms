package model

import (
	"time"
)

// LogEntry records an administrative action for the audit history log.
type LogEntry struct {
	Timestamp  int64  `json:"timestamp"`
	Actor      string `json:"actor"`      // Who executed the command
	TargetType string `json:"targetType"` // "user", "group", "track", "general"
	Target     string `json:"target"`     // Username/UUID, Group Name, Track Name
	Action     string `json:"action"`     // The specific command executed
}

// NewLogEntry creates a new audit log entry.
func NewLogEntry(actor, targetType, target, action string) LogEntry {
	return LogEntry{
		Timestamp:  time.Now().Unix(),
		Actor:      actor,
		TargetType: targetType,
		Target:     target,
		Action:     action,
	}
}

// Time returns the time.Time of the log entry.
func (l LogEntry) Time() time.Time {
	return time.Unix(l.Timestamp, 0)
}
