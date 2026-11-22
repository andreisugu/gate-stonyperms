package manager

import (
	"fmt"
	"sort"
	"strings"

	"github.com/andreisugu/gate-stonyperms/model"
)

// EditorChangeNotice is delivered after an editor/applyedits mutation so the
// proxy layer can broadcast a LuckPerms-style diff to online staff.
type EditorChangeNotice struct {
	Actor   string
	Summary EditorApplySummary
}

// SetEditorChangeNotifier registers the proxy-side diff broadcaster.
func (m *Manager) SetEditorChangeNotifier(fn func(EditorChangeNotice)) {
	if fn == nil {
		m.changeNotifier.Store(nil)
		return
	}
	m.changeNotifier.Store(&fn)
}

func (m *Manager) notifyEditorChange(notice EditorChangeNotice) {
	if fn := m.changeNotifier.Load(); fn != nil {
		(*fn)(notice)
	}
}

// SetLogNotify mutes or unmutes editor diff broadcasts for one actor.
func (m *Manager) SetLogNotify(actor string, enabled bool) {
	key := strings.ToLower(strings.TrimSpace(actor))
	if key == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled {
		delete(m.logNotifyMuted, key)
	} else {
		m.logNotifyMuted[key] = true
	}
}

// LogNotifyEnabled reports whether editor diff broadcasts are enabled for actor.
func (m *Manager) LogNotifyEnabled(actor string) bool {
	key := strings.ToLower(strings.TrimSpace(actor))
	if key == "" {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.logNotifyMuted[key]
}

func diffContexts(ctx map[string]string) string {
	if len(ctx) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+ctx[k])
	}
	return " [" + strings.Join(parts, ",") + "]"
}

func diffNodeKey(n model.Node) string {
	return strings.ToLower(strings.TrimSpace(n.Key)) + "|" + diffContexts(n.Contexts)
}

func diffParentKey(p model.Inheritance) string {
	return strings.ToLower(strings.TrimSpace(p.Group)) + "|" + diffContexts(p.Contexts)
}

func diffAffixKey(p model.PriorityMeta) string {
	return fmt.Sprintf("%d|%s", p.Priority, diffContexts(p.Contexts))
}

func diffMetaKey(m model.MetaEntry) string {
	return strings.ToLower(strings.TrimSpace(m.Key)) + "|" + diffContexts(m.Contexts)
}

func diffNodes(prefix string, oldNodes, newNodes []model.Node) []string {
	oldMap := make(map[string]model.Node, len(oldNodes))
	for _, n := range oldNodes {
		oldMap[diffNodeKey(n)] = n
	}
	newMap := make(map[string]model.Node, len(newNodes))
	var lines []string
	for _, n := range newNodes {
		newMap[diffNodeKey(n)] = n
		prev, ok := oldMap[diffNodeKey(n)]
		if !ok {
			lines = append(lines, fmt.Sprintf("+ %s node %s=%t%s", prefix, n.Key, n.Value, diffContexts(n.Contexts)))
			continue
		}
		if prev.Value != n.Value {
			lines = append(lines, fmt.Sprintf("~ %s node %s: %t -> %t%s", prefix, n.Key, prev.Value, n.Value, diffContexts(n.Contexts)))
		}
		if prev.Expiry != n.Expiry {
			lines = append(lines, fmt.Sprintf("~ %s node %s expiry: %s -> %s%s", prefix, n.Key, expiryText(prev.Expiry), expiryText(n.Expiry), diffContexts(n.Contexts)))
		}
	}
	for key, n := range oldMap {
		if _, ok := newMap[key]; !ok {
			lines = append(lines, fmt.Sprintf("- %s node %s%s", prefix, n.Key, diffContexts(n.Contexts)))
		}
	}
	return lines
}

func diffParents(prefix string, oldParents, newParents []model.Inheritance) []string {
	oldMap := make(map[string]model.Inheritance, len(oldParents))
	for _, p := range oldParents {
		oldMap[diffParentKey(p)] = p
	}
	newMap := make(map[string]model.Inheritance, len(newParents))
	var lines []string
	for _, p := range newParents {
		newMap[diffParentKey(p)] = p
		prev, ok := oldMap[diffParentKey(p)]
		if !ok {
			lines = append(lines, fmt.Sprintf("+ %s parent %s%s", prefix, p.Group, diffContexts(p.Contexts)))
			continue
		}
		if prev.Expiry != p.Expiry {
			lines = append(lines, fmt.Sprintf("~ %s parent %s expiry: %s -> %s%s", prefix, p.Group, expiryText(prev.Expiry), expiryText(p.Expiry), diffContexts(p.Contexts)))
		}
	}
	for key, p := range oldMap {
		if _, ok := newMap[key]; !ok {
			lines = append(lines, fmt.Sprintf("- %s parent %s%s", prefix, p.Group, diffContexts(p.Contexts)))
		}
	}
	return lines
}

func diffAffixes(prefix string, oldAffixes, newAffixes []model.PriorityMeta) []string {
	oldMap := make(map[string]model.PriorityMeta, len(oldAffixes))
	for _, a := range oldAffixes {
		oldMap[diffAffixKey(a)] = a
	}
	newMap := make(map[string]model.PriorityMeta, len(newAffixes))
	var lines []string
	for _, a := range newAffixes {
		newMap[diffAffixKey(a)] = a
		prev, ok := oldMap[diffAffixKey(a)]
		if !ok {
			lines = append(lines, fmt.Sprintf("+ %s %s priority %d: %q%s", prefix, kindForAffix(prefix), a.Priority, a.Value, diffContexts(a.Contexts)))
			continue
		}
		if prev.Value != a.Value {
			lines = append(lines, fmt.Sprintf("~ %s %s priority %d: %q -> %q%s", prefix, kindForAffix(prefix), a.Priority, prev.Value, a.Value, diffContexts(a.Contexts)))
		}
		if prev.Expiry != a.Expiry {
			lines = append(lines, fmt.Sprintf("~ %s %s priority %d expiry: %s -> %s%s", prefix, kindForAffix(prefix), a.Priority, expiryText(prev.Expiry), expiryText(a.Expiry), diffContexts(a.Contexts)))
		}
	}
	for key, a := range oldMap {
		if _, ok := newMap[key]; !ok {
			lines = append(lines, fmt.Sprintf("- %s %s priority %d: %q%s", prefix, kindForAffix(prefix), a.Priority, a.Value, diffContexts(a.Contexts)))
		}
	}
	return lines
}

func kindForAffix(prefix string) string {
	if strings.Contains(strings.ToLower(prefix), "suffix") {
		return "suffix"
	}
	return "prefix"
}

func diffMeta(prefix string, oldMeta map[string]string, oldEntries []model.MetaEntry, newMeta map[string]string, newEntries []model.MetaEntry) []string {
	var lines []string
	for k, v := range newMeta {
		oldV, ok := oldMeta[k]
		if !ok {
			lines = append(lines, fmt.Sprintf("+ %s meta %s=%q", prefix, k, v))
		} else if oldV != v {
			lines = append(lines, fmt.Sprintf("~ %s meta %s: %q -> %q", prefix, k, oldV, v))
		}
	}
	for k, v := range oldMeta {
		if _, ok := newMeta[k]; !ok {
			lines = append(lines, fmt.Sprintf("- %s meta %s=%q", prefix, k, v))
		}
	}
	oldMap := make(map[string]model.MetaEntry, len(oldEntries))
	for _, m := range oldEntries {
		oldMap[diffMetaKey(m)] = m
	}
	newMap := make(map[string]model.MetaEntry, len(newEntries))
	for _, m := range newEntries {
		newMap[diffMetaKey(m)] = m
		prev, ok := oldMap[diffMetaKey(m)]
		if !ok {
			lines = append(lines, fmt.Sprintf("+ %s meta %s=%q%s", prefix, m.Key, m.Value, diffContexts(m.Contexts)))
			continue
		}
		if prev.Value != m.Value {
			lines = append(lines, fmt.Sprintf("~ %s meta %s: %q -> %q%s", prefix, m.Key, prev.Value, m.Value, diffContexts(m.Contexts)))
		}
	}
	for key, m := range oldMap {
		if _, ok := newMap[key]; !ok {
			lines = append(lines, fmt.Sprintf("- %s meta %s=%q%s", prefix, m.Key, m.Value, diffContexts(m.Contexts)))
		}
	}
	return lines
}

func expiryText(epoch int64) string {
	if epoch <= 0 {
		return "permanent"
	}
	return fmt.Sprintf("epoch %d", epoch)
}

func diffGroup(old, new *model.Group) []string {
	prefix := "group " + new.Name
	var lines []string
	if old.Weight != new.Weight {
		lines = append(lines, fmt.Sprintf("~ %s weight: %d -> %d", prefix, old.Weight, new.Weight))
	}
	if old.DisplayName != new.DisplayName {
		lines = append(lines, fmt.Sprintf("~ %s display: %q -> %q", prefix, old.DisplayName, new.DisplayName))
	}
	lines = append(lines, diffNodes(prefix, old.Nodes, new.Nodes)...)
	lines = append(lines, diffParents(prefix, old.Parents, new.Parents)...)
	lines = append(lines, diffAffixes(prefix+" prefix", old.Prefixes, new.Prefixes)...)
	lines = append(lines, diffAffixes(prefix+" suffix", old.Suffixes, new.Suffixes)...)
	lines = append(lines, diffMeta(prefix, old.Meta, old.MetaEntries, new.Meta, new.MetaEntries)...)
	return lines
}

func diffUser(old, new *model.User) []string {
	prefix := "user " + new.Username
	var lines []string
	if old.Username != new.Username {
		lines = append(lines, fmt.Sprintf("~ %s name: %q -> %q", prefix, old.Username, new.Username))
	}
	if old.PrimaryGroup != new.PrimaryGroup {
		lines = append(lines, fmt.Sprintf("~ %s primary: %q -> %q", prefix, old.PrimaryGroup, new.PrimaryGroup))
	}
	lines = append(lines, diffNodes(prefix, old.Nodes, new.Nodes)...)
	lines = append(lines, diffParents(prefix, old.Parents, new.Parents)...)
	lines = append(lines, diffAffixes(prefix+" prefix", old.Prefixes, new.Prefixes)...)
	lines = append(lines, diffAffixes(prefix+" suffix", old.Suffixes, new.Suffixes)...)
	lines = append(lines, diffMeta(prefix, old.Meta, old.MetaEntries, new.Meta, new.MetaEntries)...)
	return lines
}

func diffTrack(old, new *model.Track) []string {
	if strings.Join(old.Groups, " -> ") == strings.Join(new.Groups, " -> ") {
		return nil
	}
	return []string{fmt.Sprintf("~ track %s: %s -> %s", new.Name, strings.Join(old.Groups, " -> "), strings.Join(new.Groups, " -> "))}
}

func capDiff(lines []string) []string {
	const maxLines = 40
	if len(lines) <= maxLines {
		return lines
	}
	out := append([]string(nil), lines[:maxLines]...)
	out = append(out, fmt.Sprintf("... and %d more changes", len(lines)-maxLines))
	return out
}
