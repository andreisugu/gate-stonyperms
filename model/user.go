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
	MetaEntries  []MetaEntry       `json:"metaEntries,omitempty"`
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
		MetaEntries:  make([]MetaEntry, 0),
		Prefixes:     make([]PriorityMeta, 0),
		Suffixes:     make([]PriorityMeta, 0),
	}
}

// AddNode adds or updates a permission node for this user.
func (u *User) AddNode(node Node) {
	_, _ = u.AddNodeWithModifier(node, TempModifierReplace, time.Now())
}

// AddNodeWithModifier adds or updates a permission node respecting temporary modifiers.
func (u *User) AddNodeWithModifier(node Node, mod TempModifier, now time.Time) (Node, error) {
	return addNodeWithModifier(&u.Nodes, node, mod, now)
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

// RemoveTempNode removes a temporary permission node by key and context.
func (u *User) RemoveTempNode(key string, contexts map[string]string) bool {
	return removeTempNode(&u.Nodes, key, contexts)
}

// AddParent adds an inherited parent group.
func (u *User) AddParent(inh Inheritance) {
	_, _ = u.AddParentWithModifier(inh, TempModifierReplace, time.Now())
}

// AddParentWithModifier adds an inherited parent group respecting temporary modifiers.
func (u *User) AddParentWithModifier(inh Inheritance, mod TempModifier, now time.Time) (Inheritance, error) {
	return addParentWithModifier(&u.Parents, inh, mod, now)
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

// RemoveTempParent removes a temporary parent group inheritance.
func (u *User) RemoveTempParent(groupName string, contexts map[string]string) bool {
	return removeTempParent(&u.Parents, groupName, contexts)
}

// SetMeta sets a metadata key-value pair.
func (u *User) SetMeta(key, value string) {
	_, _ = u.SetMetaEntry(MetaEntry{Key: key, Value: value}, TempModifierReplace, time.Now())
}

// SetMetaEntry sets a permanent, temporary, or contextual metadata entry.
func (u *User) SetMetaEntry(entry MetaEntry, mod TempModifier, now time.Time) (MetaEntry, error) {
	return setMetaEntry(&u.Meta, &u.MetaEntries, entry, mod, now)
}

// UnsetMeta removes a metadata key-value pair.
func (u *User) UnsetMeta(key string) bool {
	return u.UnsetMetaEntry(key, nil, false)
}

// UnsetMetaEntry removes metadata entries matching key, contexts, and tempOnly filter.
func (u *User) UnsetMetaEntry(key string, contexts map[string]string, tempOnly bool) bool {
	return unsetMetaEntry(&u.Meta, &u.MetaEntries, key, contexts, tempOnly)
}

// AddPrefix adds a prefix with priority.
func (u *User) AddPrefix(pm PriorityMeta) {
	_, _ = u.AddPrefixWithModifier(pm, TempModifierReplace, time.Now())
}

// AddPrefixWithModifier adds a prefix respecting temporary modifiers.
func (u *User) AddPrefixWithModifier(pm PriorityMeta, mod TempModifier, now time.Time) (PriorityMeta, error) {
	return addAffixWithModifier(&u.Prefixes, pm, mod, now)
}

// SetPrefixInContext replaces prefixes sharing pm.Contexts before adding pm.
func (u *User) SetPrefixInContext(pm PriorityMeta) {
	setAffixInContext(&u.Prefixes, pm)
}

// RemovePrefix removes a prefix matching priority.
func (u *User) RemovePrefix(priority int) bool {
	return u.RemovePrefixMatch(priority, "", nil, false)
}

// RemovePrefixMatch removes prefixes matching priority, optional value, optional contexts, and tempOnly.
func (u *User) RemovePrefixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool {
	return removeAffixMatch(&u.Prefixes, priority, value, contexts, tempOnly)
}

// AddSuffix adds a suffix with priority.
func (u *User) AddSuffix(pm PriorityMeta) {
	_, _ = u.AddSuffixWithModifier(pm, TempModifierReplace, time.Now())
}

// AddSuffixWithModifier adds a suffix respecting temporary modifiers.
func (u *User) AddSuffixWithModifier(pm PriorityMeta, mod TempModifier, now time.Time) (PriorityMeta, error) {
	return addAffixWithModifier(&u.Suffixes, pm, mod, now)
}

// SetSuffixInContext replaces suffixes sharing pm.Contexts before adding pm.
func (u *User) SetSuffixInContext(pm PriorityMeta) {
	setAffixInContext(&u.Suffixes, pm)
}

// RemoveSuffix removes a suffix matching priority.
func (u *User) RemoveSuffix(priority int) bool {
	return u.RemoveSuffixMatch(priority, "", nil, false)
}

// RemoveSuffixMatch removes suffixes matching priority, optional value, optional contexts, and tempOnly.
func (u *User) RemoveSuffixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool {
	return removeAffixMatch(&u.Suffixes, priority, value, contexts, tempOnly)
}

// CleanExpired removes any temporary permissions, groups, or meta that have expired.
func (u *User) CleanExpired(now time.Time) int {
	return cleanExpiredCommon(&u.Nodes, &u.Parents, &u.Prefixes, &u.Suffixes, &u.MetaEntries, now)
}

// ClearAll removes all direct nodes, parents, meta, prefixes, and suffixes.
func (u *User) ClearAll() {
	u.Nodes = make([]Node, 0)
	u.Parents = make([]Inheritance, 0)
	u.Meta = make(map[string]string)
	u.MetaEntries = make([]MetaEntry, 0)
	u.Prefixes = make([]PriorityMeta, 0)
	u.Suffixes = make([]PriorityMeta, 0)
	u.PrimaryGroup = "default"
}

// Clone creates a deep copy of the user's permissions and metadata.
func (u *User) Clone(targetUUID uuid.UUID, targetUsername string) *User {
	cloned := NewUser(targetUUID, targetUsername)
	cloned.PrimaryGroup = u.PrimaryGroup
	cloned.Parents = cloneInheritances(u.Parents)
	cloned.Nodes = cloneNodes(u.Nodes)
	cloned.Meta = cloneStringMap(u.Meta)
	cloned.MetaEntries = cloneMetaEntries(u.MetaEntries)
	cloned.Prefixes = clonePriorityMetas(u.Prefixes)
	cloned.Suffixes = clonePriorityMetas(u.Suffixes)
	return cloned
}
