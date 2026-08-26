package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

const maxLogEntries = 1000

// JSONStorage implements StorageProvider using atomic JSON file serialization.
type JSONStorage struct {
	mu       sync.RWMutex
	baseDir  string
	usersDir string
}

// NewJSONStorage initializes a JSON storage provider at the specified base directory.
func NewJSONStorage(baseDir string) (*JSONStorage, error) {
	usersDir := filepath.Join(baseDir, "users")
	if err := os.MkdirAll(usersDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create storage directories: %w", err)
	}

	js := &JSONStorage{
		baseDir:  baseDir,
		usersDir: usersDir,
	}

	if err := js.ensureDefaults(); err != nil {
		return nil, err
	}

	return js, nil
}

func (js *JSONStorage) groupsPath() string {
	return filepath.Join(js.baseDir, "groups.json")
}

func (js *JSONStorage) tracksPath() string {
	return filepath.Join(js.baseDir, "tracks.json")
}

func (js *JSONStorage) logsPath() string {
	return filepath.Join(js.baseDir, "logs.json")
}

func (js *JSONStorage) userPath(id uuid.UUID) string {
	return filepath.Join(js.usersDir, id.String()+".json")
}

func (js *JSONStorage) ensureDefaults() error {
	// 1. Ensure groups.json
	if _, err := os.Stat(js.groupsPath()); os.IsNotExist(err) {
		defaultGroup := model.NewGroup("default", 0)
		defaultGroup.DisplayName = "&7Member"
		defaultGroup.AddNode(model.NewNode("gate.command.server", true))
		defaultGroup.AddNode(model.NewNode("gate.command.glist", true))
		defaultGroup.AddNode(model.NewNode("gate.command.ping", true))
		defaultGroup.AddPrefix(model.PriorityMeta{Priority: 10, Value: "&7[Member] "})

		adminGroup := model.NewGroup("admin", 100)
		adminGroup.DisplayName = "&cAdmin"
		adminGroup.AddParent(model.NewInheritance("default"))
		adminGroup.AddNode(model.NewNode("*", true))
		adminGroup.AddNode(model.NewNode("stonyperms.*", true))
		adminGroup.AddPrefix(model.PriorityMeta{Priority: 100, Value: "&c[Admin] "})

		groupsMap := map[string]*model.Group{
			"default": defaultGroup,
			"admin":   adminGroup,
		}
		if err := js.atomicWriteJSON(js.groupsPath(), groupsMap); err != nil {
			return err
		}
	}

	// 2. Ensure tracks.json
	if _, err := os.Stat(js.tracksPath()); os.IsNotExist(err) {
		ranksTrack := model.NewTrack("ranks")
		ranksTrack.Append("default")
		ranksTrack.Append("admin")

		tracksMap := map[string]*model.Track{
			"ranks": ranksTrack,
		}
		if err := js.atomicWriteJSON(js.tracksPath(), tracksMap); err != nil {
			return err
		}
	}

	// 3. Ensure logs.json
	if _, err := os.Stat(js.logsPath()); os.IsNotExist(err) {
		initialLogs := []model.LogEntry{
			model.NewLogEntry("System", "general", "system", "Initialized StonyPerms storage"),
		}
		if err := js.atomicWriteJSON(js.logsPath(), initialLogs); err != nil {
			return err
		}
	}

	return nil
}

// LoadAll loads all entities into memory.
func (js *JSONStorage) LoadAll() (*DataBundle, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	bundle := &DataBundle{
		Users:  make(map[uuid.UUID]*model.User),
		Groups: make(map[string]*model.Group),
		Tracks: make(map[string]*model.Track),
		Logs:   make([]model.LogEntry, 0),
	}

	// 1. Load Groups
	if data, err := os.ReadFile(js.groupsPath()); err == nil {
		var groupsMap map[string]*model.Group
		if err := json.Unmarshal(data, &groupsMap); err == nil {
			for k, g := range groupsMap {
				g.Name = strings.ToLower(k)
				bundle.Groups[g.Name] = g
			}
		}
	}

	// 2. Load Tracks
	if data, err := os.ReadFile(js.tracksPath()); err == nil {
		var tracksMap map[string]*model.Track
		if err := json.Unmarshal(data, &tracksMap); err == nil {
			for k, t := range tracksMap {
				t.Name = strings.ToLower(k)
				bundle.Tracks[t.Name] = t
			}
		}
	}

	// 3. Load Logs
	if data, err := os.ReadFile(js.logsPath()); err == nil {
		_ = json.Unmarshal(data, &bundle.Logs)
	}

	// 4. Load Users from usersDir
	entries, err := os.ReadDir(js.usersDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			filePath := filepath.Join(js.usersDir, entry.Name())
			userData, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			var user model.User
			if err := json.Unmarshal(userData, &user); err == nil && user.UUID != uuid.Nil {
				bundle.Users[user.UUID] = &user
			}
		}
	}

	return bundle, nil
}

// SaveUser saves a user record to disk.
func (js *JSONStorage) SaveUser(user *model.User) error {
	if user == nil || user.UUID == uuid.Nil {
		return fmt.Errorf("invalid user for save")
	}
	js.mu.Lock()
	defer js.mu.Unlock()

	return js.atomicWriteJSON(js.userPath(user.UUID), user)
}

// DeleteUser removes a user record from disk.
func (js *JSONStorage) DeleteUser(id uuid.UUID) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	path := js.userPath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SaveGroup updates the group in groups.json.
func (js *JSONStorage) SaveGroup(group *model.Group) error {
	if group == nil || group.Name == "" {
		return fmt.Errorf("invalid group for save")
	}
	js.mu.Lock()
	defer js.mu.Unlock()

	groupsMap := make(map[string]*model.Group)
	if data, err := os.ReadFile(js.groupsPath()); err == nil {
		_ = json.Unmarshal(data, &groupsMap)
	}

	name := strings.ToLower(group.Name)
	group.Name = name
	groupsMap[name] = group

	return js.atomicWriteJSON(js.groupsPath(), groupsMap)
}

// DeleteGroup removes a group from groups.json.
func (js *JSONStorage) DeleteGroup(name string) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	name = strings.ToLower(strings.TrimSpace(name))
	groupsMap := make(map[string]*model.Group)
	if data, err := os.ReadFile(js.groupsPath()); err == nil {
		_ = json.Unmarshal(data, &groupsMap)
	}

	delete(groupsMap, name)
	return js.atomicWriteJSON(js.groupsPath(), groupsMap)
}

// SaveTrack updates the track in tracks.json.
func (js *JSONStorage) SaveTrack(track *model.Track) error {
	if track == nil || track.Name == "" {
		return fmt.Errorf("invalid track for save")
	}
	js.mu.Lock()
	defer js.mu.Unlock()

	tracksMap := make(map[string]*model.Track)
	if data, err := os.ReadFile(js.tracksPath()); err == nil {
		_ = json.Unmarshal(data, &tracksMap)
	}

	name := strings.ToLower(track.Name)
	track.Name = name
	tracksMap[name] = track

	return js.atomicWriteJSON(js.tracksPath(), tracksMap)
}

// DeleteTrack removes a track from tracks.json.
func (js *JSONStorage) DeleteTrack(name string) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	name = strings.ToLower(strings.TrimSpace(name))
	tracksMap := make(map[string]*model.Track)
	if data, err := os.ReadFile(js.tracksPath()); err == nil {
		_ = json.Unmarshal(data, &tracksMap)
	}

	delete(tracksMap, name)
	return js.atomicWriteJSON(js.tracksPath(), tracksMap)
}

// AppendLog appends a log entry to logs.json.
func (js *JSONStorage) AppendLog(entry model.LogEntry) error {
	js.mu.Lock()
	defer js.mu.Unlock()

	var logs []model.LogEntry
	if data, err := os.ReadFile(js.logsPath()); err == nil {
		_ = json.Unmarshal(data, &logs)
	}

	logs = append(logs, entry)
	if len(logs) > maxLogEntries {
		logs = logs[len(logs)-maxLogEntries:]
	}

	return js.atomicWriteJSON(js.logsPath(), logs)
}

// GetLogs returns audit log entries up to limit.
func (js *JSONStorage) GetLogs(limit int) ([]model.LogEntry, error) {
	js.mu.RLock()
	defer js.mu.RUnlock()

	var logs []model.LogEntry
	if data, err := os.ReadFile(js.logsPath()); err == nil {
		_ = json.Unmarshal(data, &logs)
	}

	if limit > 0 && len(logs) > limit {
		return logs[len(logs)-limit:], nil
	}
	return logs, nil
}

// atomicWriteJSON serializes data to pretty JSON and writes it atomically using a temporary file.
func (js *JSONStorage) atomicWriteJSON(filePath string, data interface{}) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, bytes, 0o644); err != nil {
		return fmt.Errorf("failed to write tmp file: %w", err)
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename atomic file: %w", err)
	}

	return nil
}
