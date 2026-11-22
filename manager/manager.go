package manager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.minekube.com/gate/pkg/util/permission"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

var (
	ErrTrackEmpty  = errors.New("track contains no groups")
	ErrTrackTop    = errors.New("user is already at the highest group on track")
	ErrTrackBottom = errors.New("user is already at the lowest group on track")
	ErrNotOnTrack  = errors.New("user is not on track")
)

// Manager coordinates in-memory data, persistence, permission evaluation, and promotion tracks.
type Manager struct {
	mu               sync.RWMutex
	storage          storage.StorageProvider
	engine           *engine.Engine
	users            map[uuid.UUID]*model.User
	nameToUUID       map[string]uuid.UUID
	groups           map[string]*model.Group
	tracks           map[string]*model.Track
	logs             []model.LogEntry
	updateListener   atomic.Pointer[func()]
	changeNotifier   atomic.Pointer[func(EditorChangeNotice)]
	logNotifyMuted   map[string]bool
	subMu            sync.Mutex
	subscribers      map[uint64]*eventSubscriber
	nextSubID        uint64
	subClosed        bool
	stopChan         chan struct{}
	revision         atomic.Uint64
	generation       string
	exportDir        string
	importDir        string
	defaultGroup     string
	auditLogDisabled bool
}

// EffectiveNode is a permission node together with the entity it came from.
// The source order is significant: user nodes always precede group nodes, and
// groups are returned in descending weight order, mirroring engine evaluation.
type EffectiveNode struct {
	Node   model.Node
	Source string // "user" or "group:<name>"
	Weight int
	User   bool
}

// EffectiveGroup is a lightweight, immutable view of a resolved group.
type EffectiveGroup struct {
	Name        string
	DisplayName string
	Weight      int
}

// EffectiveUser is an immutable snapshot of a user's resolved permissions for
// one context. It is safe to use after the manager lock is released.
type EffectiveUser struct {
	UUID         uuid.UUID
	Username     string
	PrimaryGroup string
	Synthetic    bool
	Groups       []EffectiveGroup
	Nodes        []EffectiveNode
	Meta         map[string]string
	Prefix       string
	Suffix       string
}

// GroupMemberInfo summarizes a user's membership in a group.
type GroupMemberInfo struct {
	UUID         uuid.UUID         `json:"uuid"`
	Username     string            `json:"username"`
	PrimaryGroup string            `json:"primaryGroup"`
	Permanent    bool              `json:"permanent"`
	Expiry       int64             `json:"expiry,omitempty"`
	Contexts     map[string]string `json:"contexts,omitempty"`
}

// Revision returns a monotonically increasing counter that changes whenever
// users, groups, or tracks are modified. It is used for cheap cache validation by
// external permission adapters.
func (m *Manager) Revision() uint64 {
	return m.revision.Load()
}

// Generation identifies this manager process. It changes on every proxy
// restart so ETags from a previous process can never be reused for different
// in-memory state.
func (m *Manager) Generation() string {
	return m.generation
}

// SetUpdateListener registers a callback triggered whenever permissions, groups, or users are modified.
func (m *Manager) SetUpdateListener(listener func()) {
	if listener == nil {
		m.updateListener.Store(nil)
	} else {
		m.updateListener.Store(&listener)
	}
}

func (m *Manager) notifyUpdate() {
	ptr := m.updateListener.Load()
	if ptr != nil && *ptr != nil {
		fn := *ptr
		go fn()
	}
}

// bumpRevisionLocked advances the visible-state version. Callers must hold
// m.mu for writing so a snapshot cannot observe changed state with an old
// revision.
func (m *Manager) bumpRevisionLocked() uint64 {
	return m.revision.Add(1)
}

func newGeneration() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// New creates and loads a new StonyPerms Manager.
func New(store storage.StorageProvider) (*Manager, error) {
	m := &Manager{
		storage:      store,
		engine:       engine.New(),
		users:        make(map[uuid.UUID]*model.User),
		nameToUUID:   make(map[string]uuid.UUID),
		groups:       make(map[string]*model.Group),
		tracks:       make(map[string]*model.Track),
		logs:         make([]model.LogEntry, 0),
		subscribers:  make(map[uint64]*eventSubscriber),
		logNotifyMuted: make(map[string]bool),
		stopChan:     make(chan struct{}),
		generation:   newGeneration(),
		exportDir:    deriveExportDir(store),
		importDir:    deriveImportDir(store),
		defaultGroup: "default",
	}
	m.revision.Store(1)

	if err := m.Reload(); err != nil {
		return nil, err
	}

	// Start background cleanup ticker
	go m.startCleanupLoop()

	return m, nil
}

// Close stops background tasks and closes active event subscriptions.
func (m *Manager) Close() {
	select {
	case <-m.stopChan:
	default:
		close(m.stopChan)
	}
	m.closeSubscribers()
	if closer, ok := m.storage.(io.Closer); ok {
		_ = closer.Close()
	}
}

// Engine returns the underlying calculation engine.
func (m *Manager) Engine() *engine.Engine {
	return m.engine
}

// SetDefaultGroup configures the fallback group name assigned to new or default-resolved users.
func (m *Manager) SetDefaultGroup(name string) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" {
		clean = "default"
	}
	m.mu.Lock()
	m.defaultGroup = clean
	m.mu.Unlock()

	st := m.getWebState()
	st.mu.Lock()
	if st.config.DefaultGroup == "" || st.config.DefaultGroup == "default" {
		st.config.DefaultGroup = clean
	}
	st.mu.Unlock()
}

// DefaultGroup returns the configured fallback group name (defaults to "default").
func (m *Manager) DefaultGroup() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.defaultGroup == "" {
		return "default"
	}
	return m.defaultGroup
}

// SetAuditLogEnabled enables or disables recording administrative audit log entries.
func (m *Manager) SetAuditLogEnabled(enabled bool) {
	m.mu.Lock()
	m.auditLogDisabled = !enabled
	m.mu.Unlock()
}

// IsAuditLogEnabled reports whether audit logging is currently enabled.
func (m *Manager) IsAuditLogEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.auditLogDisabled
}

// Reload reloads all data from the storage provider into memory.
func (m *Manager) Reload() error {
	return m.ReloadWithActor("")
}

// ReloadWithActor reloads all data from storage and records an optional actor audit entry.
func (m *Manager) ReloadWithActor(actor string) error {
	bundle, err := m.storage.LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load permissions data: %w", err)
	}

	m.mu.Lock()
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
	if actor != "" {
		m.logActionLocked(actor, "general", "system", "Reloaded permissions from storage")
	}
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventReload,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})

	return nil
}

// -----------------------------------------------------------------------------
// User Operations
// -----------------------------------------------------------------------------

// GetUser retrieves a user by UUID. Published user records are treated as
// immutable; mutating callers must use GetUserCopy and SaveUser.
func (m *Manager) GetUser(id uuid.UUID) *model.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[id]
}

// GetUserCopy retrieves a deep copy safe for command-side mutation.
func (m *Manager) GetUserCopy(id uuid.UUID) *model.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u := m.users[id]
	if u == nil {
		return nil
	}
	return copyUser(u)
}

// UserSnapshot returns a deep copy of a user together with the consistent revision.
func (m *Manager) UserSnapshot(id uuid.UUID) (*model.User, bool, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()
	u, ok := m.users[id]
	if !ok || u == nil {
		return nil, false, rev
	}
	return copyUser(u), true, rev
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

// GetUserByNameCopy is the copy-returning variant used by command handlers.
func (m *Manager) GetUserByNameCopy(query string) *model.User {
	u, ok, _ := m.ResolveUserByName(query)
	if !ok {
		return nil
	}
	return u
}

// ResolveUserByName looks up a user by case-insensitive username or UUID string
// and returns a deep copy plus the consistent revision.
func (m *Manager) ResolveUserByName(query string) (*model.User, bool, uint64) {
	query = strings.TrimSpace(query)
	if id, err := uuid.Parse(query); err == nil {
		return m.UserSnapshot(id)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()

	if id, ok := m.nameToUUID[strings.ToLower(query)]; ok {
		if u := m.users[id]; u != nil {
			return copyUser(u), true, rev
		}
	}
	return nil, false, rev
}

// ListUsers returns deterministically sorted user copies matching groupFilter with pagination.
func (m *Manager) ListUsers(groupFilter string, offset, limit int) ([]*model.User, int, uint64) {
	groupFilter = strings.ToLower(strings.TrimSpace(groupFilter))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()
	now := time.Now()

	matched := make([]*model.User, 0, len(m.users))
	for _, u := range m.users {
		if u == nil {
			continue
		}
		if groupFilter != "" && !userInGroup(u, groupFilter, now) {
			continue
		}
		matched = append(matched, u)
	}

	sort.SliceStable(matched, func(i, j int) bool {
		li := strings.ToLower(matched[i].Username)
		lj := strings.ToLower(matched[j].Username)
		if li != lj {
			return li < lj
		}
		return matched[i].UUID.String() < matched[j].UUID.String()
	})

	total := len(matched)
	if offset >= total {
		return make([]*model.User, 0), total, rev
	}
	end := offset + limit
	if end > total {
		end = total
	}

	res := make([]*model.User, 0, end-offset)
	for _, u := range matched[offset:end] {
		res = append(res, copyUser(u))
	}
	return res, total, rev
}

func userInGroup(u *model.User, groupName string, now time.Time) bool {
	if strings.EqualFold(u.PrimaryGroup, groupName) {
		return true
	}
	for _, p := range u.Parents {
		if strings.EqualFold(p.Group, groupName) && !p.IsExpired(now) {
			return true
		}
	}
	return false
}

// GetOrCreateUser retrieves an existing user or creates a new one with the given UUID and name.
func (m *Manager) GetOrCreateUser(id uuid.UUID, username string) *model.User {
	m.mu.Lock()
	if u, ok := m.users[id]; ok {
		if username != "" && u.Username != username {
			cp := copyUser(u)
			if u.Username != "" && !strings.EqualFold(u.Username, username) {
				delete(m.nameToUUID, strings.ToLower(u.Username))
			}
			cp.Username = username
			m.users[id] = cp
			m.nameToUUID[strings.ToLower(username)] = id
			rev := m.bumpRevisionLocked()
			m.mu.Unlock()
			_ = m.storage.SaveUser(cp)
			m.notifyEvent(ChangeEvent{
				Revision:  rev,
				Type:      EventUserUpdated,
				UUID:      id.String(),
				Username:  cp.Username,
				Timestamp: time.Now().UTC().Unix(),
			})
			return cp
		}
		m.mu.Unlock()
		return u
	}

	defGroup := m.defaultGroup
	if defGroup == "" {
		defGroup = "default"
	}
	u := model.NewUser(id, username)
	u.PrimaryGroup = defGroup
	m.users[id] = u
	if username != "" {
		m.nameToUUID[strings.ToLower(username)] = id
	}
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	_ = m.storage.SaveUser(u)
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserUpdated,
		UUID:      id.String(),
		Username:  username,
		Timestamp: time.Now().UTC().Unix(),
	})
	return u
}

// SaveUser saves the user state to storage and updates in-memory indexes.
// The in-memory publication and revision bump happen under one write lock.
func (m *Manager) SaveUser(user *model.User) error {
	m.mu.Lock()
	if old, ok := m.users[user.UUID]; ok && old != nil && old.Username != "" && !strings.EqualFold(old.Username, user.Username) {
		delete(m.nameToUUID, strings.ToLower(old.Username))
	}
	m.users[user.UUID] = user
	if user.Username != "" {
		m.nameToUUID[strings.ToLower(user.Username)] = user.UUID
	}
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	err := m.storage.SaveUser(user)
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserUpdated,
		UUID:      user.UUID.String(),
		Username:  user.Username,
		Timestamp: time.Now().UTC().Unix(),
	})
	return err
}

// DeleteUser deletes a user from memory and storage.
func (m *Manager) DeleteUser(id uuid.UUID) error {
	m.mu.Lock()
	var rev uint64
	var username string
	deleted := false
	if u, ok := m.users[id]; ok {
		username = u.Username
		if u.Username != "" {
			delete(m.nameToUUID, strings.ToLower(u.Username))
		}
		delete(m.users, id)
		rev = m.bumpRevisionLocked()
		deleted = true
	}
	m.mu.Unlock()

	err := m.storage.DeleteUser(id)
	if deleted {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventUserDeleted,
			UUID:      id.String(),
			Username:  username,
			Timestamp: time.Now().UTC().Unix(),
		})
	} else {
		m.notifyUpdate()
	}
	return err
}

// -----------------------------------------------------------------------------
// Group Operations
// -----------------------------------------------------------------------------

// GetGroup retrieves a group by name. Published group records are treated as
// immutable; mutating callers must use GetGroupCopy and SaveGroup.
func (m *Manager) GetGroup(name string) *model.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.groups[strings.ToLower(strings.TrimSpace(name))]
}

// GetGroupCopy retrieves a deep copy safe for command-side mutation.
func (m *Manager) GetGroupCopy(name string) *model.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g := m.groups[strings.ToLower(strings.TrimSpace(name))]
	if g == nil {
		return nil
	}
	return copyGroup(g)
}

// GetGroups returns all groups sorted deterministically by Weight descending then Name ascending.
func (m *Manager) GetGroups() []*model.Group {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*model.Group, 0, len(m.groups))
	for _, g := range m.groups {
		res = append(res, g)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Weight != res[j].Weight {
			return res[i].Weight > res[j].Weight
		}
		return res[i].Name < res[j].Name
	})
	return res
}

// CreateGroup creates a new group, saves it, and adds it to cache.
func (m *Manager) CreateGroup(name string, weight int, displayName string) (*model.Group, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	m.mu.Lock()
	if _, exists := m.groups[name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("group '%s' already exists", name)
	}

	g := model.NewGroup(name, weight)
	g.DisplayName = displayName
	m.groups[name] = g
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	// The group remains visible with its new revision if persistence fails;
	// this keeps the version consistent with the published state.
	if err := m.storage.SaveGroup(g); err != nil {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventGroupUpdated,
			Group:     name,
			Timestamp: time.Now().UTC().Unix(),
		})
		return nil, err
	}
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventGroupUpdated,
		Group:     name,
		Timestamp: time.Now().UTC().Unix(),
	})
	return g, nil
}

// SaveGroup persists a group to storage. Publication and revision bump happen
// under one write lock.
func (m *Manager) SaveGroup(group *model.Group) error {
	name := strings.ToLower(group.Name)
	m.mu.Lock()
	m.groups[name] = group
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	err := m.storage.SaveGroup(group)
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventGroupUpdated,
		Group:     name,
		Timestamp: time.Now().UTC().Unix(),
	})
	return err
}

// DeleteGroup removes a group from cache and storage.
func (m *Manager) DeleteGroup(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "default" {
		return fmt.Errorf("cannot delete the default group")
	}

	m.mu.Lock()
	if m.defaultGroup != "" && name == m.defaultGroup {
		m.mu.Unlock()
		return fmt.Errorf("cannot delete the default group")
	}
	var rev uint64
	deleted := false
	if _, ok := m.groups[name]; ok {
		delete(m.groups, name)
		rev = m.bumpRevisionLocked()
		deleted = true
	}
	m.mu.Unlock()

	err := m.storage.DeleteGroup(name)
	if deleted {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventGroupDeleted,
			Group:     name,
			Timestamp: time.Now().UTC().Unix(),
		})
	} else {
		m.notifyUpdate()
	}
	return err
}

// GetGroupMembers returns active (non-expired) users that have this group as primary group or direct parent.
func (m *Manager) GetGroupMembers(groupName string) []*model.User {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	var members []*model.User
	for _, u := range m.users {
		if u == nil {
			continue
		}
		if strings.EqualFold(u.PrimaryGroup, groupName) {
			members = append(members, u)
			continue
		}
		for _, p := range u.Parents {
			if strings.EqualFold(p.Group, groupName) && !p.IsExpired(now) {
				members = append(members, u)
				break
			}
		}
	}
	sort.SliceStable(members, func(i, j int) bool {
		li := strings.ToLower(members[i].Username)
		lj := strings.ToLower(members[j].Username)
		if li != lj {
			return li < lj
		}
		return members[i].UUID.String() < members[j].UUID.String()
	})
	return members
}

// GroupMembersSnapshot returns active (non-expired) members of groupName together
// with the consistent revision. Returns found=false if groupName does not exist.
func (m *Manager) GroupMembersSnapshot(groupName string) ([]GroupMemberInfo, bool, uint64) {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()

	if _, ok := m.groups[groupName]; !ok {
		return nil, false, rev
	}

	now := time.Now()
	members := make([]GroupMemberInfo, 0)
	for _, u := range m.users {
		if u == nil {
			continue
		}
		if strings.EqualFold(u.PrimaryGroup, groupName) {
			members = append(members, GroupMemberInfo{
				UUID:         u.UUID,
				Username:     u.Username,
				PrimaryGroup: u.PrimaryGroup,
				Permanent:    true,
			})
			continue
		}
		var best *model.Inheritance
		for i := range u.Parents {
			p := u.Parents[i]
			if !strings.EqualFold(p.Group, groupName) || p.IsExpired(now) {
				continue
			}
			if best == nil || (p.Expiry <= 0 && best.Expiry > 0) || (best.Expiry > 0 && p.Expiry > best.Expiry) {
				cp := p
				best = &cp
			}
		}
		if best != nil {
			var ctxs map[string]string
			if len(best.Contexts) > 0 {
				ctxs = make(map[string]string, len(best.Contexts))
				for k, v := range best.Contexts {
					ctxs[k] = v
				}
			}
			members = append(members, GroupMemberInfo{
				UUID:         u.UUID,
				Username:     u.Username,
				PrimaryGroup: u.PrimaryGroup,
				Permanent:    best.Expiry <= 0,
				Expiry:       best.Expiry,
				Contexts:     ctxs,
			})
		}
	}

	sort.SliceStable(members, func(i, j int) bool {
		li := strings.ToLower(members[i].Username)
		lj := strings.ToLower(members[j].Username)
		if li != lj {
			return li < lj
		}
		return members[i].UUID.String() < members[j].UUID.String()
	})
	return members, true, rev
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

// GetTrackCopy retrieves a deep copy safe for command-side mutation.
func (m *Manager) GetTrackCopy(name string) *model.Track {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t := m.tracks[strings.ToLower(strings.TrimSpace(name))]
	if t == nil {
		return nil
	}
	return copyTrack(t)
}

// GetTracks returns all tracks sorted deterministically by Name ascending.
func (m *Manager) GetTracks() []*model.Track {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*model.Track, 0, len(m.tracks))
	for _, t := range m.tracks {
		res = append(res, t)
	}
	sort.SliceStable(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})
	return res
}

// TracksSnapshot returns deterministically sorted deep copies of all tracks and the revision.
func (m *Manager) TracksSnapshot() ([]*model.Track, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()

	res := make([]*model.Track, 0, len(m.tracks))
	for _, t := range m.tracks {
		res = append(res, copyTrack(t))
	}
	sort.SliceStable(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})
	return res, rev
}

// TrackSnapshot returns a deep copy of a single track and the revision.
func (m *Manager) TrackSnapshot(name string) (*model.Track, bool, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()

	t, ok := m.tracks[strings.ToLower(strings.TrimSpace(name))]
	if !ok || t == nil {
		return nil, false, rev
	}
	return copyTrack(t), true, rev
}

// CreateTrack creates a new track.
func (m *Manager) CreateTrack(name string) (*model.Track, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("track name cannot be empty")
	}

	m.mu.Lock()
	if _, exists := m.tracks[name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("track '%s' already exists", name)
	}

	t := model.NewTrack(name)
	m.tracks[name] = t
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	if err := m.storage.SaveTrack(t); err != nil {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventTrackUpdated,
			Track:     name,
			Timestamp: time.Now().UTC().Unix(),
		})
		return nil, err
	}
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventTrackUpdated,
		Track:     name,
		Timestamp: time.Now().UTC().Unix(),
	})
	return t, nil
}

// SaveTrack persists a track.
func (m *Manager) SaveTrack(track *model.Track) error {
	name := strings.ToLower(track.Name)
	m.mu.Lock()
	m.tracks[name] = track
	rev := m.bumpRevisionLocked()
	m.mu.Unlock()

	err := m.storage.SaveTrack(track)
	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventTrackUpdated,
		Track:     name,
		Timestamp: time.Now().UTC().Unix(),
	})
	return err
}

// DeleteTrack removes a track.
func (m *Manager) DeleteTrack(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	m.mu.Lock()
	var rev uint64
	deleted := false
	if _, ok := m.tracks[name]; ok {
		delete(m.tracks, name)
		rev = m.bumpRevisionLocked()
		deleted = true
	}
	m.mu.Unlock()

	err := m.storage.DeleteTrack(name)
	if deleted {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventTrackDeleted,
			Track:     name,
			Timestamp: time.Now().UTC().Unix(),
		})
	} else {
		m.notifyUpdate()
	}
	return err
}

// PromoteUser promotes a user along a track.
func (m *Manager) PromoteUser(user *model.User, trackName string, actor string) (string, string, error) {
	if user == nil {
		return "", "", ErrUserNotFound
	}
	_, from, to, _, err := m.stepUserOnTrack(user.UUID, trackName, actor, true)
	return from, to, err
}

// DemoteUser demotes a user along a track.
func (m *Manager) DemoteUser(user *model.User, trackName string, actor string) (string, string, error) {
	if user == nil {
		return "", "", ErrUserNotFound
	}
	_, from, to, _, err := m.stepUserOnTrack(user.UUID, trackName, actor, false)
	return from, to, err
}

// PromoteUserByID promotes a user by UUID and returns a deep copy plus the new revision.
func (m *Manager) PromoteUserByID(id uuid.UUID, trackName string, actor string) (*model.User, string, string, uint64, error) {
	return m.stepUserOnTrack(id, trackName, actor, true)
}

// DemoteUserByID demotes a user by UUID and returns a deep copy plus the new revision.
func (m *Manager) DemoteUserByID(id uuid.UUID, trackName string, actor string) (*model.User, string, string, uint64, error) {
	return m.stepUserOnTrack(id, trackName, actor, false)
}

func (m *Manager) stepUserOnTrack(id uuid.UUID, trackName string, actor string, promote bool) (*model.User, string, string, uint64, error) {
	trackKey := strings.ToLower(strings.TrimSpace(trackName))

	m.mu.Lock()
	track := m.tracks[trackKey]
	if track == nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, "", "", rev, fmt.Errorf("%w: %s", ErrTrackNotFound, trackName)
	}
	if len(track.Groups) == 0 {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, "", "", rev, fmt.Errorf("%w: %s", ErrTrackEmpty, trackName)
	}

	target, ok := m.users[id]
	if !ok || target == nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, "", "", rev, ErrUserNotFound
	}
	updated := copyUser(target)

	currentGroup := ""
	for _, gName := range track.Groups {
		if strings.EqualFold(updated.PrimaryGroup, gName) {
			currentGroup = gName
			continue
		}
		for _, p := range updated.Parents {
			if strings.EqualFold(p.Group, gName) {
				currentGroup = gName
				break
			}
		}
	}

	var nextGroup string
	if promote {
		var ok bool
		nextGroup, ok = track.NextGroup(currentGroup)
		if !ok {
			if currentGroup != "" {
				rev := m.revision.Load()
				m.mu.Unlock()
				return nil, currentGroup, "", rev, fmt.Errorf("%w: %s", ErrTrackTop, trackName)
			}
			nextGroup = track.Groups[0]
		}
	} else {
		if currentGroup == "" {
			rev := m.revision.Load()
			m.mu.Unlock()
			return nil, "", "", rev, fmt.Errorf("%w: %s", ErrNotOnTrack, trackName)
		}
		var ok bool
		nextGroup, ok = track.PreviousGroup(currentGroup)
		if !ok {
			rev := m.revision.Load()
			m.mu.Unlock()
			return nil, currentGroup, "", rev, fmt.Errorf("%w: %s", ErrTrackBottom, trackName)
		}
	}

	if currentGroup != "" {
		updated.RemoveParent(currentGroup, nil)
	}
	updated.AddParent(model.NewInheritance(nextGroup))
	updated.PrimaryGroup = nextGroup

	if err := m.storage.SaveUser(updated); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, "", "", rev, err
	}

	m.users[updated.UUID] = updated
	rev := m.bumpRevisionLocked()
	verb := "Demoted"
	if promote {
		verb = "Promoted"
	}
	m.logActionLocked(actor, "user", userLogTarget(updated), fmt.Sprintf("%s along track '%s' (%s -> %s)", verb, trackName, currentGroup, nextGroup))
	ret := copyUser(updated)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserUpdated,
		UUID:      updated.UUID.String(),
		Username:  updated.Username,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})

	return ret, currentGroup, nextGroup, rev, nil
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
			return m.syntheticGroupPermission(userID, perm, ctxs)
		}
	}
}

func (m *Manager) syntheticGroupPermission(userID uuid.UUID, perm string, ctxs map[string]string) permission.TriState {
	clean := strings.ToLower(strings.TrimSpace(perm))
	switch {
	case strings.HasPrefix(clean, "stonyperms.primarygroup."):
		return m.matchesEffectiveGroup(userID, strings.TrimPrefix(clean, "stonyperms.primarygroup."), ctxs, true)
	case strings.HasPrefix(clean, "bettertab.role."):
		return m.matchesEffectiveGroup(userID, strings.TrimPrefix(clean, "bettertab.role."), ctxs, true)
	case strings.HasPrefix(clean, "group."):
		return m.matchesEffectiveGroup(userID, strings.TrimPrefix(clean, "group."), ctxs, false)
	case strings.HasPrefix(clean, "stonyperms.group."):
		return m.matchesEffectiveGroup(userID, strings.TrimPrefix(clean, "stonyperms.group."), ctxs, false)
	default:
		return permission.Undefined
	}
}

func (m *Manager) matchesEffectiveGroup(userID uuid.UUID, target string, ctxs map[string]string, primaryOnly bool) permission.TriState {
	if target == "" {
		return permission.Undefined
	}
	defGroup := m.DefaultGroup()
	eff, ok, _ := m.EffectiveUser(userID, ctxs, defGroup)
	if !ok || eff == nil {
		return permission.Undefined
	}
	primary := strings.ToLower(strings.TrimSpace(eff.PrimaryGroup))
	if primary == "" {
		primary = defGroup
	}
	if primary == target {
		return permission.True
	}
	if primaryOnly {
		return permission.Undefined
	}
	for _, g := range eff.Groups {
		if strings.EqualFold(g.Name, target) {
			return permission.True
		}
	}
	return permission.Undefined
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

// EffectiveUser resolves a user's permissions for one context and returns an
// immutable snapshot. Unknown users are resolved against defaultGroup without
// being persisted, matching StonyPerms' first-login semantics. The second
// return value reports whether a stored user record was found.
func (m *Manager) EffectiveUser(id uuid.UUID, contexts map[string]string, defaultGroup string) (*EffectiveUser, bool, uint64) {
	if defaultGroup == "" {
		defaultGroup = "default"
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	revision := m.revision.Load()
	now := time.Now()

	eff, found := m.effectiveUserLocked(id, contexts, defaultGroup, now)
	return eff, found, revision
}

// EffectiveUsersBatch resolves multiple users under a single read lock for
// snapshot consistency at one revision.
func (m *Manager) EffectiveUsersBatch(ids []uuid.UUID, contexts map[string]string, defaultGroup string) (map[uuid.UUID]*EffectiveUser, map[uuid.UUID]bool, uint64) {
	if defaultGroup == "" {
		defaultGroup = "default"
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	revision := m.revision.Load()
	now := time.Now()

	effs := make(map[uuid.UUID]*EffectiveUser, len(ids))
	foundMap := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if _, seen := effs[id]; seen {
			continue
		}
		eff, found := m.effectiveUserLocked(id, contexts, defaultGroup, now)
		effs[id] = eff
		foundMap[id] = found
	}
	return effs, foundMap, revision
}

func (m *Manager) effectiveUserLocked(id uuid.UUID, contexts map[string]string, defaultGroup string, now time.Time) (*EffectiveUser, bool) {
	user, found := m.users[id]
	if !found {
		user = model.NewUser(id, "")
		user.PrimaryGroup = defaultGroup
	}

	resolved := m.engine.ResolveInheritedGroups(user, m.groups, contexts, now)
	primaryGroup := m.engine.ResolvePrimaryGroup(user, m.groups, contexts, defaultGroup)

	eff := &EffectiveUser{
		UUID:         id,
		Username:     user.Username,
		PrimaryGroup: primaryGroup,
		Synthetic:    !found,
		Groups:       make([]EffectiveGroup, 0, len(resolved)),
		Nodes:        make([]EffectiveNode, 0),
		Meta:         make(map[string]string, len(user.Meta)),
	}

	for _, g := range resolved {
		eff.Groups = append(eff.Groups, EffectiveGroup{
			Name:        g.Name,
			DisplayName: g.GetDisplayName(),
			Weight:      g.Weight,
		})
	}

	for _, n := range user.Nodes {
		if n.IsExpired(now) || !n.MatchesContext(contexts) {
			continue
		}
		eff.Nodes = append(eff.Nodes, EffectiveNode{
			Node:   copyNode(n),
			Source: "user",
			Weight: 0,
			User:   true,
		})
	}

	for _, g := range resolved {
		for _, n := range g.Nodes {
			if n.IsExpired(now) || !n.MatchesContext(contexts) {
				continue
			}
			eff.Nodes = append(eff.Nodes, EffectiveNode{
				Node:   copyNode(n),
				Source: "group:" + g.Name,
				Weight: g.Weight,
				User:   false,
			})
		}
	}

	eff.Meta = m.engine.ResolveAllMeta(user, m.groups, contexts)

	eff.Prefix = m.engine.GetPrefix(user, m.groups, contexts)
	eff.Suffix = m.engine.GetSuffix(user, m.groups, contexts)

	return eff, found
}

// EvaluateUser evaluates a single permission with the authoritative engine and
// returns the revision of the same read lock. Unknown users are evaluated
// against defaultGroup without being persisted.
func (m *Manager) EvaluateUser(id uuid.UUID, permission string, contexts map[string]string, defaultGroup string) (model.Tristate, string, uint64) {
	if defaultGroup == "" {
		defaultGroup = "default"
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	revision := m.revision.Load()

	user, found := m.users[id]
	if !found {
		user = model.NewUser(id, "")
		user.PrimaryGroup = defaultGroup
	}

	res := m.engine.Evaluate(user, m.groups, permission, contexts)
	return res.Result, res.Source, revision
}

// GroupsSnapshot returns deep copies of all groups ordered by descending
// weight, with name as a stable tie-breaker, plus the revision of the same
// read lock.
func (m *Manager) GroupsSnapshot() ([]*model.Group, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	revision := m.revision.Load()

	res := make([]*model.Group, 0, len(m.groups))
	for _, g := range m.groups {
		res = append(res, copyGroup(g))
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Weight != res[j].Weight {
			return res[i].Weight > res[j].Weight
		}
		return res[i].Name < res[j].Name
	})
	return res, revision
}

// GroupSnapshot returns a deep copy of one group and the revision of the same
// read lock.
func (m *Manager) GroupSnapshot(name string) (*model.Group, bool, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	revision := m.revision.Load()

	g, ok := m.groups[strings.ToLower(strings.TrimSpace(name))]
	if !ok || g == nil {
		return nil, false, revision
	}
	return copyGroup(g), true, revision
}

func copyNode(n model.Node) model.Node {
	if n.Contexts != nil {
		ctxs := make(map[string]string, len(n.Contexts))
		for k, v := range n.Contexts {
			ctxs[k] = v
		}
		n.Contexts = ctxs
	}
	return n
}

func copyUser(u *model.User) *model.User {
	cp := model.NewUser(u.UUID, u.Username)
	cp.PrimaryGroup = u.PrimaryGroup
	for _, n := range u.Nodes {
		cp.Nodes = append(cp.Nodes, copyNode(n))
	}
	for _, p := range u.Parents {
		cp.Parents = append(cp.Parents, copyInheritance(p))
	}
	for k, v := range u.Meta {
		cp.Meta[k] = v
	}
	for _, me := range u.MetaEntries {
		cp.MetaEntries = append(cp.MetaEntries, copyMetaEntry(me))
	}
	for _, p := range u.Prefixes {
		cp.Prefixes = append(cp.Prefixes, copyPriorityMeta(p))
	}
	for _, s := range u.Suffixes {
		cp.Suffixes = append(cp.Suffixes, copyPriorityMeta(s))
	}
	return cp
}

func copyTrack(t *model.Track) *model.Track {
	cp := model.NewTrack(t.Name)
	cp.Groups = append(cp.Groups, t.Groups...)
	return cp
}

func copyGroup(g *model.Group) *model.Group {
	cp := model.NewGroup(g.Name, g.Weight)
	cp.DisplayName = g.DisplayName
	for _, n := range g.Nodes {
		cp.Nodes = append(cp.Nodes, copyNode(n))
	}
	for _, p := range g.Parents {
		cp.Parents = append(cp.Parents, copyInheritance(p))
	}
	for k, v := range g.Meta {
		cp.Meta[k] = v
	}
	for _, me := range g.MetaEntries {
		cp.MetaEntries = append(cp.MetaEntries, copyMetaEntry(me))
	}
	for _, p := range g.Prefixes {
		cp.Prefixes = append(cp.Prefixes, copyPriorityMeta(p))
	}
	for _, s := range g.Suffixes {
		cp.Suffixes = append(cp.Suffixes, copyPriorityMeta(s))
	}
	return cp
}

func copyInheritance(i model.Inheritance) model.Inheritance {
	if i.Contexts != nil {
		ctxs := make(map[string]string, len(i.Contexts))
		for k, v := range i.Contexts {
			ctxs[k] = v
		}
		i.Contexts = ctxs
	}
	return i
}

func copyMetaEntry(me model.MetaEntry) model.MetaEntry {
	if me.Contexts != nil {
		ctxs := make(map[string]string, len(me.Contexts))
		for k, v := range me.Contexts {
			ctxs[k] = v
		}
		me.Contexts = ctxs
	}
	return me
}

func copyPriorityMeta(p model.PriorityMeta) model.PriorityMeta {
	if p.Contexts != nil {
		ctxs := make(map[string]string, len(p.Contexts))
		for k, v := range p.Contexts {
			ctxs[k] = v
		}
		p.Contexts = ctxs
	}
	return p
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
	if m.auditLogDisabled {
		return
	}
	entry := model.NewLogEntry(actor, targetType, target, action)
	m.logs = append(m.logs, entry)
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-1000:]
	}
	_ = m.storage.AppendLog(entry)
}

// GetLogs returns copied audit action logs up to limit (chronological order).
func (m *Manager) GetLogs(limit int) []model.LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	src := m.logs
	if limit > 0 && len(src) > limit {
		src = src[len(src)-limit:]
	}
	out := make([]model.LogEntry, len(src))
	copy(out, src)
	return out
}

// QueryLogs returns matching audit logs newest-first up to limit together with
// the current revision.
func (m *Manager) QueryLogs(limit int, targetType, target, actor string) ([]model.LogEntry, uint64) {
	targetType = strings.ToLower(strings.TrimSpace(targetType))
	target = strings.ToLower(strings.TrimSpace(target))
	actor = strings.ToLower(strings.TrimSpace(actor))

	m.mu.RLock()
	defer m.mu.RUnlock()
	rev := m.revision.Load()

	out := make([]model.LogEntry, 0)
	for i := len(m.logs) - 1; i >= 0; i-- {
		entry := m.logs[i]
		if targetType != "" && !strings.EqualFold(entry.TargetType, targetType) {
			continue
		}
		if target != "" && !strings.EqualFold(entry.Target, target) {
			continue
		}
		if actor != "" && !strings.EqualFold(entry.Actor, actor) {
			continue
		}
		out = append(out, entry)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rev
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
	changed := false

	for id, u := range m.users {
		cp := copyUser(u)
		if cleaned := cp.CleanExpired(now); cleaned > 0 {
			m.users[id] = cp
			_ = m.storage.SaveUser(cp)
			changed = true
		}
	}
	for name, g := range m.groups {
		cp := copyGroup(g)
		if cleaned := cp.CleanExpired(now); cleaned > 0 {
			m.groups[name] = cp
			_ = m.storage.SaveGroup(cp)
			changed = true
		}
	}
	var rev uint64
	if changed {
		rev = m.bumpRevisionLocked()
	}
	m.mu.Unlock()

	if changed {
		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventExpiryCleanup,
			Timestamp: now.UTC().Unix(),
		})
	}
}
