package storage

import (
	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

// DataBundle holds all loaded permissions data in memory.
type DataBundle struct {
	Users  map[uuid.UUID]*model.User
	Groups map[string]*model.Group
	Tracks map[string]*model.Track
	Logs   []model.LogEntry
}

// StorageProvider abstracts the persistence layer for StonyPerms.
type StorageProvider interface {
	// LoadAll loads all users, groups, tracks, and logs from persistent storage.
	LoadAll() (*DataBundle, error)

	// SaveUser saves or updates a single user record.
	SaveUser(user *model.User) error

	// DeleteUser removes a user record by UUID.
	DeleteUser(id uuid.UUID) error

	// SaveGroup saves or updates a single group record.
	SaveGroup(group *model.Group) error

	// DeleteGroup removes a group record by name.
	DeleteGroup(name string) error

	// SaveTrack saves or updates a single track record.
	SaveTrack(track *model.Track) error

	// DeleteTrack removes a track record by name.
	DeleteTrack(name string) error

	// AppendLog appends an audit action log entry.
	AppendLog(entry model.LogEntry) error

	// GetLogs returns the most recent audit log entries up to limit.
	GetLogs(limit int) ([]model.LogEntry, error)
}
