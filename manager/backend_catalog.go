package manager

import (
	"sort"
	"strings"
	"time"
)

const (
	maxBackendPermissions = 20000
	maxBackendContextVals = 5000
	maxBackendCatalogAge  = 30 * time.Minute
)

// BackendCatalog is the permission/context vocabulary reported by one backend.
type BackendCatalog struct {
	Server      string              `json:"server"`
	Permissions []string            `json:"permissions"`
	Contexts    map[string][]string `json:"contexts"`
	UpdatedAt   int64               `json:"updatedAt"`
}

// RegisterBackendCatalog replaces one backend's reported catalog.
func (m *Manager) RegisterBackendCatalog(server string, permissions []string, contexts map[string][]string) (int, int) {
	name := strings.ToLower(strings.TrimSpace(server))
	if name == "" {
		return 0, 0
	}
	permSet := make(map[string]struct{}, len(permissions))
	for _, raw := range permissions {
		perm := strings.TrimSpace(raw)
		if perm == "" || len(perm) > maxEditorPermLen || hasControlChars(perm) {
			continue
		}
		permSet[perm] = struct{}{}
		if len(permSet) >= maxBackendPermissions {
			break
		}
	}
	perms := make([]string, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}
	sort.Strings(perms)

	ctxOut := make(map[string][]string, len(contexts))
	for rawKey, rawValues := range contexts {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" || len(key) > 64 || hasControlChars(key) {
			continue
		}
		seen := make(map[string]struct{}, len(rawValues))
		for _, rawValue := range rawValues {
			value := strings.TrimSpace(rawValue)
			if value == "" || len(value) > 128 || hasControlChars(value) {
				continue
			}
			seen[value] = struct{}{}
			if len(seen) >= maxBackendContextVals {
				break
			}
		}
		if len(seen) == 0 {
			continue
		}
		values := make([]string, 0, len(seen))
		for value := range seen {
			values = append(values, value)
		}
		sort.Strings(values)
		ctxOut[key] = values
	}

	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.backendCatalogs[name] = BackendCatalog{
		Server:      name,
		Permissions: perms,
		Contexts:    ctxOut,
		UpdatedAt:   time.Now().UTC().Unix(),
	}
	return len(perms), len(ctxOut)
}

// BackendCatalogSnapshot merges every fresh backend catalog.
func (m *Manager) BackendCatalogSnapshot() ([]string, map[string][]string) {
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := time.Now().UTC().Add(-maxBackendCatalogAge).Unix()
	permSet := make(map[string]struct{}, 256)
	ctxSets := make(map[string]map[string]struct{}, 8)
	for server, cat := range st.backendCatalogs {
		if cat.UpdatedAt < cutoff {
			delete(st.backendCatalogs, server)
			continue
		}
		for _, perm := range cat.Permissions {
			permSet[perm] = struct{}{}
		}
		for key, values := range cat.Contexts {
			set := ctxSets[key]
			if set == nil {
				set = make(map[string]struct{}, len(values))
				ctxSets[key] = set
			}
			for _, value := range values {
				set[value] = struct{}{}
			}
		}
	}
	perms := make([]string, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	contexts := make(map[string][]string, len(ctxSets))
	for key, set := range ctxSets {
		values := make([]string, 0, len(set))
		for value := range set {
			values = append(values, value)
		}
		sort.Strings(values)
		contexts[key] = values
	}
	return perms, contexts
}

// BackendCatalogs returns a copy of all live backend catalog entries.
func (m *Manager) BackendCatalogs() map[string]BackendCatalog {
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]BackendCatalog, len(st.backendCatalogs))
	for name, cat := range st.backendCatalogs {
		out[name] = cat
	}
	return out
}
