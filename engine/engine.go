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

// StackConfig configures priority prefix and suffix stacking.
type StackConfig struct {
	PrefixEnabled     bool
	PrefixStartSpacer string
	PrefixMidSpacer   string
	PrefixEndSpacer   string
	SuffixEnabled     bool
	SuffixStartSpacer string
	SuffixMidSpacer   string
	SuffixEndSpacer   string
	Deduplicate       bool
}

// Engine performs high-performance permission, metadata, and inheritance calculations.
type Engine struct {
	mu            sync.RWMutex
	stackCfg      StackConfig
	primaryCalc   string
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
	return &Engine{
		stackCfg:    StackConfig{Deduplicate: true},
		primaryCalc: "stored",
	}
}

// SetPrimaryGroupCalculation sets how primary groups are computed:
// "stored" (default), "parents-by-weight", or "all-parents-by-weight".
func (e *Engine) SetPrimaryGroupCalculation(mode string) {
	clean := strings.ToLower(strings.TrimSpace(mode))
	switch clean {
	case "parents-by-weight", "all-parents-by-weight":
	default:
		clean = "stored"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.primaryCalc = clean
}

// GetPrimaryGroupCalculation returns the active primary group calculation mode.
func (e *Engine) GetPrimaryGroupCalculation() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.primaryCalc == "" {
		return "stored"
	}
	return e.primaryCalc
}

// ResolvePrimaryGroup determines a user's effective primary group using the active calculation mode.
func (e *Engine) ResolvePrimaryGroup(user *model.User, groups map[string]*model.Group, contexts map[string]string, defaultGroup string) string {
	if defaultGroup == "" {
		defaultGroup = "default"
	}
	if user == nil {
		return defaultGroup
	}
	mode := e.GetPrimaryGroupCalculation()
	now := time.Now()
	if mode == "parents-by-weight" {
		var bestGroup *model.Group
		for _, p := range user.Parents {
			if p.IsExpired(now) || !p.MatchesContext(contexts) {
				continue
			}
			if g, ok := groups[strings.ToLower(strings.TrimSpace(p.Group))]; ok && g != nil {
				if bestGroup == nil || g.Weight > bestGroup.Weight {
					bestGroup = g
				}
			}
		}
		if bestGroup != nil {
			return bestGroup.Name
		}
	} else if mode == "all-parents-by-weight" {
		resolved := e.ResolveInheritedGroups(user, groups, contexts, now)
		if len(resolved) > 0 {
			return resolved[0].Name
		}
	}
	primary := strings.ToLower(strings.TrimSpace(user.PrimaryGroup))
	if primary == "" {
		return defaultGroup
	}
	return primary
}

// SetStackConfig updates prefix/suffix stacking behavior.
func (e *Engine) SetStackConfig(cfg StackConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stackCfg = cfg
}

// GetStackConfig returns the current prefix/suffix stacking configuration.
func (e *Engine) GetStackConfig() StackConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stackCfg
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

	// 1 & 2. Direct User Nodes (exact > wildcard/regex, breaking ties by context specificity)
	if node, exact := selectBestNode(user.Nodes, targetPerm, contexts, now); node != nil {
		val := model.False
		if node.Value {
			val = model.True
		}
		src := "user:direct"
		if !exact {
			src = "user:wildcard (" + node.Key + ")"
		}
		return CheckResult{Result: val, Source: src, Node: node}
	}

	// 3. Resolve Inherited Groups (sorted by Weight descending)
	resolvedGroups := e.ResolveInheritedGroups(user, groups, contexts, now)

	// 4. Evaluate Groups in Weight Order (exact > wildcard/regex, higher context specificity wins)
	for _, g := range resolvedGroups {
		if node, exact := selectBestNode(g.Nodes, targetPerm, contexts, now); node != nil {
			val := model.False
			if node.Value {
				val = model.True
			}
			src := "group:" + g.Name
			if !exact {
				src = "group:" + g.Name + ":wildcard (" + node.Key + ")"
			}
			return CheckResult{Result: val, Source: src, Node: node}
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
		if node, exact := selectBestNode(g.Nodes, targetPerm, contexts, now); node != nil {
			val := model.False
			if node.Value {
				val = model.True
			}
			src := "group:" + g.Name
			if !exact {
				src = "group:" + g.Name + ":wildcard (" + node.Key + ")"
			}
			return CheckResult{Result: val, Source: src, Node: node}
		}
	}

	return CheckResult{Result: model.Undefined, Source: "default (undefined)"}
}

func selectBestNode(nodes []model.Node, targetPerm string, contexts map[string]string, now time.Time) (*model.Node, bool) {
	var best *model.Node
	bestExact := false
	bestSpec := 0
	for _, n := range nodes {
		if n.IsExpired(now) || !n.MatchesContext(contexts) {
			continue
		}
		matched, exact, spec := model.EvaluateNodeMatch(n.Key, targetPerm)
		if !matched {
			continue
		}
		better := false
		if best == nil {
			better = true
		} else if exact != bestExact {
			better = exact
		} else if exact {
			if len(n.Contexts) != len(best.Contexts) {
				better = len(n.Contexts) > len(best.Contexts)
			} else if spec != bestSpec {
				better = spec > bestSpec
			} else if !n.Value && best.Value {
				better = true
			}
		} else {
			if spec != bestSpec {
				better = spec > bestSpec
			} else if len(n.Contexts) != len(best.Contexts) {
				better = len(n.Contexts) > len(best.Contexts)
			} else if !n.Value && best.Value {
				better = true
			}
		}
		if better {
			cpy := n
			best = &cpy
			bestExact = exact
			bestSpec = spec
		}
	}
	return best, bestExact
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

// GetPrefix resolves the user's prefix, using prefix stacking if enabled or
// primary-group priority precedence otherwise.
func (e *Engine) GetPrefix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	if user == nil {
		return ""
	}
	stackCfg := e.GetStackConfig()
	if stackCfg.PrefixEnabled {
		if stacked := e.GetStackedPrefix(user, groups, contexts); stacked != "" {
			return stacked
		}
	}
	return e.resolveAffix(user, groups, contexts, user.Prefixes, func(g *model.Group) []model.PriorityMeta {
		return g.Prefixes
	})
}

// GetSuffix resolves the user's suffix, using suffix stacking if enabled or
// primary-group priority precedence otherwise.
func (e *Engine) GetSuffix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	if user == nil {
		return ""
	}
	stackCfg := e.GetStackConfig()
	if stackCfg.SuffixEnabled {
		if stacked := e.GetStackedSuffix(user, groups, contexts); stacked != "" {
			return stacked
		}
	}
	return e.resolveAffix(user, groups, contexts, user.Suffixes, func(g *model.Group) []model.PriorityMeta {
		return g.Suffixes
	})
}

// GetStackedPrefix formats all active context-matching prefixes ordered by priority descending.
func (e *Engine) GetStackedPrefix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	stackCfg := e.GetStackConfig()
	return e.formatStackedAffix(
		user, groups, contexts,
		user.Prefixes, func(g *model.Group) []model.PriorityMeta { return g.Prefixes },
		stackCfg.PrefixStartSpacer, stackCfg.PrefixMidSpacer, stackCfg.PrefixEndSpacer, stackCfg.Deduplicate,
	)
}

// GetStackedSuffix formats all active context-matching suffixes ordered by priority descending.
func (e *Engine) GetStackedSuffix(user *model.User, groups map[string]*model.Group, contexts map[string]string) string {
	stackCfg := e.GetStackConfig()
	return e.formatStackedAffix(
		user, groups, contexts,
		user.Suffixes, func(g *model.Group) []model.PriorityMeta { return g.Suffixes },
		stackCfg.SuffixStartSpacer, stackCfg.SuffixMidSpacer, stackCfg.SuffixEndSpacer, stackCfg.Deduplicate,
	)
}

func (e *Engine) formatStackedAffix(
	user *model.User,
	groups map[string]*model.Group,
	contexts map[string]string,
	direct []model.PriorityMeta,
	groupAffixes func(*model.Group) []model.PriorityMeta,
	startSpacer, midSpacer, endSpacer string,
	deduplicate bool,
) string {
	if user == nil {
		return ""
	}
	now := time.Now()
	type rankedAffix struct {
		priority int
		order    int
		value    string
	}
	var all []rankedAffix
	order := 0
	for _, item := range direct {
		if !item.IsExpired(now) && item.MatchesContext(contexts) && item.Value != "" {
			all = append(all, rankedAffix{priority: item.Priority, order: order, value: item.Value})
			order++
		}
	}
	for _, g := range e.ResolveInheritedGroups(user, groups, contexts, now) {
		for _, item := range groupAffixes(g) {
			if !item.IsExpired(now) && item.MatchesContext(contexts) && item.Value != "" {
				all = append(all, rankedAffix{priority: item.Priority, order: order, value: item.Value})
				order++
			}
		}
	}
	if len(all) == 0 {
		return ""
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].priority != all[j].priority {
			return all[i].priority > all[j].priority
		}
		return all[i].order < all[j].order
	})

	var values []string
	seen := make(map[string]struct{}, len(all))
	for _, a := range all {
		if deduplicate {
			if _, exists := seen[a.value]; exists {
				continue
			}
			seen[a.value] = struct{}{}
		}
		values = append(values, a.value)
	}
	if len(values) == 0 {
		return ""
	}
	return startSpacer + strings.Join(values, midSpacer) + endSpacer
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

	primary := e.ResolvePrimaryGroup(user, groups, contexts, "default")
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
	return e.GetMetaWithContext(user, groups, key, nil)
}

// GetMetaWithContext resolves a custom metadata key honoring active contextual and temporary entries.
func (e *Engine) GetMetaWithContext(user *model.User, groups map[string]*model.Group, key string, contexts map[string]string) (string, bool) {
	all := e.ResolveAllMeta(user, groups, contexts)
	if val, ok := all[key]; ok {
		return val, true
	}
	for k, v := range all {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// ResolveAllMeta computes all effective metadata keys for a user in the supplied contexts.
// Resolution priority: user MetaEntries (most specific context -> highest priority) > user Meta >
// resolved groups in descending weight order (group MetaEntries > group Meta).
func (e *Engine) ResolveAllMeta(user *model.User, groups map[string]*model.Group, contexts map[string]string) map[string]string {
	result := make(map[string]string)
	if user == nil {
		return result
	}
	now := time.Now()

	applyMetaEntries := func(entries []model.MetaEntry, staticMeta map[string]string) {
		var matching []model.MetaEntry
		for _, entry := range entries {
			if !entry.IsExpired(now) && entry.MatchesContext(contexts) {
				matching = append(matching, entry)
			}
		}
		sort.SliceStable(matching, func(i, j int) bool {
			if len(matching[i].Contexts) != len(matching[j].Contexts) {
				return len(matching[i].Contexts) > len(matching[j].Contexts)
			}
			return matching[i].Priority > matching[j].Priority
		})
		for _, entry := range matching {
			if _, exists := result[entry.Key]; !exists {
				result[entry.Key] = entry.Value
			}
		}
		for k, v := range staticMeta {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}

	applyMetaEntries(user.MetaEntries, user.Meta)
	for _, g := range e.ResolveInheritedGroups(user, groups, contexts, now) {
		applyMetaEntries(g.MetaEntries, g.Meta)
	}
	return result
}
