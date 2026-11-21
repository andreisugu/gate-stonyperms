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

const maxVerboseEntries = 2000

// VerboseEntry captures a single evaluated permission check during verbose recording.
type VerboseEntry struct {
	Subject    string `json:"subject"`
	Permission string `json:"permission"`
	Result     string `json:"result"`
	Source     string `json:"source,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// VerboseRecording captures a session of recorded permission checks.
type VerboseRecording struct {
	Active     bool           `json:"active"`
	Actor      string         `json:"actor"`
	Filter     VerboseFilter  `json:"filter"`
	StartTime  int64          `json:"startTime"`
	EndTime    int64          `json:"endTime"`
	TotalCount int            `json:"totalCount"`
	Truncated  bool           `json:"truncated"`
	Entries    []VerboseEntry `json:"entries"`
}

// Engine performs high-performance permission, metadata, and inheritance calculations.
type Engine struct {
	mu            sync.RWMutex
	verboseActive bool
	verboseFilter VerboseFilter
	verboseLog    VerboseLogger
	recordActive  bool
	recordActor   string
	recordFilter  VerboseFilter
	recordStart   int64
	recordEnd     int64
	recordTotal   int
	recordTrunc   bool
	recordEntries []VerboseEntry
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

// StartVerboseRecording begins capturing evaluated permission checks for web playback.
func (e *Engine) StartVerboseRecording(actor string, filter VerboseFilter) {
	nowMs := time.Now().UTC().UnixMilli()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordActive = true
	e.recordActor = actor
	e.recordFilter = filter
	e.recordStart = nowMs
	e.recordEnd = nowMs
	e.recordTotal = 0
	e.recordTrunc = false
	e.recordEntries = nil
}

// StopVerboseRecording stops active recording while preserving captured entries for upload.
func (e *Engine) StopVerboseRecording() VerboseRecording {
	nowMs := time.Now().UTC().UnixMilli()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.recordActive {
		e.recordActive = false
		if nowMs > e.recordEnd {
			e.recordEnd = nowMs
		}
	}
	return e.snapshotRecordingLocked()
}

// SnapshotVerboseRecording returns a copy of the current or most recent verbose recording.
func (e *Engine) SnapshotVerboseRecording() VerboseRecording {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapshotRecordingLocked()
}

// IsVerboseRecording reports whether background verbose check recording is active.
func (e *Engine) IsVerboseRecording() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.recordActive
}

func (e *Engine) snapshotRecordingLocked() VerboseRecording {
	entries := make([]VerboseEntry, len(e.recordEntries))
	copy(entries, e.recordEntries)
	return VerboseRecording{
		Active:     e.recordActive,
		Actor:      e.recordActor,
		Filter:     e.recordFilter,
		StartTime:  e.recordStart,
		EndTime:    e.recordEnd,
		TotalCount: e.recordTotal,
		Truncated:  e.recordTrunc,
		Entries:    entries,
	}
}

func matchesFilter(f VerboseFilter, subject, perm string) bool {
	if f.Target != "" && !strings.EqualFold(f.Target, subject) {
		return false
	}
	if f.Query != "" {
		node := model.Node{Key: f.Query}
		if !node.MatchesPermission(perm) && !strings.Contains(strings.ToLower(perm), strings.ToLower(f.Query)) {
			return false
		}
	}
	return true
}

// PermissionValue evaluates the permission for a user with given groups and context.
func (e *Engine) PermissionValue(user *model.User, groups map[string]*model.Group, targetPerm string, contexts map[string]string) model.Tristate {
	res := e.Evaluate(user, groups, targetPerm, contexts)

	e.mu.RLock()
	needChat := e.verboseActive && e.verboseLog != nil && matchesFilter(e.verboseFilter, user.Username, targetPerm)
	chatLogger := e.verboseLog
	needRecord := e.recordActive && matchesFilter(e.recordFilter, user.Username, targetPerm)
	e.mu.RUnlock()

	if needChat {
		chatLogger(user.Username, targetPerm, res.Result, res.Source)
	}
	if needRecord {
		nowMs := time.Now().UTC().UnixMilli()
		entry := VerboseEntry{
			Subject:    user.Username,
			Permission: targetPerm,
			Result:     strings.ToLower(res.Result.String()),
			Source:     res.Source,
			Timestamp:  nowMs,
		}
		e.mu.Lock()
		if e.recordActive && matchesFilter(e.recordFilter, user.Username, targetPerm) {
			e.recordTotal++
			if nowMs > e.recordEnd {
				e.recordEnd = nowMs
			}
			if len(e.recordEntries) < maxVerboseEntries {
				e.recordEntries = append(e.recordEntries, entry)
			} else {
				e.recordTrunc = true
			}
		}
		e.mu.Unlock()
	}

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

	// 4. Evaluate Groups in Weight Order (Higher weight group completely overrides lower weight group)
	for _, g := range resolvedGroups {
		// 4a. Check exact match in group g
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

		// 4b. Check best wildcard match in group g
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

	// Evaluate in weight descending order
	for _, g := range resolved {
		// Exact match first within group
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

		// Wildcard match second within group
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

// GetPrefix resolves the highest priority active prefix for a user,
// giving precedence to direct user prefixes, then the user's explicit non-default
// primary group, and finally inherited groups.
func (e *Engine) GetPrefix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	if user == nil {
		return ""
	}
	return e.resolveAffix(user, groups, contexts, user.Prefixes, func(g *model.Group) []model.PriorityMeta {
		return g.Prefixes
	})
}

// GetSuffix resolves the highest priority active suffix for a user,
// giving precedence to direct user suffixes, then the user's explicit non-default
// primary group, and finally inherited groups.
func (e *Engine) GetSuffix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	if user == nil {
		return ""
	}
	return e.resolveAffix(user, groups, contexts, user.Suffixes, func(g *model.Group) []model.PriorityMeta {
		return g.Suffixes
	})
}

func highestActiveAffix(items []model.PriorityMeta, contexts map[string]string, now time.Time) *model.PriorityMeta {
	var highest *model.PriorityMeta
	for _, item := range items {
		if item.IsExpired(now) || !item.MatchesContext(contexts) {
			continue
		}
		if highest == nil || item.Priority > highest.Priority {
			curr := item
			highest = &curr
		}
	}
	return highest
}

func (e *Engine) resolveAffix(
	user *model.User,
	groups map[string]*model.Group,
	contexts map[string]string,
	direct []model.PriorityMeta,
	groupAffixes func(*model.Group) []model.PriorityMeta,
) string {
	now := time.Now()
	if best := highestActiveAffix(direct, contexts, now); best != nil {
		return best.Value
	}

	primary := strings.ToLower(strings.TrimSpace(user.PrimaryGroup))
	if primary != "" && primary != "default" {
		if pg, ok := groups[primary]; ok && pg != nil {
			if best := highestActiveAffix(groupAffixes(pg), contexts, now); best != nil {
				return best.Value
			}
		}
	}

	var highest *model.PriorityMeta
	for _, g := range e.ResolveInheritedGroups(user, groups, contexts, now) {
		candidate := highestActiveAffix(groupAffixes(g), contexts, now)
		if candidate != nil && (highest == nil || candidate.Priority > highest.Priority) {
			highest = candidate
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
