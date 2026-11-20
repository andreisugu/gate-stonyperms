package manager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

// Manager coordinates in-memory data, persistence, permission evaluation, and promotion tracks.
type Manager struct {
	mu             sync.RWMutex
	storage        storage.StorageProvider
	engine         *engine.Engine
	users          map[uuid.UUID]*model.User
	nameToUUID     map[string]uuid.UUID
	groups         map[string]*model.Group
	tracks         map[string]*model.Track
	logs           []model.LogEntry
	updateListener atomic.Pointer[func()]
	stopChan       chan struct{}
	revision       atomic.Uint64
	generation     string
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

// Revision returns a monotonically increasing counter that changes whenever
// users or groups are modified. It is used for cheap cache validation by
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
func (m *Manager) bumpRevisionLocked() {
	m.revision.Add(1)
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
		storage:    store,
		engine:     engine.New(),
		users:      make(map[uuid.UUID]*model.User),
		nameToUUID: make(map[string]uuid.UUID),
		groups:     make(map[string]*model.Group),
		tracks:     make(map[string]*model.Track),
		logs:       make([]model.LogEntry, 0),
		stopChan:   make(chan struct{}),
		generation: newGeneration(),
	}
	m.revision.Store(1)

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
	m.bumpRevisionLocked()
	m.mu.Unlock()
	m.notifyUpdate()

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
	query = strings.TrimSpace(query)
	if id, err := uuid.Parse(query); err == nil {
		return m.GetUserCopy(id)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if id, ok := m.nameToUUID[strings.ToLower(query)]; ok {
		u := m.users[id]
		if u == nil {
			return nil
		}
		return copyUser(u)
	}
	return nil
}

// GetOrCreateUser retrieves an existing user or creates a new one with the given UUID and name.
func (m *Manager) GetOrCreateUser(id uuid.UUID, username string) *model.User {
	m.mu.Lock()
	if u, ok := m.users[id]; ok {
		if username != "" && u.Username != username {
			cp := copyUser(u)
			cp.Username = username
			m.users[id] = cp
			m.nameToUUID[strings.ToLower(username)] = id
			m.bumpRevisionLocked()
			m.mu.Unlock()
			_ = m.storage.SaveUser(cp)
			m.notifyUpdate()
			return cp
		}
		m.mu.Unlock()
		return u
	}

	u := model.NewUser(id, username)
	m.users[id] = u
	if username != "" {
		m.nameToUUID[strings.ToLower(username)] = id
	}
	m.bumpRevisionLocked()
	m.mu.Unlock()

	_ = m.storage.SaveUser(u)
	m.notifyUpdate()
	return u
}

// SaveUser saves the user state to storage and updates in-memory indexes.
// The in-memory publication and revision bump happen under one write lock.
func (m *Manager) SaveUser(user *model.User) error {
	m.mu.Lock()
	m.users[user.UUID] = user
	if user.Username != "" {
		m.nameToUUID[strings.ToLower(user.Username)] = user.UUID
	}
	m.bumpRevisionLocked()
	m.mu.Unlock()

	err := m.storage.SaveUser(user)
	m.notifyUpdate()
	return err
}

// DeleteUser deletes a user from memory and storage.
func (m *Manager) DeleteUser(id uuid.UUID) error {
	m.mu.Lock()
	if u, ok := m.users[id]; ok {
		delete(m.nameToUUID, strings.ToLower(u.Username))
		delete(m.users, id)
		m.bumpRevisionLocked()
	}
	m.mu.Unlock()

	err := m.storage.DeleteUser(id)
	m.notifyUpdate()
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
	if _, exists := m.groups[name]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("group '%s' already exists", name)
	}

	g := model.NewGroup(name, weight)
	g.DisplayName = displayName
	m.groups[name] = g
	m.bumpRevisionLocked()
	m.mu.Unlock()

	// The group remains visible with its new revision if persistence fails;
	// this keeps the version consistent with the published state.
	if err := m.storage.SaveGroup(g); err != nil {
		m.notifyUpdate()
		return nil, err
	}
	m.notifyUpdate()
	return g, nil
}

// SaveGroup persists a group to storage. Publication and revision bump happen
// under one write lock.
func (m *Manager) SaveGroup(group *model.Group) error {
	m.mu.Lock()
	m.groups[strings.ToLower(group.Name)] = group
	m.bumpRevisionLocked()
	m.mu.Unlock()

	err := m.storage.SaveGroup(group)
	m.notifyUpdate()
	return err
}

// DeleteGroup removes a group from cache and storage.
func (m *Manager) DeleteGroup(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "default" {
		return fmt.Errorf("cannot delete the default group")
	}

	m.mu.Lock()
	if _, ok := m.groups[name]; ok {
		delete(m.groups, name)
		m.bumpRevisionLocked()
	}
	m.mu.Unlock()

	err := m.storage.DeleteGroup(name)
	m.notifyUpdate()
	return err
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
	target, ok := m.users[user.UUID]
	if !ok || target == nil {
		m.mu.Unlock()
		return "", "", fmt.Errorf("user record not found")
	}
	updated := copyUser(target)

	// Find user's current highest group on this track
	currentGroup := ""
	for _, gName := range track.Groups {
		// Check primary group
		if strings.EqualFold(updated.PrimaryGroup, gName) {
			currentGroup = gName
			continue
		}
		// Check parents
		for _, p := range updated.Parents {
			if strings.EqualFold(p.Group, gName) {
				currentGroup = gName
				break
			}
		}
	}

	nextGroup, ok := track.NextGroup(currentGroup)
	if !ok {
		if currentGroup != "" {
			m.mu.Unlock()
			return currentGroup, "", fmt.Errorf("user is already at the highest group on track '%s'", trackName)
		}
		nextGroup = track.Groups[0]
	}

	// Apply promotion: remove old track groups, set new group
	if currentGroup != "" {
		updated.RemoveParent(currentGroup, nil)
	}
	updated.AddParent(model.NewInheritance(nextGroup))
	updated.PrimaryGroup = nextGroup

	m.users[updated.UUID] = updated
	m.bumpRevisionLocked()
	_ = m.storage.SaveUser(updated)
	m.logActionLocked(actor, "user", updated.Username, fmt.Sprintf("Promoted along track '%s' (%s -> %s)", trackName, currentGroup, nextGroup))
	m.mu.Unlock()
	m.notifyUpdate()

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
	target, ok := m.users[user.UUID]
	if !ok || target == nil {
		m.mu.Unlock()
		return "", "", fmt.Errorf("user record not found")
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

	if currentGroup == "" {
		m.mu.Unlock()
		return "", "", fmt.Errorf("user is not on track '%s'", trackName)
	}

	prevGroup, ok := track.PreviousGroup(currentGroup)
	if !ok {
		m.mu.Unlock()
		return currentGroup, "", fmt.Errorf("user is already at the lowest group on track '%s'", trackName)
	}

	updated.RemoveParent(currentGroup, nil)
	updated.AddParent(model.NewInheritance(prevGroup))
	updated.PrimaryGroup = prevGroup

	m.users[updated.UUID] = updated
	m.bumpRevisionLocked()
	_ = m.storage.SaveUser(updated)
	m.logActionLocked(actor, "user", updated.Username, fmt.Sprintf("Demoted along track '%s' (%s -> %s)", trackName, currentGroup, prevGroup))
	m.mu.Unlock()
	m.notifyUpdate()

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
	user, found := m.users[id]
	if !found {
		user = model.NewUser(id, "")
		user.PrimaryGroup = defaultGroup
	}

	resolved := m.engine.ResolveInheritedGroups(user, m.groups, contexts, now)
	primaryGroup := user.PrimaryGroup
	if primaryGroup == "" {
		primaryGroup = defaultGroup
	}

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

	for k, v := range user.Meta {
		eff.Meta[k] = v
	}
	for _, g := range resolved {
		for k, v := range g.Meta {
			if _, exists := eff.Meta[k]; !exists {
				eff.Meta[k] = v
			}
		}
	}

	eff.Prefix = m.engine.GetPrefix(user, m.groups, contexts)
	eff.Suffix = m.engine.GetSuffix(user, m.groups, contexts)

	return eff, found, revision
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
	if changed {
		m.bumpRevisionLocked()
	}
	m.mu.Unlock()

	if changed {
		m.notifyUpdate()
	}
}
