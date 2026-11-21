package manager

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrGroupNotFound       = errors.New("group not found")
	ErrTrackNotFound       = errors.New("track not found")
	ErrGroupExists         = errors.New("group already exists")
	ErrTrackExists         = errors.New("track already exists")
	ErrCannotDeleteDefault = errors.New("cannot delete default group")
)

func userLogTarget(u *model.User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return u.Username
	}
	return u.UUID.String()
}

// MutateUser atomically loads, copies, mutates, persists, and publishes a user.
// fn runs while m.mu is held and must not call back into Manager methods.
// Storage persistence runs before in-memory publication so disk errors leave
// published state and revision unchanged.
func (m *Manager) MutateUser(
	id uuid.UUID,
	username string,
	createIfMissing bool,
	defaultGroup string,
	actor string,
	fn func(u *model.User) (changed bool, auditAction string, err error),
) (*model.User, uint64, error) {
	if id == uuid.Nil {
		return nil, m.revision.Load(), errors.New("invalid user uuid")
	}
	if defaultGroup == "" {
		defaultGroup = "default"
	}

	m.mu.Lock()
	existing, found := m.users[id]
	var cp *model.User
	if !found {
		if !createIfMissing {
			rev := m.revision.Load()
			m.mu.Unlock()
			return nil, rev, ErrUserNotFound
		}
		cp = model.NewUser(id, username)
		cp.PrimaryGroup = defaultGroup
	} else {
		cp = copyUser(existing)
		if username != "" {
			cp.Username = username
		}
	}

	changed, auditAction, err := fn(cp)
	if err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}

	renamed := found && username != "" && existing.Username != cp.Username
	effectiveChanged := changed || !found || renamed
	if !effectiveChanged {
		rev := m.revision.Load()
		ret := copyUser(existing)
		m.mu.Unlock()
		return ret, rev, nil
	}

	if err := m.storage.SaveUser(cp); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}

	if found && existing.Username != "" && !strings.EqualFold(existing.Username, cp.Username) {
		delete(m.nameToUUID, strings.ToLower(existing.Username))
	}
	m.users[id] = cp
	if cp.Username != "" {
		m.nameToUUID[strings.ToLower(cp.Username)] = id
	}
	rev := m.bumpRevisionLocked()
	if auditAction != "" {
		m.logActionLocked(actor, "user", userLogTarget(cp), auditAction)
	}
	ret := copyUser(cp)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserUpdated,
		UUID:      id.String(),
		Username:  cp.Username,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, rev, nil
}

// ResolveOrCreateUser finds an existing user by UUID (updating username if provided)
// or creates and persists a new user with defaultGroup.
func (m *Manager) ResolveOrCreateUser(id uuid.UUID, username string, defaultGroup string, actor string) (*model.User, bool, uint64, error) {
	if id == uuid.Nil {
		return nil, false, m.revision.Load(), errors.New("invalid user uuid")
	}
	if defaultGroup == "" {
		defaultGroup = "default"
	}

	m.mu.Lock()
	existing, found := m.users[id]
	if found {
		if username == "" || existing.Username == username {
			rev := m.revision.Load()
			ret := copyUser(existing)
			m.mu.Unlock()
			return ret, false, rev, nil
		}
		cp := copyUser(existing)
		cp.Username = username
		if err := m.storage.SaveUser(cp); err != nil {
			rev := m.revision.Load()
			m.mu.Unlock()
			return nil, false, rev, err
		}
		if existing.Username != "" && !strings.EqualFold(existing.Username, cp.Username) {
			delete(m.nameToUUID, strings.ToLower(existing.Username))
		}
		m.users[id] = cp
		m.nameToUUID[strings.ToLower(cp.Username)] = id
		rev := m.bumpRevisionLocked()
		if actor != "" {
			m.logActionLocked(actor, "user", userLogTarget(cp), fmt.Sprintf("Updated username to %s", cp.Username))
		}
		ret := copyUser(cp)
		m.mu.Unlock()

		m.notifyEvent(ChangeEvent{
			Revision:  rev,
			Type:      EventUserUpdated,
			UUID:      id.String(),
			Username:  cp.Username,
			Actor:     actor,
			Timestamp: time.Now().UTC().Unix(),
		})
		return ret, false, rev, nil
	}

	u := model.NewUser(id, username)
	u.PrimaryGroup = defaultGroup
	if err := m.storage.SaveUser(u); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, false, rev, err
	}

	m.users[id] = u
	if u.Username != "" {
		m.nameToUUID[strings.ToLower(u.Username)] = id
	}
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "user", userLogTarget(u), fmt.Sprintf("Created user (primaryGroup: %s)", defaultGroup))
	}
	ret := copyUser(u)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserUpdated,
		UUID:      id.String(),
		Username:  u.Username,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, true, rev, nil
}

// DeleteUserByID removes an existing user from storage and memory with audit attribution.
func (m *Manager) DeleteUserByID(id uuid.UUID, actor string) (uint64, error) {
	m.mu.Lock()
	u, ok := m.users[id]
	if !ok || u == nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, ErrUserNotFound
	}
	if err := m.storage.DeleteUser(id); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, err
	}
	if u.Username != "" {
		delete(m.nameToUUID, strings.ToLower(u.Username))
	}
	delete(m.users, id)
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "user", userLogTarget(u), "Deleted user")
	}
	username := u.Username
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventUserDeleted,
		UUID:      id.String(),
		Username:  username,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return rev, nil
}

// MutateGroup atomically copies, mutates, persists, and publishes a group.
// fn runs while m.mu is held and must not call back into Manager methods.
func (m *Manager) MutateGroup(
	name string,
	actor string,
	fn func(g *model.Group) (changed bool, auditAction string, err error),
) (*model.Group, uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	m.mu.Lock()
	existing, ok := m.groups[name]
	if !ok || existing == nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, ErrGroupNotFound
	}
	cp := copyGroup(existing)
	changed, auditAction, err := fn(cp)
	if err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	if !changed {
		rev := m.revision.Load()
		ret := copyGroup(existing)
		m.mu.Unlock()
		return ret, rev, nil
	}
	if err := m.storage.SaveGroup(cp); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	m.groups[name] = cp
	rev := m.bumpRevisionLocked()
	if auditAction != "" {
		m.logActionLocked(actor, "group", name, auditAction)
	}
	ret := copyGroup(cp)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventGroupUpdated,
		Group:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, rev, nil
}

// CreateGroupWithActor creates and persists a group before publishing it in memory.
func (m *Manager) CreateGroupWithActor(name string, weight int, displayName string, actor string) (*model.Group, uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, m.revision.Load(), errors.New("group name cannot be empty")
	}

	m.mu.Lock()
	if _, exists := m.groups[name]; exists {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, ErrGroupExists
	}
	g := model.NewGroup(name, weight)
	g.DisplayName = displayName
	if err := m.storage.SaveGroup(g); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	m.groups[name] = g
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "group", name, fmt.Sprintf("Created group %s (weight: %d)", name, weight))
	}
	ret := copyGroup(g)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventGroupUpdated,
		Group:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, rev, nil
}

// DeleteGroupWithActor deletes a non-default group after storage deletion succeeds.
func (m *Manager) DeleteGroupWithActor(name string, actor string) (uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "default" {
		return m.revision.Load(), ErrCannotDeleteDefault
	}

	m.mu.Lock()
	if _, exists := m.groups[name]; !exists {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, ErrGroupNotFound
	}
	if err := m.storage.DeleteGroup(name); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, err
	}
	delete(m.groups, name)
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "group", name, fmt.Sprintf("Deleted group %s", name))
	}
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventGroupDeleted,
		Group:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return rev, nil
}

// MutateTrack atomically copies, mutates, persists, and publishes a track.
// fn runs while m.mu is held and must not call back into Manager methods.
func (m *Manager) MutateTrack(
	name string,
	actor string,
	fn func(t *model.Track) (changed bool, auditAction string, err error),
) (*model.Track, uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	m.mu.Lock()
	existing, ok := m.tracks[name]
	if !ok || existing == nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, ErrTrackNotFound
	}
	cp := copyTrack(existing)
	changed, auditAction, err := fn(cp)
	if err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	if !changed {
		rev := m.revision.Load()
		ret := copyTrack(existing)
		m.mu.Unlock()
		return ret, rev, nil
	}
	if err := m.storage.SaveTrack(cp); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	m.tracks[name] = cp
	rev := m.bumpRevisionLocked()
	if auditAction != "" {
		m.logActionLocked(actor, "track", name, auditAction)
	}
	ret := copyTrack(cp)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventTrackUpdated,
		Track:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, rev, nil
}

// CreateTrackWithActor creates a track with validated groups and persists before publishing.
func (m *Manager) CreateTrackWithActor(name string, groups []string, actor string) (*model.Track, uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, m.revision.Load(), errors.New("track name cannot be empty")
	}

	m.mu.Lock()
	if _, exists := m.tracks[name]; exists {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, ErrTrackExists
	}
	normalized, err := m.validateTrackGroupsLocked(groups)
	if err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	t := model.NewTrack(name)
	t.Groups = normalized
	if err := m.storage.SaveTrack(t); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return nil, rev, err
	}
	m.tracks[name] = t
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "track", name, fmt.Sprintf("Created track %s", name))
	}
	ret := copyTrack(t)
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventTrackUpdated,
		Track:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ret, rev, nil
}

// SetTrackGroups atomically replaces a track's group progression after validating every group exists.
func (m *Manager) SetTrackGroups(name string, groups []string, actor string) (*model.Track, uint64, error) {
	return m.MutateTrack(name, actor, func(t *model.Track) (bool, string, error) {
		normalized, err := m.validateTrackGroupsLocked(groups)
		if err != nil {
			return false, "", err
		}
		t.Groups = normalized
		return true, fmt.Sprintf("Updated track groups (%d)", len(normalized)), nil
	})
}

func (m *Manager) validateTrackGroupsLocked(groups []string) ([]string, error) {
	normalized := make([]string, 0, len(groups))
	seen := make(map[string]bool, len(groups))
	for _, raw := range groups {
		g := strings.ToLower(strings.TrimSpace(raw))
		if g == "" {
			return nil, errors.New("track group name cannot be empty")
		}
		if seen[g] {
			return nil, fmt.Errorf("duplicate group '%s' in track", g)
		}
		if _, ok := m.groups[g]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrGroupNotFound, g)
		}
		seen[g] = true
		normalized = append(normalized, g)
	}
	return normalized, nil
}

// DeleteTrackWithActor deletes an existing track after storage deletion succeeds.
func (m *Manager) DeleteTrackWithActor(name string, actor string) (uint64, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	m.mu.Lock()
	if _, exists := m.tracks[name]; !exists {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, ErrTrackNotFound
	}
	if err := m.storage.DeleteTrack(name); err != nil {
		rev := m.revision.Load()
		m.mu.Unlock()
		return rev, err
	}
	delete(m.tracks, name)
	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "track", name, fmt.Sprintf("Deleted track %s", name))
	}
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventTrackDeleted,
		Track:     name,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return rev, nil
}
