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
	Prefixes    []PriorityMeta    `json:"prefixes,omitempty"`
	Suffixes    []PriorityMeta    `json:"suffixes,omitempty"`
}

// NewGroup creates a new group initialized with clean defaults.
func NewGroup(name string, weight int) *Group {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	return &Group{
		Name:     cleanName,
		Weight:   weight,
		Parents:  make([]Inheritance, 0),
		Nodes:    make([]Node, 0),
		Meta:     make(map[string]string),
		Prefixes: make([]PriorityMeta, 0),
		Suffixes: make([]PriorityMeta, 0),
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
	node.Key = strings.ToLower(strings.TrimSpace(node.Key))
	for i, existing := range g.Nodes {
		if existing.Key == node.Key && SameContexts(existing.Contexts, node.Contexts) {
			g.Nodes[i] = node
			return
		}
	}
	g.Nodes = append(g.Nodes, node)
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

// AddParent adds an inheritance relationship to a parent group.
func (g *Group) AddParent(inh Inheritance) {
	inh.Group = strings.ToLower(strings.TrimSpace(inh.Group))
	for i, existing := range g.Parents {
		if existing.Group == inh.Group && SameContexts(existing.Contexts, inh.Contexts) {
			g.Parents[i] = inh
			return
		}
	}
	g.Parents = append(g.Parents, inh)
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

// SetMeta sets a key-value metadata entry.
func (g *Group) SetMeta(key, value string) {
	if g.Meta == nil {
		g.Meta = make(map[string]string)
	}
	g.Meta[key] = value
}

// UnsetMeta removes a key-value metadata entry.
func (g *Group) UnsetMeta(key string) bool {
	if g.Meta == nil {
		return false
	}
	if _, ok := g.Meta[key]; ok {
		delete(g.Meta, key)
		return true
	}
	return false
}

// AddPrefix adds a prefix with priority.
func (g *Group) AddPrefix(pm PriorityMeta) {
	for i, existing := range g.Prefixes {
		if existing.Priority == pm.Priority && SameContexts(existing.Contexts, pm.Contexts) {
			g.Prefixes[i] = pm
			return
		}
	}
	g.Prefixes = append(g.Prefixes, pm)
}

// RemovePrefix removes a prefix matching priority.
func (g *Group) RemovePrefix(priority int) bool {
	for i, existing := range g.Prefixes {
		if existing.Priority == priority {
			g.Prefixes = append(g.Prefixes[:i], g.Prefixes[i+1:]...)
			return true
		}
	}
	return false
}

// AddSuffix adds a suffix with priority.
func (g *Group) AddSuffix(pm PriorityMeta) {
	for i, existing := range g.Suffixes {
		if existing.Priority == pm.Priority && SameContexts(existing.Contexts, pm.Contexts) {
			g.Suffixes[i] = pm
			return
		}
	}
	g.Suffixes = append(g.Suffixes, pm)
}

// RemoveSuffix removes a suffix matching priority.
func (g *Group) RemoveSuffix(priority int) bool {
	for i, existing := range g.Suffixes {
		if existing.Priority == priority {
			g.Suffixes = append(g.Suffixes[:i], g.Suffixes[i+1:]...)
			return true
		}
	}
	return false
}

// CleanExpired removes any temporary nodes, parents, prefixes, or suffixes that have expired.
func (g *Group) CleanExpired(now time.Time) int {
	cleaned := 0

	// Nodes
	activeNodes := make([]Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.IsExpired(now) {
			cleaned++
		} else {
			activeNodes = append(activeNodes, n)
		}
	}
	g.Nodes = activeNodes

	// Parents
	activeParents := make([]Inheritance, 0, len(g.Parents))
	for _, p := range g.Parents {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activeParents = append(activeParents, p)
		}
	}
	g.Parents = activeParents

	// Prefixes
	activePrefixes := make([]PriorityMeta, 0, len(g.Prefixes))
	for _, p := range g.Prefixes {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activePrefixes = append(activePrefixes, p)
		}
	}
	g.Prefixes = activePrefixes

	// Suffixes
	activeSuffixes := make([]PriorityMeta, 0, len(g.Suffixes))
	for _, s := range g.Suffixes {
		if s.IsExpired(now) {
			cleaned++
		} else {
			activeSuffixes = append(activeSuffixes, s)
		}
	}
	g.Suffixes = activeSuffixes

	return cleaned
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
