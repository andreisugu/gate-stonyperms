package model

import (
	"errors"
	"strings"
	"time"
)

// TempModifier controls how temporary nodes, parents, meta entries, and affixes
// interact with an already active matching entry.
type TempModifier string

const (
	TempModifierReplace    TempModifier = "replace"
	TempModifierAccumulate TempModifier = "accumulate"
	TempModifierDeny       TempModifier = "deny"
)

// ErrTempEntryExists is returned when TempModifierDeny encounters an active matching entry.
var ErrTempEntryExists = errors.New("matching temporary entry already exists")

// ParseTempModifier parses a temporary entry collision modifier keyword.
func ParseTempModifier(raw string) (TempModifier, bool) {
	switch TempModifier(strings.ToLower(strings.TrimSpace(raw))) {
	case TempModifierReplace:
		return TempModifierReplace, true
	case TempModifierAccumulate:
		return TempModifierAccumulate, true
	case TempModifierDeny:
		return TempModifierDeny, true
	default:
		return "", false
	}
}

// MetaEntry represents a metadata key-value entry with optional priority, expiration, and contexts.
type MetaEntry struct {
	Key      string            `json:"key"`
	Value    string            `json:"value"`
	Priority int               `json:"priority,omitempty"`
	Expiry   int64             `json:"expiry,omitempty"`
	Contexts map[string]string `json:"contexts,omitempty"`
}

// IsExpired reports whether the meta entry has expired relative to now.
func (m MetaEntry) IsExpired(now time.Time) bool {
	if m.Expiry <= 0 {
		return false
	}
	return now.Unix() >= m.Expiry
}

// MatchesContext reports whether the meta entry applies under queryContexts.
func (m MetaEntry) MatchesContext(queryContexts map[string]string) bool {
	return matchesContextMap(m.Contexts, queryContexts)
}

func matchesContextMap(entryContexts, queryContexts map[string]string) bool {
	if len(entryContexts) == 0 {
		return true
	}
	if len(queryContexts) == 0 {
		return false
	}
	for k, v := range entryContexts {
		queryVal, ok := queryContexts[k]
		if !ok || !strings.EqualFold(queryVal, v) {
			return false
		}
	}
	return true
}

func cloneContextMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	cp := make(map[string]string, len(src))
	for k, v := range src {
		cp[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return cp
}

func resolveExpiryWithModifier(existingExpiry, newExpiry int64, isExpired bool, mod TempModifier, now time.Time) (int64, error) {
	if mod == "" {
		mod = TempModifierReplace
	}
	if existingExpiry <= 0 {
		if mod == TempModifierDeny {
			return 0, ErrTempEntryExists
		}
		return 0, nil
	}
	switch mod {
	case TempModifierDeny:
		if !isExpired {
			return 0, ErrTempEntryExists
		}
		return newExpiry, nil
	case TempModifierAccumulate:
		if isExpired || existingExpiry <= now.Unix() {
			return newExpiry, nil
		}
		if newExpiry <= now.Unix() {
			return existingExpiry, nil
		}
		addedSeconds := newExpiry - now.Unix()
		return existingExpiry + addedSeconds, nil
	case TempModifierReplace:
		if !isExpired && existingExpiry > 0 && newExpiry > 0 && existingExpiry > newExpiry {
			return existingExpiry, nil
		}
		return newExpiry, nil
	default:
		return newExpiry, nil
	}
}

func addNodeWithModifier(nodes *[]Node, node Node, mod TempModifier, now time.Time) (Node, error) {
	node.Key = strings.ToLower(strings.TrimSpace(node.Key))
	node.Contexts = cloneContextMap(node.Contexts)
	if node.Expiry <= 0 {
		for i, existing := range *nodes {
			if existing.Key == node.Key && SameContexts(existing.Contexts, node.Contexts) {
				(*nodes)[i] = node
				return node, nil
			}
		}
		*nodes = append(*nodes, node)
		return node, nil
	}

	for i, existing := range *nodes {
		if existing.Key == node.Key && SameContexts(existing.Contexts, node.Contexts) {
			expired := existing.IsExpired(now)
			if existing.Expiry <= 0 && !expired && mod == TempModifierDeny {
				return Node{}, ErrTempEntryExists
			}
			exp, err := resolveExpiryWithModifier(existing.Expiry, node.Expiry, expired, mod, now)
			if err != nil {
				return Node{}, err
			}
			node.Expiry = exp
			(*nodes)[i] = node
			return node, nil
		}
	}
	*nodes = append(*nodes, node)
	return node, nil
}

func removeTempNode(nodes *[]Node, key string, contexts map[string]string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for i, existing := range *nodes {
		if existing.Expiry > 0 && existing.Key == key && SameContexts(existing.Contexts, contexts) {
			*nodes = append((*nodes)[:i], (*nodes)[i+1:]...)
			return true
		}
	}
	return false
}

func addParentWithModifier(parents *[]Inheritance, inh Inheritance, mod TempModifier, now time.Time) (Inheritance, error) {
	inh.Group = strings.ToLower(strings.TrimSpace(inh.Group))
	inh.Contexts = cloneContextMap(inh.Contexts)
	if inh.Expiry <= 0 {
		for i, existing := range *parents {
			if existing.Group == inh.Group && SameContexts(existing.Contexts, inh.Contexts) {
				(*parents)[i] = inh
				return inh, nil
			}
		}
		*parents = append(*parents, inh)
		return inh, nil
	}

	for i, existing := range *parents {
		if existing.Group == inh.Group && SameContexts(existing.Contexts, inh.Contexts) {
			expired := existing.IsExpired(now)
			if existing.Expiry <= 0 && !expired && mod == TempModifierDeny {
				return Inheritance{}, ErrTempEntryExists
			}
			exp, err := resolveExpiryWithModifier(existing.Expiry, inh.Expiry, expired, mod, now)
			if err != nil {
				return Inheritance{}, err
			}
			inh.Expiry = exp
			(*parents)[i] = inh
			return inh, nil
		}
	}
	*parents = append(*parents, inh)
	return inh, nil
}

func removeTempParent(parents *[]Inheritance, groupName string, contexts map[string]string) bool {
	groupName = strings.ToLower(strings.TrimSpace(groupName))
	for i, existing := range *parents {
		if existing.Expiry > 0 && existing.Group == groupName && SameContexts(existing.Contexts, contexts) {
			*parents = append((*parents)[:i], (*parents)[i+1:]...)
			return true
		}
	}
	return false
}

func addAffixWithModifier(items *[]PriorityMeta, pm PriorityMeta, mod TempModifier, now time.Time) (PriorityMeta, error) {
	pm.Contexts = cloneContextMap(pm.Contexts)
	for i, existing := range *items {
		if existing.Priority == pm.Priority && SameContexts(existing.Contexts, pm.Contexts) {
			if pm.Expiry <= 0 {
				(*items)[i] = pm
				return pm, nil
			}
			expired := existing.IsExpired(now)
			if existing.Expiry <= 0 && !expired && mod == TempModifierDeny {
				return PriorityMeta{}, ErrTempEntryExists
			}
			exp, err := resolveExpiryWithModifier(existing.Expiry, pm.Expiry, expired, mod, now)
			if err != nil {
				return PriorityMeta{}, err
			}
			pm.Expiry = exp
			(*items)[i] = pm
			return pm, nil
		}
	}
	*items = append(*items, pm)
	return pm, nil
}

func setAffixInContext(items *[]PriorityMeta, pm PriorityMeta) {
	pm.Contexts = cloneContextMap(pm.Contexts)
	kept := make([]PriorityMeta, 0, len(*items)+1)
	for _, existing := range *items {
		if SameContexts(existing.Contexts, pm.Contexts) {
			continue
		}
		kept = append(kept, existing)
	}
	kept = append(kept, pm)
	*items = kept
}

func removeAffixMatch(items *[]PriorityMeta, priority int, value string, contexts map[string]string, tempOnly bool) bool {
	removed := false
	kept := make([]PriorityMeta, 0, len(*items))
	for _, existing := range *items {
		if existing.Priority == priority &&
			(!tempOnly || existing.Expiry > 0) &&
			(value == "" || existing.Value == value) &&
			(len(contexts) == 0 || SameContexts(existing.Contexts, contexts)) {
			removed = true
			continue
		}
		kept = append(kept, existing)
	}
	*items = kept
	return removed
}

func setMetaEntry(
	metaMap *map[string]string,
	entries *[]MetaEntry,
	entry MetaEntry,
	mod TempModifier,
	now time.Time,
) (MetaEntry, error) {
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Contexts = cloneContextMap(entry.Contexts)
	if *metaMap == nil {
		*metaMap = make(map[string]string)
	}

	if entry.Expiry <= 0 && len(entry.Contexts) == 0 {
		(*metaMap)[entry.Key] = entry.Value
		// Keep any permanent global entry aligned or updated inside entries too.
		for i, existing := range *entries {
			if strings.EqualFold(existing.Key, entry.Key) && len(existing.Contexts) == 0 && existing.Expiry <= 0 {
				(*entries)[i] = entry
				return entry, nil
			}
		}
		*entries = append(*entries, entry)
		return entry, nil
	}

	for i, existing := range *entries {
		if strings.EqualFold(existing.Key, entry.Key) && SameContexts(existing.Contexts, entry.Contexts) {
			if entry.Expiry > 0 {
				expired := existing.IsExpired(now)
				if existing.Expiry <= 0 && !expired && mod == TempModifierDeny {
					return MetaEntry{}, ErrTempEntryExists
				}
				exp, err := resolveExpiryWithModifier(existing.Expiry, entry.Expiry, expired, mod, now)
				if err != nil {
					return MetaEntry{}, err
				}
				entry.Expiry = exp
			}
			(*entries)[i] = entry
			return entry, nil
		}
	}
	*entries = append(*entries, entry)
	return entry, nil
}

func unsetMetaEntry(metaMap *map[string]string, entries *[]MetaEntry, key string, contexts map[string]string, tempOnly bool) bool {
	key = strings.TrimSpace(key)
	removed := false

	if !tempOnly && len(contexts) == 0 && *metaMap != nil {
		if _, ok := (*metaMap)[key]; ok {
			delete(*metaMap, key)
			removed = true
		} else {
			for k := range *metaMap {
				if strings.EqualFold(k, key) {
					delete(*metaMap, k)
					removed = true
					break
				}
			}
		}
	}

	kept := make([]MetaEntry, 0, len(*entries))
	for _, existing := range *entries {
		if strings.EqualFold(existing.Key, key) &&
			(!tempOnly || existing.Expiry > 0) &&
			(len(contexts) == 0 && !tempOnly || SameContexts(existing.Contexts, contexts)) {
			removed = true
			continue
		}
		kept = append(kept, existing)
	}
	*entries = kept
	return removed
}

func cleanExpiredCommon(
	nodes *[]Node,
	parents *[]Inheritance,
	prefixes *[]PriorityMeta,
	suffixes *[]PriorityMeta,
	entries *[]MetaEntry,
	now time.Time,
) int {
	cleaned := 0

	activeNodes := make([]Node, 0, len(*nodes))
	for _, n := range *nodes {
		if n.IsExpired(now) {
			cleaned++
		} else {
			activeNodes = append(activeNodes, n)
		}
	}
	*nodes = activeNodes

	activeParents := make([]Inheritance, 0, len(*parents))
	for _, p := range *parents {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activeParents = append(activeParents, p)
		}
	}
	*parents = activeParents

	activePrefixes := make([]PriorityMeta, 0, len(*prefixes))
	for _, p := range *prefixes {
		if p.IsExpired(now) {
			cleaned++
		} else {
			activePrefixes = append(activePrefixes, p)
		}
	}
	*prefixes = activePrefixes

	activeSuffixes := make([]PriorityMeta, 0, len(*suffixes))
	for _, s := range *suffixes {
		if s.IsExpired(now) {
			cleaned++
		} else {
			activeSuffixes = append(activeSuffixes, s)
		}
	}
	*suffixes = activeSuffixes

	if len(*entries) > 0 {
		activeEntries := make([]MetaEntry, 0, len(*entries))
		for _, m := range *entries {
			if m.IsExpired(now) {
				cleaned++
			} else {
				activeEntries = append(activeEntries, m)
			}
		}
		*entries = activeEntries
	}

	return cleaned
}

func cloneMetaEntries(src []MetaEntry) []MetaEntry {
	if len(src) == 0 {
		return make([]MetaEntry, 0)
	}
	out := make([]MetaEntry, 0, len(src))
	for _, m := range src {
		cp := m
		cp.Contexts = cloneContextMap(m.Contexts)
		out = append(out, cp)
	}
	return out
}
