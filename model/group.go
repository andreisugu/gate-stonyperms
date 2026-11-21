package model

import (
	"strings"
	"time"
)

// Group represents a permission group with inheritance, weighted priority, and metadata.
type Group struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName,omitempty"`
	Weight      int               `json:"weight"`
	Parents     []Inheritance     `json:"parents,omitempty"`
	Nodes       []Node            `json:"nodes,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	MetaEntries []MetaEntry       `json:"metaEntries,omitempty"`
	Prefixes    []PriorityMeta    `json:"prefixes,omitempty"`
	Suffixes    []PriorityMeta    `json:"suffixes,omitempty"`
}

// NewGroup creates a new group initialized with clean defaults.
func NewGroup(name string, weight int) *Group {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	return &Group{
		Name:        cleanName,
		Weight:      weight,
		Parents:     make([]Inheritance, 0),
		Nodes:       make([]Node, 0),
		Meta:        make(map[string]string),
		MetaEntries: make([]MetaEntry, 0),
		Prefixes:    make([]PriorityMeta, 0),
		Suffixes:    make([]PriorityMeta, 0),
	}
}

// GetDisplayName returns the display name if set, or the group name otherwise.
func (g *Group) GetDisplayName() string {
	if g.DisplayName != "" {
		return g.DisplayName
	}
	return g.Name
}

// AddNode adds or updates a permission node in this group.
func (g *Group) AddNode(node Node) {
	_, _ = g.AddNodeWithModifier(node, TempModifierReplace, time.Now())
}

// AddNodeWithModifier adds or updates a group node respecting temporary modifiers.
func (g *Group) AddNodeWithModifier(node Node, mod TempModifier, now time.Time) (Node, error) {
	return addNodeWithModifier(&g.Nodes, node, mod, now)
}

// RemoveNode removes a permission node by key and contexts. Returns true if removed.
func (g *Group) RemoveNode(key string, contexts map[string]string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for i, existing := range g.Nodes {
		if existing.Key == key && SameContexts(existing.Contexts, contexts) {
			g.Nodes = append(g.Nodes[:i], g.Nodes[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveTempNode removes a temporary permission node by key and contexts.
func (g *Group) RemoveTempNode(key string, contexts map[string]string) bool {
	return removeTempNode(&g.Nodes, key, contexts)
}

// AddParent adds an inheritance relationship to a parent group.
func (g *Group) AddParent(inh Inheritance) {
	_, _ = g.AddParentWithModifier(inh, TempModifierReplace, time.Now())
}

// AddParentWithModifier adds a parent inheritance relationship respecting temporary modifiers.
func (g *Group) AddParentWithModifier(inh Inheritance, mod TempModifier, now time.Time) (Inheritance, error) {
	return addParentWithModifier(&g.Parents, inh, mod, now)
}

// RemoveParent removes a parent inheritance relationship.
func (g *Group) RemoveParent(groupName string, contexts map[string]string) bool {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	for i, existing := range g.Parents {
		if existing.Group == groupName && SameContexts(existing.Contexts, contexts) {
			g.Parents = append(g.Parents[:i], g.Parents[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveTempParent removes a temporary parent inheritance relationship.
func (g *Group) RemoveTempParent(groupName string, contexts map[string]string) bool {
	return removeTempParent(&g.Parents, groupName, contexts)
}

// SetMeta sets a key-value metadata entry.
func (g *Group) SetMeta(key, value string) {
	_, _ = g.SetMetaEntry(MetaEntry{Key: key, Value: value}, TempModifierReplace, time.Now())
}

// SetMetaEntry sets a permanent, temporary, or contextual metadata entry.
func (g *Group) SetMetaEntry(entry MetaEntry, mod TempModifier, now time.Time) (MetaEntry, error) {
	return setMetaEntry(&g.Meta, &g.MetaEntries, entry, mod, now)
}

// UnsetMeta removes a key-value metadata entry.
func (g *Group) UnsetMeta(key string) bool {
	return g.UnsetMetaEntry(key, nil, false)
}

// UnsetMetaEntry removes metadata entries matching key, contexts, and tempOnly filter.
func (g *Group) UnsetMetaEntry(key string, contexts map[string]string, tempOnly bool) bool {
	return unsetMetaEntry(&g.Meta, &g.MetaEntries, key, contexts, tempOnly)
}

// AddPrefix adds a prefix with priority.
func (g *Group) AddPrefix(pm PriorityMeta) {
	_, _ = g.AddPrefixWithModifier(pm, TempModifierReplace, time.Now())
}

// AddPrefixWithModifier adds a prefix respecting temporary modifiers.
func (g *Group) AddPrefixWithModifier(pm PriorityMeta, mod TempModifier, now time.Time) (PriorityMeta, error) {
	return addAffixWithModifier(&g.Prefixes, pm, mod, now)
}

// SetPrefixInContext replaces prefixes sharing pm.Contexts before adding pm.
func (g *Group) SetPrefixInContext(pm PriorityMeta) {
	setAffixInContext(&g.Prefixes, pm)
}

// RemovePrefix removes a prefix matching priority.
func (g *Group) RemovePrefix(priority int) bool {
	return g.RemovePrefixMatch(priority, "", nil, false)
}

// RemovePrefixMatch removes prefixes matching priority, optional value, optional contexts, and tempOnly.
func (g *Group) RemovePrefixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool {
	return removeAffixMatch(&g.Prefixes, priority, value, contexts, tempOnly)
}

// AddSuffix adds a suffix with priority.
func (g *Group) AddSuffix(pm PriorityMeta) {
	_, _ = g.AddSuffixWithModifier(pm, TempModifierReplace, time.Now())
}

// AddSuffixWithModifier adds a suffix respecting temporary modifiers.
func (g *Group) AddSuffixWithModifier(pm PriorityMeta, mod TempModifier, now time.Time) (PriorityMeta, error) {
	return addAffixWithModifier(&g.Suffixes, pm, mod, now)
}

// SetSuffixInContext replaces suffixes sharing pm.Contexts before adding pm.
func (g *Group) SetSuffixInContext(pm PriorityMeta) {
	setAffixInContext(&g.Suffixes, pm)
}

// RemoveSuffix removes a suffix matching priority.
func (g *Group) RemoveSuffix(priority int) bool {
	return g.RemoveSuffixMatch(priority, "", nil, false)
}

// RemoveSuffixMatch removes suffixes matching priority, optional value, optional contexts, and tempOnly.
func (g *Group) RemoveSuffixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool {
	return removeAffixMatch(&g.Suffixes, priority, value, contexts, tempOnly)
}

// CleanExpired removes any temporary nodes, parents, prefixes, suffixes, or meta entries that have expired.
func (g *Group) CleanExpired(now time.Time) int {
	return cleanExpiredCommon(&g.Nodes, &g.Parents, &g.Prefixes, &g.Suffixes, &g.MetaEntries, now)
}

// Clone creates a deep copy of the group with a new name.
func (g *Group) Clone(newName string) *Group {
	cloned := NewGroup(newName, g.Weight)
	cloned.DisplayName = g.DisplayName
	cloned.Parents = append(cloned.Parents, g.Parents...)
	cloned.Nodes = append(cloned.Nodes, g.Nodes...)
	for k, v := range g.Meta {
		cloned.Meta[k] = v
	}
	cloned.MetaEntries = cloneMetaEntries(g.MetaEntries)
	cloned.Prefixes = append(cloned.Prefixes, g.Prefixes...)
	cloned.Suffixes = append(cloned.Suffixes, g.Suffixes...)
	return cloned
}

// SameContexts compares two context maps for equality (case-insensitive values).
func SameContexts(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !strings.EqualFold(v, bv) {
			return false
		}
	}
	return true
}
