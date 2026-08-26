package manager

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.minekube.com/gate/pkg/util/permission"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

// Manager coordinates in-memory data, persistence, permission evaluation, and promotion tracks.
type Manager struct {
	mu         sync.RWMutex
	storage    storage.StorageProvider
	engine     *engine.Engine
	users      map[uuid.UUID]*model.User
	nameToUUID map[string]uuid.UUID
	groups     map[string]*model.Group
	tracks     map[string]*model.Track
	logs       []model.LogEntry
	stopChan   chan struct{}
}

// New creates and loads a new StonyPerms Manager.
func New(store storage.StorageProvider) (*Manager, error) {
	m := &Manager{
		storage:    store,
		engine:     engine.New(),
		users:      make(map[uuid.UUID]*model.User),
		nameToUUID: make(map[string]uuid.UUID),
		groups:     make(map[string]*model.Group),
		tracks:     make(map[string]*model.Track),
		logs:       make([]model.LogEntry, 0),
		stopChan:   make(chan struct{}),
	}

	if err := m.Reload(); err != nil {
		return nil, err
	}

	// Start background cleanup ticker
	go m.startCleanupLoop()

	return m, nil
}

// Close stops background tasks.
func (m *Manager) Close() {
	select {
	case <-m.stopChan:
	default:
		close(m.stopChan)
	}
}

// Engine returns the underlying calculation engine.
func (m *Manager) Engine() *engine.Engine {
	return m.engine
}

// Reload reloads all data from the storage provider into memory.
func (m *Manager) Reload() error {
	bundle, err := m.storage.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load permissions data: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.users = bundle.Users
	m.groups = bundle.Groups
	m.tracks = bundle.Tracks
	m.logs = bundle.Logs

	m.nameToUUID = make(map[string]uuid.UUID, len(m.users))
	for id, u := range m.users {
		if u.Username != "" {
			m.nameToUUID[strings.ToLower(u.Username)] = id
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// User Operations
// -----------------------------------------------------------------------------

// GetUser retrieves a user by UUID.
func (m *Manager) GetUser(id uuid.UUID) *model.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[id]
}

// GetUserByName looks up a user by username (case-insensitive) or UUID string.
func (m *Manager) GetUserByName(query string) *model.User {
	query = strings.TrimSpace(query)
	if id, err := uuid.Parse(query); err == nil {
		return m.GetUser(id)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if id, ok := m.nameToUUID[strings.ToLower(query)]; ok {
		return m.users[id]
	}
	return nil
}

// GetOrCreateUser retrieves an existing user or creates a new one with the given UUID and name.
func (m *Manager) GetOrCreateUser(id uuid.UUID, username string) *model.User {
	m.mu.Lock()
	defer m.mu.Unlock()

	if u, ok := m.users[id]; ok {
		if username != "" && u.Username != username {
			u.Username = username
			m.nameToUUID[strings.ToLower(username)] = id
			_ = m.storage.SaveUser(u)
		}
		return u
	}

	u := model.NewUser(id, username)
	m.users[id] = u
	if username != "" {
		m.nameToUUID[strings.ToLower(username)] = id
	}
	_ = m.storage.SaveUser(u)
	return u
}

// SaveUser saves the user state to storage and updates in-memory indexes.
func (m *Manager) SaveUser(user *model.User) error {
	m.mu.Lock()
	m.users[user.UUID] = user
	if user.Username != "" {
		m.nameToUUID[strings.ToLower(user.Username)] = user.UUID
	}
	m.mu.Unlock()

	return m.storage.SaveUser(user)
}

// DeleteUser deletes a user from memory and storage.
func (m *Manager) DeleteUser(id uuid.UUID) error {
	m.mu.Lock()
	if u, ok := m.users[id]; ok {
		delete(m.nameToUUID, strings.ToLower(u.Username))
		delete(m.users, id)
	}
	m.mu.Unlock()

	return m.storage.DeleteUser(id)
}

// -----------------------------------------------------------------------------
// Group Operations
// -----------------------------------------------------------------------------

// GetGroup retrieves a group by name.
func (m *Manager) GetGroup(name string) *model.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groups[strings.ToLower(strings.TrimSpace(name))]
}

// GetGroups returns all groups.
func (m *Manager) GetGroups() []*model.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*model.Group, 0, len(m.groups))
	for _, g := range m.groups {
		res = append(res, g)
	}
	return res
}

// CreateGroup creates a new group, saves it, and adds it to cache.
func (m *Manager) CreateGroup(name string, weight int, displayName string) (*model.Group, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.groups[name]; exists {
		return nil, fmt.Errorf("group '%s' already exists", name)
	}

	g := model.NewGroup(name, weight)
	g.DisplayName = displayName
	m.groups[name] = g

	if err := m.storage.SaveGroup(g); err != nil {
		return nil, err
	}
	return g, nil
}

// SaveGroup persists a group to storage.
func (m *Manager) SaveGroup(group *model.Group) error {
	m.mu.Lock()
	m.groups[strings.ToLower(group.Name)] = group
	m.mu.Unlock()

	return m.storage.SaveGroup(group)
}

// DeleteGroup removes a group from cache and storage.
func (m *Manager) DeleteGroup(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "default" {
		return fmt.Errorf("cannot delete the default group")
	}

	m.mu.Lock()
	delete(m.groups, name)
	m.mu.Unlock()

	return m.storage.DeleteGroup(name)
}

// GetGroupMembers returns all users that have this group as primary group or direct parent.
func (m *Manager) GetGroupMembers(groupName string) []*model.User {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	m.mu.RLock()
	defer m.mu.RUnlock()

	var members []*model.User
	for _, u := range m.users {
		if strings.EqualFold(u.PrimaryGroup, groupName) {
			members = append(members, u)
			continue
		}
		for _, p := range u.Parents {
			if strings.EqualFold(p.Group, groupName) {
				members = append(members, u)
				break
			}
		}
	}
	return members
}

// -----------------------------------------------------------------------------
// Track Operations
// -----------------------------------------------------------------------------

// GetTrack retrieves a track by name.
func (m *Manager) GetTrack(name string) *model.Track {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tracks[strings.ToLower(strings.TrimSpace(name))]
}

// GetTracks returns all tracks.
func (m *Manager) GetTracks() []*model.Track {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*model.Track, 0, len(m.tracks))
	for _, t := range m.tracks {
		res = append(res, t)
	}
	return res
}

// CreateTrack creates a new track.
func (m *Manager) CreateTrack(name string) (*model.Track, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("track name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tracks[name]; exists {
		return nil, fmt.Errorf("track '%s' already exists", name)
	}

	t := model.NewTrack(name)
	m.tracks[name] = t

	if err := m.storage.SaveTrack(t); err != nil {
		return nil, err
	}
	return t, nil
}

// SaveTrack persists a track.
func (m *Manager) SaveTrack(track *model.Track) error {
	m.mu.Lock()
	m.tracks[strings.ToLower(track.Name)] = track
	m.mu.Unlock()

	return m.storage.SaveTrack(track)
}

// DeleteTrack removes a track.
func (m *Manager) DeleteTrack(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	m.mu.Lock()
	delete(m.tracks, name)
	m.mu.Unlock()

	return m.storage.DeleteTrack(name)
}

// PromoteUser promotes a user along a track.
func (m *Manager) PromoteUser(user *model.User, trackName string, actor string) (string, string, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return "", "", fmt.Errorf("track '%s' does not exist", trackName)
	}
	if len(track.Groups) == 0 {
		return "", "", fmt.Errorf("track '%s' contains no groups", trackName)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Find user's current highest group on this track
	currentGroup := ""
	for _, gName := range track.Groups {
		// Check primary group
		if strings.EqualFold(user.PrimaryGroup, gName) {
			currentGroup = gName
			continue
		}
		// Check parents
		for _, p := range user.Parents {
			if strings.EqualFold(p.Group, gName) {
				currentGroup = gName
				break
			}
		}
	}

	nextGroup, ok := track.NextGroup(currentGroup)
	if !ok {
		if currentGroup != "" {
			return currentGroup, "", fmt.Errorf("user is already at the highest group on track '%s'", trackName)
		}
		nextGroup = track.Groups[0]
	}

	// Apply promotion: remove old track groups, set new group
	if currentGroup != "" {
		user.RemoveParent(currentGroup, nil)
	}
	user.AddParent(model.NewInheritance(nextGroup))
	user.PrimaryGroup = nextGroup

	_ = m.storage.SaveUser(user)
	m.logActionLocked(actor, "user", user.Username, fmt.Sprintf("Promoted along track '%s' (%s -> %s)", trackName, currentGroup, nextGroup))

	return currentGroup, nextGroup, nil
}

// DemoteUser demotes a user along a track.
func (m *Manager) DemoteUser(user *model.User, trackName string, actor string) (string, string, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return "", "", fmt.Errorf("track '%s' does not exist", trackName)
	}
	if len(track.Groups) == 0 {
		return "", "", fmt.Errorf("track '%s' contains no groups", trackName)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	currentGroup := ""
	for _, gName := range track.Groups {
		if strings.EqualFold(user.PrimaryGroup, gName) {
			currentGroup = gName
			continue
		}
		for _, p := range user.Parents {
			if strings.EqualFold(p.Group, gName) {
				currentGroup = gName
				break
			}
		}
	}

	if currentGroup == "" {
		return "", "", fmt.Errorf("user is not on track '%s'", trackName)
	}

	prevGroup, ok := track.PreviousGroup(currentGroup)
	if !ok {
		return currentGroup, "", fmt.Errorf("user is already at the lowest group on track '%s'", trackName)
	}

	user.RemoveParent(currentGroup, nil)
	user.AddParent(model.NewInheritance(prevGroup))
	user.PrimaryGroup = prevGroup

	_ = m.storage.SaveUser(user)
	m.logActionLocked(actor, "user", user.Username, fmt.Sprintf("Demoted along track '%s' (%s -> %s)", trackName, currentGroup, prevGroup))

	return currentGroup, prevGroup, nil
}

// -----------------------------------------------------------------------------
// Permission Resolution & Gate Integration
// -----------------------------------------------------------------------------

// HasPermission checks if a user has a given permission node.
func (m *Manager) HasPermission(user *model.User, perm string, contexts map[string]string) bool {
	m.mu.RLock()
	groupsCopy := make(map[string]*model.Group, len(m.groups))
	for k, v := range m.groups {
		groupsCopy[k] = v
	}
	m.mu.RUnlock()

	tri := m.engine.PermissionValue(user, groupsCopy, perm, contexts)
	return tri == model.True
}

// PermissionValue evaluates the full Tristate for a user.
func (m *Manager) PermissionValue(user *model.User, perm string, contexts map[string]string) model.Tristate {
	m.mu.RLock()
	groupsCopy := make(map[string]*model.Group, len(m.groups))
	for k, v := range m.groups {
		groupsCopy[k] = v
	}
	m.mu.RUnlock()

	return m.engine.PermissionValue(user, groupsCopy, perm, contexts)
}

// GatePermissionFunc creates a Gate permission.Func for a user by UUID.
func (m *Manager) GatePermissionFunc(userID uuid.UUID, contextProvider func() map[string]string) permission.Func {
	return func(perm string) permission.TriState {
		var ctxs map[string]string
		if contextProvider != nil {
			ctxs = contextProvider()
		}

		user := m.GetUser(userID)
		if user == nil {
			return permission.Undefined
		}

		res := m.PermissionValue(user, perm, ctxs)
		switch res {
		case model.True:
			return permission.True
		case model.False:
			return permission.False
		default:
			return permission.Undefined
		}
	}
}

// GetPrefix gets the active prefix for a user.
func (m *Manager) GetPrefix(user *model.User, contexts map[string]string) string {
	m.mu.RLock()
	groupsCopy := make(map[string]*model.Group, len(m.groups))
	for k, v := range m.groups {
		groupsCopy[k] = v
	}
	m.mu.RUnlock()

	return m.engine.GetPrefix(user, groupsCopy, contexts)
}

// GetSuffix gets the active suffix for a user.
func (m *Manager) GetSuffix(user *model.User, contexts map[string]string) string {
	m.mu.RLock()
	groupsCopy := make(map[string]*model.Group, len(m.groups))
	for k, v := range m.groups {
		groupsCopy[k] = v
	}
	m.mu.RUnlock()

	return m.engine.GetSuffix(user, groupsCopy, contexts)
}

// GetMeta gets a metadata key for a user.
func (m *Manager) GetMeta(user *model.User, key string) (string, bool) {
	m.mu.RLock()
	groupsCopy := make(map[string]*model.Group, len(m.groups))
	for k, v := range m.groups {
		groupsCopy[k] = v
	}
	m.mu.RUnlock()

	return m.engine.GetMeta(user, groupsCopy, key)
}

// -----------------------------------------------------------------------------
// Audit Logging
// -----------------------------------------------------------------------------

// LogAction records an administrative action in the audit history.
func (m *Manager) LogAction(actor, targetType, target, action string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logActionLocked(actor, targetType, target, action)
}

func (m *Manager) logActionLocked(actor, targetType, target, action string) {
	entry := model.NewLogEntry(actor, targetType, target, action)
	m.logs = append(m.logs, entry)
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-1000:]
	}
	_ = m.storage.AppendLog(entry)
}

// GetLogs returns audit action logs up to limit.
func (m *Manager) GetLogs(limit int) []model.LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit > 0 && len(m.logs) > limit {
		return m.logs[len(m.logs)-limit:]
	}
	return m.logs
}

// Stats returns high-level diagnostic statistics.
func (m *Manager) Stats() (usersCount, groupsCount, tracksCount, logsCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users), len(m.groups), len(m.tracks), len(m.logs)
}

// -----------------------------------------------------------------------------
// Background Ticker
// -----------------------------------------------------------------------------

func (m *Manager) startCleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case now := <-ticker.C:
			m.cleanupExpired(now)
		}
	}
}

func (m *Manager) cleanupExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, u := range m.users {
		if cleaned := u.CleanExpired(now); cleaned > 0 {
			_ = m.storage.SaveUser(u)
		}
	}
	for _, g := range m.groups {
		if cleaned := g.CleanExpired(now); cleaned > 0 {
			_ = m.storage.SaveGroup(g)
		}
	}
}
