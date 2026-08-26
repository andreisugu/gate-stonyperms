package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// User represents a player with unique UUID, permission overrides, parent groups, and metadata.
type User struct {
	UUID         uuid.UUID         `json:"uuid"`
	Username     string            `json:"username"`
	PrimaryGroup string            `json:"primaryGroup"`
	Parents      []Inheritance     `json:"parents,omitempty"`
	Nodes        []Node            `json:"nodes,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
	Prefixes     []PriorityMeta    `json:"prefixes,omitempty"`
	Suffixes     []PriorityMeta    `json:"suffixes,omitempty"`
}

// NewUser creates a new User with default primary group.
func NewUser(id uuid.UUID, username string) *User {
	return &User{
		UUID:         id,
		Username:     username,
		PrimaryGroup: "default",
		Parents:      make([]Inheritance, 0),
		Nodes:        make([]Node, 0),
		Meta:         make(map[string]string),
		Prefixes:     make([]PriorityMeta, 0),
		Suffixes:     make([]PriorityMeta, 0),
	}
}

// AddNode adds or updates a permission node for this user.
func (u *User) AddNode(node Node) {
	node.Key = strings.ToLower(strings.TrimSpace(node.Key))
	for i, existing := range u.Nodes {
		if existing.Key == node.Key && SameContexts(existing.Contexts, node.Contexts) {
			u.Nodes[i] = node
			return
		}
	}
	u.Nodes = append(u.Nodes, node)
}

// RemoveNode removes a permission node by key and context. Returns true if removed.
func (u *User) RemoveNode(key string, contexts map[string]string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for i, existing := range u.Nodes {
		if existing.Key == key && SameContexts(existing.Contexts, contexts) {
			u.Nodes = append(u.Nodes[:i], u.Nodes[i+1:]...)
			return true
		}
	}
	return false
}

// AddParent adds an inherited parent group.
func (u *User) AddParent(inh Inheritance) {
	inh.Group = strings.ToLower(strings.TrimSpace(inh.Group))
	for i, existing := range u.Parents {
		if existing.Group == inh.Group && SameContexts(existing.Contexts, inh.Contexts) {
			u.Parents[i] = inh
			return
		}
	}
	u.Parents = append(u.Parents, inh)
}

// RemoveParent removes a parent group inheritance.
func (u *User) RemoveParent(groupName string, contexts map[string]string) bool {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	for i, existing := range u.Parents {
		if existing.Group == groupName && SameContexts(existing.Contexts, contexts) {
			u.Parents = append(u.Parents[:i], u.Parents[i+1:]...)
			return true
		}
	}
	return false
}

// SetMeta sets a metadata key-value pair.
func (u *User) SetMeta(key, value string) {
	if u.Meta == nil {
		u.Meta = make(map[string]string)
	}
	u.Meta[key] = value
}

// UnsetMeta removes a metadata key-value pair.
func (u *User) UnsetMeta(key string) bool {
	if u.Meta == nil {
		return false
	}
	if _, ok := u.Meta[key]; ok {
		delete(u.Meta, key)
		return true
	}
	return false
}

// AddPrefix adds a prefix with priority.
func (u *User) AddPrefix(pm PriorityMeta) {
	for i, existing := range u.Prefixes {
		if existing.Priority == pm.Priority && SameContexts(existing.Contexts, pm.Contexts) {
			u.Prefixes[i] = pm
			return
		}
	}
	u.Prefixes = append(u.Prefixes, pm)
}

// RemovePrefix removes a prefix matching priority.
func (u *User) RemovePrefix(priority int) bool {
	for i, existing := range u.Prefixes {
		if existing.Priority == priority {
			u.Prefixes = append(u.Prefixes[:i], u.Prefixes[i+1:]...)
			return true
		}
	}
	return false
}

// AddSuffix adds a suffix with priority.
func (u *User) AddSuffix(pm PriorityMeta) {
	for i, existing := range u.Suffixes {
		if existing.Priority == pm.Priority && SameContexts(existing.Contexts, pm.Contexts) {
			u.Suffixes[i] = pm
			return
		}
	}
	u.Suffixes = append(u.Suffixes, pm)
}

// RemoveSuffix removes a suffix matching priority.
func (u *User) RemoveSuffix(priority int) bool {
	for i, existing := range u.Suffixes {
		if existing.Priority == priority {
			u.Suffixes = append(u.Suffixes[:i], u.Suffixes[i+1:]...)
			return true
		}
	}
	return false
}

// CleanExpired removes any temporary permissions, groups, or meta that have expired.
func (u *User) CleanExpired(now time.Time) int {
	cleaned := 0

	// Nodes
	activeNodes := make([]Node, 0, len(u.Nodes))
	for _, n := range u.Nodes {
		if n.IsExpired(now) {
			cleaned++
		} else {
			activeNodes = append(activeNodes, n)
		}
	}
	u.Nodes = activeNodes

	// Parents
	activeParents := make([]Inheritance, 0, len(u.Parents))
	for _, p := range u.Parents {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activeParents = append(activeParents, p)
		}
	}
	u.Parents = activeParents

	// Prefixes
	activePrefixes := make([]PriorityMeta, 0, len(u.Prefixes))
	for _, p := range u.Prefixes {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activePrefixes = append(activePrefixes, p)
		}
	}
	u.Prefixes = activePrefixes

	// Suffixes
	activeSuffixes := make([]PriorityMeta, 0, len(u.Suffixes))
	for _, s := range u.Suffixes {
		if s.IsExpired(now) {
			cleaned++
		} else {
			activeSuffixes = append(activeSuffixes, s)
		}
	}
	u.Suffixes = activeSuffixes

	return cleaned
}

// ClearAll removes all direct nodes, parents, meta, prefixes, and suffixes.
func (u *User) ClearAll() {
	u.Nodes = make([]Node, 0)
	u.Parents = make([]Inheritance, 0)
	u.Meta = make(map[string]string)
	u.Prefixes = make([]PriorityMeta, 0)
	u.Suffixes = make([]PriorityMeta, 0)
	u.PrimaryGroup = "default"
}

// Clone creates a deep copy of the user's permissions and metadata.
func (u *User) Clone(targetUUID uuid.UUID, targetUsername string) *User {
	cloned := NewUser(targetUUID, targetUsername)
	cloned.PrimaryGroup = u.PrimaryGroup
	cloned.Parents = append(cloned.Parents, u.Parents...)
	cloned.Nodes = append(cloned.Nodes, u.Nodes...)
	for k, v := range u.Meta {
		cloned.Meta[k] = v
	}
	cloned.Prefixes = append(cloned.Prefixes, u.Prefixes...)
	cloned.Suffixes = append(cloned.Suffixes, u.Suffixes...)
	return cloned
}
