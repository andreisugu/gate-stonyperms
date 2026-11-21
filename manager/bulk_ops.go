package manager

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

const exportSchemaVersion = 1

func deriveExportDir(store any) string {
	if s, ok := store.(interface{ BaseDir() string }); ok {
		if bd := strings.TrimSpace(s.BaseDir()); bd != "" {
			return filepath.Join(bd, "exports")
		}
	}
	return filepath.Join("config", "stonyperms", "exports")
}

// SetExportDir configures the directory where /sp export and /sp import read and write backups.
func (m *Manager) SetExportDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportDir = strings.TrimSpace(dir)
}

// ExportDir returns the configured export directory.
func (m *Manager) ExportDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.exportDir
}

// SearchHit describes one matched permission or parent inheritance entry.
type SearchHit struct {
	TargetType string            `json:"targetType"` // "user" or "group"
	Target     string            `json:"target"`
	UUID       string            `json:"uuid,omitempty"`
	NodeKey    string            `json:"nodeKey"`
	Value      bool              `json:"value"`
	Expiry     int64             `json:"expiry,omitempty"`
	Contexts   map[string]string `json:"contexts,omitempty"`
}

// SearchResult aggregates search hits across groups and users.
type SearchResult struct {
	Query     string      `json:"query"`
	GroupHits []SearchHit `json:"groupHits"`
	UserHits  []SearchHit `json:"userHits"`
}

// Search scans all groups and users for active nodes matching query or group membership
// (`group.<name>` / `parent:<name>` / permission exact or wildcard pattern).
func (m *Manager) Search(query string) SearchResult {
	clean := strings.ToLower(strings.TrimSpace(query))
	res := SearchResult{Query: clean}
	if clean == "" {
		return res
	}

	now := time.Now()
	isGroupQuery := false
	targetGroup := ""
	switch {
	case strings.HasPrefix(clean, "group."):
		isGroupQuery = true
		targetGroup = strings.TrimPrefix(clean, "group.")
	case strings.HasPrefix(clean, "parent:"):
		isGroupQuery = true
		targetGroup = strings.TrimPrefix(clean, "parent:")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, g := range m.groups {
		if g == nil {
			continue
		}
		if isGroupQuery {
			for _, p := range g.Parents {
				if !p.IsExpired(now) && strings.EqualFold(p.Group, targetGroup) {
					res.GroupHits = append(res.GroupHits, SearchHit{
						TargetType: "group",
						Target:     g.Name,
						NodeKey:    "group." + p.Group,
						Value:      true,
						Expiry:     p.Expiry,
						Contexts:   copyMetaEntry(model.MetaEntry{Contexts: p.Contexts}).Contexts,
					})
				}
			}
			continue
		}
		for _, n := range g.Nodes {
			if !n.IsExpired(now) && nodeKeyMatchesQuery(n.Key, clean) {
				res.GroupHits = append(res.GroupHits, SearchHit{
					TargetType: "group",
					Target:     g.Name,
					NodeKey:    n.Key,
					Value:      n.Value,
					Expiry:     n.Expiry,
					Contexts:   copyNode(n).Contexts,
				})
			}
		}
	}

	for _, u := range m.users {
		if u == nil {
			continue
		}
		label := u.Username
		if label == "" {
			label = u.UUID.String()
		}
		if isGroupQuery {
			if strings.EqualFold(u.PrimaryGroup, targetGroup) {
				res.UserHits = append(res.UserHits, SearchHit{
					TargetType: "user",
					Target:     label,
					UUID:       u.UUID.String(),
					NodeKey:    "group." + targetGroup + " (primary)",
					Value:      true,
				})
			}
			for _, p := range u.Parents {
				if !p.IsExpired(now) && strings.EqualFold(p.Group, targetGroup) {
					res.UserHits = append(res.UserHits, SearchHit{
						TargetType: "user",
						Target:     label,
						UUID:       u.UUID.String(),
						NodeKey:    "group." + p.Group,
						Value:      true,
						Expiry:     p.Expiry,
						Contexts:   copyMetaEntry(model.MetaEntry{Contexts: p.Contexts}).Contexts,
					})
				}
			}
			continue
		}
		for _, n := range u.Nodes {
			if !n.IsExpired(now) && nodeKeyMatchesQuery(n.Key, clean) {
				res.UserHits = append(res.UserHits, SearchHit{
					TargetType: "user",
					Target:     label,
					UUID:       u.UUID.String(),
					NodeKey:    n.Key,
					Value:      n.Value,
					Expiry:     n.Expiry,
					Contexts:   copyNode(n).Contexts,
				})
			}
		}
	}

	sortHits := func(hits []SearchHit) {
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].Target != hits[j].Target {
				return hits[i].Target < hits[j].Target
			}
			return hits[i].NodeKey < hits[j].NodeKey
		})
	}
	sortHits(res.GroupHits)
	sortHits(res.UserHits)
	return res
}

func nodeKeyMatchesQuery(nodeKey, query string) bool {
	lk := strings.ToLower(strings.TrimSpace(nodeKey))
	if lk == query {
		return true
	}
	if strings.ContainsAny(query, "*?[") {
		if ok, err := path.Match(query, lk); err == nil && ok {
			return true
		}
		if strings.HasSuffix(query, ".*") {
			prefix := strings.TrimSuffix(query, "*")
			if strings.HasPrefix(lk, prefix) {
				return true
			}
		}
	}
	return false
}

// BulkConstraint restricts which nodes are matched by a BulkUpdate operation.
type BulkConstraint struct {
	Field    string // "permission", "value", "server", "world"
	Operator string // "==", "!=", "~~", "!~"
	Pattern  string
}

// BulkUpdateOptions defines a mass update or delete across users and/or groups.
type BulkUpdateOptions struct {
	Scope       string // "all", "users", "groups"
	Action      string // "update", "delete"
	Field       string // "permission", "server", "world"
	Value       string
	Constraints []BulkConstraint
}

// BulkUpdateSummary reports affected entities and nodes from a BulkUpdate or preview.
type BulkUpdateSummary struct {
	DryRun        bool   `json:"dryRun"`
	GroupsChanged int    `json:"groupsChanged"`
	UsersChanged  int    `json:"usersChanged"`
	NodesAffected int    `json:"nodesAffected"`
	Revision      uint64 `json:"revision"`
}

func validateBulkOptions(opts BulkUpdateOptions) (BulkUpdateOptions, error) {
	opts.Scope = strings.ToLower(strings.TrimSpace(opts.Scope))
	if opts.Scope == "" {
		opts.Scope = "all"
	}
	switch opts.Scope {
	case "all", "users", "groups":
	default:
		return opts, fmt.Errorf("invalid bulkupdate scope %q (expected all, users, or groups)", opts.Scope)
	}

	opts.Action = strings.ToLower(strings.TrimSpace(opts.Action))
	switch opts.Action {
	case "update", "delete":
	default:
		return opts, fmt.Errorf("invalid bulkupdate action %q (expected update or delete)", opts.Action)
	}

	if opts.Action == "update" {
		opts.Field = strings.ToLower(strings.TrimSpace(opts.Field))
		opts.Value = strings.TrimSpace(opts.Value)
		switch opts.Field {
		case "permission", "server", "world":
		default:
			return opts, fmt.Errorf("invalid bulkupdate field %q (expected permission, server, or world)", opts.Field)
		}
		if opts.Field == "permission" {
			opts.Value = strings.ToLower(opts.Value)
		}
		if opts.Value == "" {
			return opts, errors.New("bulkupdate value cannot be empty for update action")
		}
	}

	for i, c := range opts.Constraints {
		f := strings.ToLower(strings.TrimSpace(c.Field))
		op := strings.TrimSpace(c.Operator)
		switch f {
		case "permission", "value", "server", "world":
		default:
			return opts, fmt.Errorf("invalid constraint field %q", c.Field)
		}
		switch op {
		case "==", "!=", "~~", "!~":
		default:
			return opts, fmt.Errorf("invalid constraint operator %q (expected ==, !=, ~~, or !~)", c.Operator)
		}
		if f == "permission" || f == "value" {
			opts.Constraints[i].Pattern = strings.ToLower(strings.TrimSpace(c.Pattern))
		} else {
			opts.Constraints[i].Pattern = strings.TrimSpace(c.Pattern)
		}
		opts.Constraints[i].Field = f
		opts.Constraints[i].Operator = op
	}
	return opts, nil
}

// PreviewBulkUpdate calculates how many groups, users, and nodes would be changed without mutating state.
func (m *Manager) PreviewBulkUpdate(opts BulkUpdateOptions) (BulkUpdateSummary, error) {
	return m.runBulkUpdate(opts, "", true)
}

// BulkUpdate applies a validated bulk mutation atomically across users and/or groups.
func (m *Manager) BulkUpdate(opts BulkUpdateOptions, actor string) (BulkUpdateSummary, error) {
	return m.runBulkUpdate(opts, actor, false)
}

func (m *Manager) runBulkUpdate(opts BulkUpdateOptions, actor string, dryRun bool) (BulkUpdateSummary, error) {
	cleanOpts, err := validateBulkOptions(opts)
	if err != nil {
		return BulkUpdateSummary{}, err
	}

	m.mu.Lock()
	summary := BulkUpdateSummary{
		DryRun:   dryRun,
		Revision: m.revision.Load(),
	}

	var changedUsers []*model.User
	var changedGroups []*model.Group

	if cleanOpts.Scope == "all" || cleanOpts.Scope == "groups" {
		for _, g := range m.groups {
			if g == nil {
				continue
			}
			nextNodes, affected, changed := applyBulkToNodeSlice(g.Nodes, cleanOpts)
			if changed {
				summary.GroupsChanged++
				summary.NodesAffected += affected
				if !dryRun {
					cp := copyGroup(g)
					cp.Nodes = nextNodes
					changedGroups = append(changedGroups, cp)
				}
			}
		}
	}

	if cleanOpts.Scope == "all" || cleanOpts.Scope == "users" {
		for _, u := range m.users {
			if u == nil {
				continue
			}
			nextNodes, affected, changed := applyBulkToNodeSlice(u.Nodes, cleanOpts)
			if changed {
				summary.UsersChanged++
				summary.NodesAffected += affected
				if !dryRun {
					cp := copyUser(u)
					cp.Nodes = nextNodes
					changedUsers = append(changedUsers, cp)
				}
			}
		}
	}

	if dryRun || (len(changedUsers) == 0 && len(changedGroups) == 0) {
		m.mu.Unlock()
		return summary, nil
	}

	for _, g := range changedGroups {
		if saveErr := m.storage.SaveGroup(g); saveErr != nil {
			m.mu.Unlock()
			return BulkUpdateSummary{}, saveErr
		}
	}
	for _, u := range changedUsers {
		if saveErr := m.storage.SaveUser(u); saveErr != nil {
			m.mu.Unlock()
			return BulkUpdateSummary{}, saveErr
		}
	}

	for _, g := range changedGroups {
		m.groups[g.Name] = g
	}
	for _, u := range changedUsers {
		m.users[u.UUID] = u
	}

	rev := m.bumpRevisionLocked()
	summary.Revision = rev
	if actor != "" {
		m.logActionLocked(actor, "general", "bulkupdate",
			fmt.Sprintf("BulkUpdate %s %s (%d nodes across %d groups, %d users)",
				cleanOpts.Scope, cleanOpts.Action, summary.NodesAffected, summary.GroupsChanged, summary.UsersChanged))
	}
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventReload,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return summary, nil
}

func applyBulkToNodeSlice(nodes []model.Node, opts BulkUpdateOptions) ([]model.Node, int, bool) {
	if len(nodes) == 0 {
		return nil, 0, false
	}
	out := make([]model.Node, 0, len(nodes))
	affected := 0
	changed := false

	for _, n := range nodes {
		if !nodeMatchesAllConstraints(n, opts.Constraints) {
			out = append(out, copyNode(n))
			continue
		}
		if opts.Action == "delete" {
			affected++
			changed = true
			continue
		}

		cp := copyNode(n)
		switch opts.Field {
		case "permission":
			if cp.Key != opts.Value {
				cp.Key = opts.Value
				changed = true
			}
		case "server", "world":
			if cp.Contexts == nil {
				cp.Contexts = make(map[string]string)
			}
			if !strings.EqualFold(cp.Contexts[opts.Field], opts.Value) {
				cp.Contexts[opts.Field] = opts.Value
				changed = true
			}
		}
		affected++
		out = append(out, cp)
	}
	return out, affected, changed
}

func nodeMatchesAllConstraints(n model.Node, constraints []BulkConstraint) bool {
	for _, c := range constraints {
		var actual string
		hasField := true
		switch c.Field {
		case "permission":
			actual = strings.ToLower(n.Key)
		case "value":
			if n.Value {
				actual = "true"
			} else {
				actual = "false"
			}
		case "server", "world":
			v, ok := n.Contexts[c.Field]
			if !ok {
				hasField = false
			}
			actual = strings.ToLower(v)
		}

		pat := strings.ToLower(c.Pattern)
		matched := false
		switch c.Operator {
		case "==":
			matched = hasField && actual == pat
		case "!=":
			matched = !hasField || actual != pat
		case "~~":
			matched = hasField && wildcardOrContains(actual, pat)
		case "!~":
			matched = !hasField || !wildcardOrContains(actual, pat)
		}
		if !matched {
			return false
		}
	}
	return true
}

func wildcardOrContains(value, pattern string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		if ok, err := path.Match(pattern, value); err == nil && ok {
			return true
		}
		if strings.Contains(pattern, "%") {
			pattern = strings.ReplaceAll(pattern, "%", "*")
			if ok, err := path.Match(pattern, value); err == nil && ok {
				return true
			}
		}
		return false
	}
	if strings.Contains(pattern, "%") {
		starPat := strings.ReplaceAll(pattern, "%", "*")
		if ok, err := path.Match(starPat, value); err == nil && ok {
			return true
		}
	}
	return strings.Contains(value, pattern)
}

// ExportBundle is the portable single-file backup/migration snapshot written by ExportToFile.
type ExportBundle struct {
	SchemaVersion int            `json:"schemaVersion"`
	ExportedAt    int64          `json:"exportedAt"`
	ExportedBy    string         `json:"exportedBy,omitempty"`
	Groups        []*model.Group `json:"groups"`
	Tracks        []*model.Track `json:"tracks"`
	Users         []*model.User  `json:"users"`
}

// ImportSummary describes entities loaded by ImportFromFile.
type ImportSummary struct {
	Groups   int    `json:"groups"`
	Tracks   int    `json:"tracks"`
	Users    int    `json:"users"`
	Replaced bool   `json:"replaced"`
	Revision uint64 `json:"revision"`
}

func resolveSafeExportPath(baseDir, rawName string, forWrite bool) (string, bool, error) {
	if baseDir == "" {
		return "", false, errors.New("export directory is not configured")
	}
	clean := strings.TrimSpace(rawName)
	if clean == "" || strings.Contains(clean, "..") || strings.ContainsAny(clean, `/\`) {
		return "", false, errors.New("filename must be a simple name without path separators or '..'")
	}

	if forWrite {
		isGz := strings.HasSuffix(strings.ToLower(clean), ".gz")
		if !isGz && !strings.HasSuffix(strings.ToLower(clean), ".json") {
			clean += ".json"
		}
		return filepath.Join(baseDir, clean), isGz, nil
	}

	candidates := []string{clean}
	if !strings.HasSuffix(strings.ToLower(clean), ".json") && !strings.HasSuffix(strings.ToLower(clean), ".json.gz") {
		candidates = append(candidates, clean+".json", clean+".json.gz")
	}
	for _, cand := range candidates {
		full := filepath.Join(baseDir, cand)
		if _, err := os.Stat(full); err == nil {
			return full, strings.HasSuffix(strings.ToLower(cand), ".gz"), nil
		}
	}
	return "", false, fmt.Errorf("export file %q not found in %s", clean, baseDir)
}

// ExportToFile writes a complete portable snapshot of groups, tracks, and users to disk.
func (m *Manager) ExportToFile(filename, actor string) (string, *ExportBundle, error) {
	m.mu.RLock()
	exportDir := m.exportDir
	bundle := &ExportBundle{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    time.Now().UTC().Unix(),
		ExportedBy:    actor,
		Groups:        make([]*model.Group, 0, len(m.groups)),
		Tracks:        make([]*model.Track, 0, len(m.tracks)),
		Users:         make([]*model.User, 0, len(m.users)),
	}
	for _, g := range m.groups {
		if g != nil {
			bundle.Groups = append(bundle.Groups, copyGroup(g))
		}
	}
	for _, t := range m.tracks {
		if t != nil {
			bundle.Tracks = append(bundle.Tracks, copyTrack(t))
		}
	}
	for _, u := range m.users {
		if u != nil {
			bundle.Users = append(bundle.Users, copyUser(u))
		}
	}
	m.mu.RUnlock()

	sort.Slice(bundle.Groups, func(i, j int) bool { return bundle.Groups[i].Name < bundle.Groups[j].Name })
	sort.Slice(bundle.Tracks, func(i, j int) bool { return bundle.Tracks[i].Name < bundle.Tracks[j].Name })
	sort.Slice(bundle.Users, func(i, j int) bool { return bundle.Users[i].UUID.String() < bundle.Users[j].UUID.String() })

	fullPath, useGzip, err := resolveSafeExportPath(exportDir, filename, true)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", nil, err
	}

	rawJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", nil, err
	}

	var payload []byte
	if useGzip {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, wErr := gz.Write(rawJSON); wErr != nil {
			_ = gz.Close()
			return "", nil, wErr
		}
		if cErr := gz.Close(); cErr != nil {
			return "", nil, cErr
		}
		payload = buf.Bytes()
	} else {
		payload = rawJSON
	}

	tmp := fullPath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return "", nil, err
	}
	if err := os.Rename(tmp, fullPath); err != nil {
		_ = os.Remove(tmp)
		return "", nil, err
	}

	if actor != "" {
		m.LogAction(actor, "general", filepath.Base(fullPath),
			fmt.Sprintf("Exported %d groups, %d tracks, %d users", len(bundle.Groups), len(bundle.Tracks), len(bundle.Users)))
	}
	return fullPath, bundle, nil
}

// ImportFromFile loads groups, tracks, and users from an export file (merge or full replace).
func (m *Manager) ImportFromFile(filename string, replace bool, actor string) (ImportSummary, error) {
	exportDir := m.ExportDir()
	fullPath, useGzip, err := resolveSafeExportPath(exportDir, filename, false)
	if err != nil {
		return ImportSummary{}, err
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ImportSummary{}, err
	}
	if useGzip {
		gz, gzErr := gzip.NewReader(bytes.NewReader(data))
		if gzErr != nil {
			return ImportSummary{}, gzErr
		}
		var out bytes.Buffer
		if _, readErr := out.ReadFrom(gz); readErr != nil {
			_ = gz.Close()
			return ImportSummary{}, readErr
		}
		_ = gz.Close()
		data = out.Bytes()
	}

	var bundle ExportBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return ImportSummary{}, fmt.Errorf("invalid export file JSON: %w", err)
	}

	m.mu.Lock()
	if replace {
		keepGroups := make(map[string]struct{}, len(bundle.Groups))
		for _, g := range bundle.Groups {
			if g != nil && strings.TrimSpace(g.Name) != "" {
				keepGroups[strings.ToLower(strings.TrimSpace(g.Name))] = struct{}{}
			}
		}
		keepGroups["default"] = struct{}{}
		for name := range m.groups {
			if _, keep := keepGroups[name]; !keep {
				if err := m.storage.DeleteGroup(name); err != nil {
					m.mu.Unlock()
					return ImportSummary{}, err
				}
				delete(m.groups, name)
			}
		}

		keepTracks := make(map[string]struct{}, len(bundle.Tracks))
		for _, t := range bundle.Tracks {
			if t != nil && strings.TrimSpace(t.Name) != "" {
				keepTracks[strings.ToLower(strings.TrimSpace(t.Name))] = struct{}{}
			}
		}
		for name := range m.tracks {
			if _, keep := keepTracks[name]; !keep {
				if err := m.storage.DeleteTrack(name); err != nil {
					m.mu.Unlock()
					return ImportSummary{}, err
				}
				delete(m.tracks, name)
			}
		}

		keepUsers := make(map[uuid.UUID]struct{}, len(bundle.Users))
		for _, u := range bundle.Users {
			if u != nil && u.UUID != uuid.Nil {
				keepUsers[u.UUID] = struct{}{}
			}
		}
		for id := range m.users {
			if _, keep := keepUsers[id]; !keep {
				if err := m.storage.DeleteUser(id); err != nil {
					m.mu.Unlock()
					return ImportSummary{}, err
				}
				delete(m.users, id)
			}
		}
	}

	importedGroups := 0
	for _, g := range bundle.Groups {
		if g == nil || strings.TrimSpace(g.Name) == "" {
			continue
		}
		cp := copyGroup(g)
		cp.Name = strings.ToLower(strings.TrimSpace(cp.Name))
		if err := m.storage.SaveGroup(cp); err != nil {
			m.mu.Unlock()
			return ImportSummary{}, err
		}
		m.groups[cp.Name] = cp
		importedGroups++
	}
	if m.groups["default"] == nil {
		def := model.NewGroup("default", 0)
		if err := m.storage.SaveGroup(def); err != nil {
			m.mu.Unlock()
			return ImportSummary{}, err
		}
		m.groups["default"] = def
	}

	importedTracks := 0
	for _, t := range bundle.Tracks {
		if t == nil || strings.TrimSpace(t.Name) == "" {
			continue
		}
		cp := copyTrack(t)
		cp.Name = strings.ToLower(strings.TrimSpace(cp.Name))
		if err := m.storage.SaveTrack(cp); err != nil {
			m.mu.Unlock()
			return ImportSummary{}, err
		}
		m.tracks[cp.Name] = cp
		importedTracks++
	}

	importedUsers := 0
	for _, u := range bundle.Users {
		if u == nil || u.UUID == uuid.Nil {
			continue
		}
		cp := copyUser(u)
		if err := m.storage.SaveUser(cp); err != nil {
			m.mu.Unlock()
			return ImportSummary{}, err
		}
		m.users[cp.UUID] = cp
		importedUsers++
	}

	m.nameToUUID = make(map[string]uuid.UUID, len(m.users))
	for id, u := range m.users {
		if u.Username != "" {
			m.nameToUUID[strings.ToLower(u.Username)] = id
		}
	}

	rev := m.bumpRevisionLocked()
	if actor != "" {
		m.logActionLocked(actor, "general", filepath.Base(fullPath),
			fmt.Sprintf("Imported %d groups, %d tracks, %d users (replace=%v)", importedGroups, importedTracks, importedUsers, replace))
	}
	m.mu.Unlock()

	m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventReload,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})
	return ImportSummary{
		Groups:   importedGroups,
		Tracks:   importedTracks,
		Users:    importedUsers,
		Replaced: replace,
		Revision: rev,
	}, nil
}

// SetUserTrackGroup replaces any existing parent group on trackName (within contexts) with targetGroup.
func (m *Manager) SetUserTrackGroup(id uuid.UUID, trackName, targetGroup string, contexts map[string]string, actor string) (*model.User, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return nil, ErrTrackNotFound
	}
	targetGroup = strings.ToLower(strings.TrimSpace(targetGroup))
	if _, ok := track.IndexOf(targetGroup); !ok {
		return nil, fmt.Errorf("group %q is not part of track %q", targetGroup, track.Name)
	}
	if m.GetGroup(targetGroup) == nil {
		return nil, ErrGroupNotFound
	}

	updated, _, err := m.MutateUser(id, "", false, "default", actor, func(u *model.User) (bool, string, error) {
		trackSet := trackGroupSet(track)
		kept := make([]model.Inheritance, 0, len(u.Parents)+1)
		for _, p := range u.Parents {
			if trackSet[strings.ToLower(p.Group)] && model.SameContexts(p.Contexts, contexts) {
				continue
			}
			kept = append(kept, p)
		}
		u.Parents = kept
		inh := model.NewInheritance(targetGroup)
		inh.Contexts = cloneContextsLocal(contexts)
		u.AddParent(inh)
		if len(contexts) == 0 && (u.PrimaryGroup == "" || u.PrimaryGroup == "default" || trackSet[strings.ToLower(u.PrimaryGroup)]) {
			u.PrimaryGroup = targetGroup
		}
		return true, fmt.Sprintf("Set track '%s' group to '%s'", track.Name, targetGroup), nil
	})
	return updated, err
}

// ClearUserTrack removes all parent memberships along trackName matching contexts.
func (m *Manager) ClearUserTrack(id uuid.UUID, trackName string, contexts map[string]string, actor string) (*model.User, bool, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return nil, false, ErrTrackNotFound
	}
	removedAny := false
	updated, _, err := m.MutateUser(id, "", false, "default", actor, func(u *model.User) (bool, string, error) {
		trackSet := trackGroupSet(track)
		kept := make([]model.Inheritance, 0, len(u.Parents))
		for _, p := range u.Parents {
			if trackSet[strings.ToLower(p.Group)] && model.SameContexts(p.Contexts, contexts) {
				removedAny = true
				continue
			}
			kept = append(kept, p)
		}
		u.Parents = kept
		if len(contexts) == 0 && trackSet[strings.ToLower(u.PrimaryGroup)] {
			u.PrimaryGroup = "default"
			removedAny = true
		}
		if !removedAny {
			return false, "", nil
		}
		return true, fmt.Sprintf("Cleared track '%s'", track.Name), nil
	})
	return updated, removedAny, err
}

// SetGroupTrackParent replaces any parent groups of groupName belonging to trackName (within contexts) with targetGroup.
func (m *Manager) SetGroupTrackParent(groupName, trackName, targetGroup string, contexts map[string]string, actor string) (*model.Group, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return nil, ErrTrackNotFound
	}
	targetGroup = strings.ToLower(strings.TrimSpace(targetGroup))
	if _, ok := track.IndexOf(targetGroup); !ok {
		return nil, fmt.Errorf("group %q is not part of track %q", targetGroup, track.Name)
	}
	if m.GetGroup(targetGroup) == nil {
		return nil, ErrGroupNotFound
	}
	updated, _, err := m.MutateGroup(groupName, actor, func(g *model.Group) (bool, string, error) {
		trackSet := trackGroupSet(track)
		kept := make([]model.Inheritance, 0, len(g.Parents)+1)
		for _, p := range g.Parents {
			if trackSet[strings.ToLower(p.Group)] && model.SameContexts(p.Contexts, contexts) {
				continue
			}
			kept = append(kept, p)
		}
		g.Parents = kept
		inh := model.NewInheritance(targetGroup)
		inh.Contexts = cloneContextsLocal(contexts)
		g.AddParent(inh)
		return true, fmt.Sprintf("Set track '%s' parent to '%s'", track.Name, targetGroup), nil
	})
	return updated, err
}

// ClearGroupTrack removes all parent inheritances on groupName belonging to trackName matching contexts.
func (m *Manager) ClearGroupTrack(groupName, trackName string, contexts map[string]string, actor string) (*model.Group, bool, error) {
	track := m.GetTrack(trackName)
	if track == nil {
		return nil, false, ErrTrackNotFound
	}
	removedAny := false
	updated, _, err := m.MutateGroup(groupName, actor, func(g *model.Group) (bool, string, error) {
		trackSet := trackGroupSet(track)
		kept := make([]model.Inheritance, 0, len(g.Parents))
		for _, p := range g.Parents {
			if trackSet[strings.ToLower(p.Group)] && model.SameContexts(p.Contexts, contexts) {
				removedAny = true
				continue
			}
			kept = append(kept, p)
		}
		g.Parents = kept
		if !removedAny {
			return false, "", nil
		}
		return true, fmt.Sprintf("Cleared track '%s'", track.Name), nil
	})
	return updated, removedAny, err
}

func trackGroupSet(track *model.Track) map[string]bool {
	set := make(map[string]bool, len(track.Groups))
	for _, g := range track.Groups {
		set[strings.ToLower(strings.TrimSpace(g))] = true
	}
	return set
}

func cloneContextsLocal(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}
