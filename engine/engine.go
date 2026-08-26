package engine

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andreisugu/gate-stonyperms/model"
)

// CheckResult contains the outcome of a permission evaluation along with explanation.
type CheckResult struct {
	Result model.Tristate
	Source string // e.g. "user:direct", "group:admin (weight 100)", "wildcard:gate.*", "default"
	Node   *model.Node
}

// VerboseFilter controls verbose output filtering.
type VerboseFilter struct {
	Target string // e.g. "player_name" or "" for all
	Query  string // e.g. "gate.*" or "" for all
}

// VerboseLogger callback func to receive verbose check events.
type VerboseLogger func(subject string, permission string, result model.Tristate, source string)

// Engine performs high-performance permission, metadata, and inheritance calculations.
type Engine struct {
	mu            sync.RWMutex
	verboseActive bool
	verboseFilter VerboseFilter
	verboseLog    VerboseLogger
}

// New creates a new permission calculation engine.
func New() *Engine {
	return &Engine{}
}

// SetVerbose enables or disables real-time verbose inspection logging.
func (e *Engine) SetVerbose(active bool, filter VerboseFilter, logger VerboseLogger) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.verboseActive = active
	e.verboseFilter = filter
	e.verboseLog = logger
}

// IsVerboseActive returns true if verbose mode is currently logging.
func (e *Engine) IsVerboseActive() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.verboseActive
}

// PermissionValue evaluates the permission for a user with given groups and context.
func (e *Engine) PermissionValue(user *model.User, groups map[string]*model.Group, targetPerm string, contexts map[string]string) model.Tristate {
	res := e.Evaluate(user, groups, targetPerm, contexts)

	e.mu.RLock()
	if e.verboseActive && e.verboseLog != nil {
		if e.matchesVerboseFilter(user.Username, targetPerm) {
			e.verboseLog(user.Username, targetPerm, res.Result, res.Source)
		}
	}
	e.mu.RUnlock()

	return res.Result
}

// Evaluate runs full permission evaluation and returns the detailed CheckResult.
func (e *Engine) Evaluate(user *model.User, groups map[string]*model.Group, targetPerm string, contexts map[string]string) CheckResult {
	if user == nil {
		return CheckResult{Result: model.Undefined, Source: "none (nil user)"}
	}

	targetPerm = strings.ToLower(strings.TrimSpace(targetPerm))
	now := time.Now()

	// 1. Direct User Exact Match
	for _, node := range user.Nodes {
		if node.IsExpired(now) || !node.MatchesContext(contexts) {
			continue
		}
		if strings.EqualFold(node.Key, targetPerm) {
			val := model.False
			if node.Value {
				val = model.True
			}
			return CheckResult{Result: val, Source: "user:direct", Node: &node}
		}
	}

	// 2. Direct User Wildcards (find the most specific matching wildcard)
	var bestUserWildcard *model.Node
	for _, node := range user.Nodes {
		if node.IsExpired(now) || !node.MatchesContext(contexts) {
			continue
		}
		if node.MatchesPermission(targetPerm) {
			if bestUserWildcard == nil || len(node.Key) > len(bestUserWildcard.Key) {
				n := node
				bestUserWildcard = &n
			}
		}
	}
	if bestUserWildcard != nil {
		val := model.False
		if bestUserWildcard.Value {
			val = model.True
		}
		return CheckResult{
			Result: val,
			Source: "user:wildcard (" + bestUserWildcard.Key + ")",
			Node:   bestUserWildcard,
		}
	}

	// 3. Resolve Inherited Groups (sorted by Weight descending)
	resolvedGroups := e.ResolveInheritedGroups(user, groups, contexts, now)

	// 4. Evaluate Groups in Weight Order
	// First pass: Group Exact Matches
	for _, g := range resolvedGroups {
		for _, node := range g.Nodes {
			if node.IsExpired(now) || !node.MatchesContext(contexts) {
				continue
			}
			if strings.EqualFold(node.Key, targetPerm) {
				val := model.False
				if node.Value {
					val = model.True
				}
				return CheckResult{
					Result: val,
					Source: "group:" + g.Name,
					Node:   &node,
				}
			}
		}
	}

	// Second pass: Group Wildcard Matches
	for _, g := range resolvedGroups {
		var bestGroupWildcard *model.Node
		for _, node := range g.Nodes {
			if node.IsExpired(now) || !node.MatchesContext(contexts) {
				continue
			}
			if node.MatchesPermission(targetPerm) {
				if bestGroupWildcard == nil || len(node.Key) > len(bestGroupWildcard.Key) {
					n := node
					bestGroupWildcard = &n
				}
			}
		}
		if bestGroupWildcard != nil {
			val := model.False
			if bestGroupWildcard.Value {
				val = model.True
			}
			return CheckResult{
				Result: val,
				Source: "group:" + g.Name + ":wildcard (" + bestGroupWildcard.Key + ")",
				Node:   bestGroupWildcard,
			}
		}
	}

	return CheckResult{Result: model.Undefined, Source: "default (undefined)"}
}

// GroupPermissionValue evaluates permission on a group directly (including its parent hierarchy).
func (e *Engine) GroupPermissionValue(group *model.Group, groups map[string]*model.Group, targetPerm string, contexts map[string]string) CheckResult {
	if group == nil {
		return CheckResult{Result: model.Undefined, Source: "none (nil group)"}
	}
	targetPerm = strings.ToLower(strings.TrimSpace(targetPerm))
	now := time.Now()

	resolved := e.ResolveGroupHierarchy(group, groups, contexts, now)

	// Exact matches first
	for _, g := range resolved {
		for _, node := range g.Nodes {
			if node.IsExpired(now) || !node.MatchesContext(contexts) {
				continue
			}
			if strings.EqualFold(node.Key, targetPerm) {
				val := model.False
				if node.Value {
					val = model.True
				}
				return CheckResult{
					Result: val,
					Source: "group:" + g.Name,
					Node:   &node,
				}
			}
		}
	}

	// Wildcard matches second
	for _, g := range resolved {
		var best *model.Node
		for _, node := range g.Nodes {
			if node.IsExpired(now) || !node.MatchesContext(contexts) {
				continue
			}
			if node.MatchesPermission(targetPerm) {
				if best == nil || len(node.Key) > len(best.Key) {
					n := node
					best = &n
				}
			}
		}
		if best != nil {
			val := model.False
			if best.Value {
				val = model.True
			}
			return CheckResult{
				Result: val,
				Source: "group:" + g.Name + ":wildcard (" + best.Key + ")",
				Node:   best,
			}
		}
	}

	return CheckResult{Result: model.Undefined, Source: "default (undefined)"}
}

// ResolveInheritedGroups builds an ordered, deduplicated slice of groups inherited by the user (weight descending).
func (e *Engine) ResolveInheritedGroups(user *model.User, groups map[string]*model.Group, contexts map[string]string, now time.Time) []*model.Group {
	visited := make(map[string]bool)
	var result []*model.Group

	var visit func(groupName string)
	visit = func(groupName string) {
		groupName = strings.ToLower(strings.TrimSpace(groupName))
		if groupName == "" || visited[groupName] {
			return
		}
		visited[groupName] = true

		g, ok := groups[groupName]
		if !ok || g == nil {
			return
		}
		result = append(result, g)

		for _, p := range g.Parents {
			if !p.IsExpired(now) && p.MatchesContext(contexts) {
				visit(p.Group)
			}
		}
	}

	// User direct parents
	for _, p := range user.Parents {
		if !p.IsExpired(now) && p.MatchesContext(contexts) {
			visit(p.Group)
		}
	}

	// Primary group fallback if not visited
	if user.PrimaryGroup != "" {
		visit(user.PrimaryGroup)
	} else {
		visit("default")
	}

	// Sort groups by Weight descending
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Weight > result[j].Weight
	})

	return result
}

// ResolveGroupHierarchy builds ordered slice of a group and its recursive parent groups.
func (e *Engine) ResolveGroupHierarchy(root *model.Group, groups map[string]*model.Group, contexts map[string]string, now time.Time) []*model.Group {
	visited := make(map[string]bool)
	var result []*model.Group

	var visit func(g *model.Group)
	visit = func(g *model.Group) {
		if g == nil {
			return
		}
		name := strings.ToLower(g.Name)
		if visited[name] {
			return
		}
		visited[name] = true
		result = append(result, g)

		for _, p := range g.Parents {
			if !p.IsExpired(now) && p.MatchesContext(contexts) {
				if parentGroup, ok := groups[strings.ToLower(p.Group)]; ok {
					visit(parentGroup)
				}
			}
		}
	}

	visit(root)

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Weight > result[j].Weight
	})

	return result
}

// GetPrefix resolves the highest priority active prefix for a user.
func (e *Engine) GetPrefix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	now := time.Now()
	var highest *model.PriorityMeta

	// Check direct user prefixes
	for _, p := range user.Prefixes {
		if p.IsExpired(now) || !p.MatchesContext(contexts) {
			continue
		}
		if highest == nil || p.Priority > highest.Priority {
			curr := p
			highest = &curr
		}
	}

	// Check group prefixes
	resolved := e.ResolveInheritedGroups(user, groups, contexts, now)
	for _, g := range resolved {
		for _, p := range g.Prefixes {
			if p.IsExpired(now) || !p.MatchesContext(contexts) {
				continue
			}
			if highest == nil || p.Priority > highest.Priority {
				curr := p
				highest = &curr
			}
		}
	}

	if highest != nil {
		return highest.Value
	}
	return ""
}

// GetSuffix resolves the highest priority active suffix for a user.
func (e *Engine) GetSuffix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	now := time.Now()
	var highest *model.PriorityMeta

	// Check direct user suffixes
	for _, s := range user.Suffixes {
		if s.IsExpired(now) || !s.MatchesContext(contexts) {
			continue
		}
		if highest == nil || s.Priority > highest.Priority {
			curr := s
			highest = &curr
		}
	}

	// Check group suffixes
	resolved := e.ResolveInheritedGroups(user, groups, contexts, now)
	for _, g := range resolved {
		for _, s := range g.Suffixes {
			if s.IsExpired(now) || !s.MatchesContext(contexts) {
				continue
			}
			if highest == nil || s.Priority > highest.Priority {
				curr := s
				highest = &curr
			}
		}
	}

	if highest != nil {
		return highest.Value
	}
	return ""
}

// GetMeta resolves a custom metadata key for a user (direct overrides group).
func (e *Engine) GetMeta(user *model.User, groups map[string]*model.Group, key string) (string, bool) {
	if val, ok := user.Meta[key]; ok {
		return val, true
	}
	resolved := e.ResolveInheritedGroups(user, groups, nil, time.Now())
	for _, g := range resolved {
		if val, ok := g.Meta[key]; ok {
			return val, true
		}
	}
	return "", false
}

func (e *Engine) matchesVerboseFilter(subject, perm string) bool {
	if e.verboseFilter.Target != "" && !strings.EqualFold(e.verboseFilter.Target, subject) {
		return false
	}
	if e.verboseFilter.Query != "" {
		node := model.Node{Key: e.verboseFilter.Query}
		if !node.MatchesPermission(perm) && !strings.Contains(strings.ToLower(perm), strings.ToLower(e.verboseFilter.Query)) {
			return false
		}
	}
	return true
}
