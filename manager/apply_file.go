package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func deriveImportDir(store any) string {
	if s, ok := store.(interface{ BaseDir() string }); ok {
		if bd := strings.TrimSpace(s.BaseDir()); bd != "" {
			return filepath.Join(bd, "imports")
		}
	}
	return filepath.Join("config", "stonyperms", "imports")
}

// SetImportDir configures where /sp applyedits file reads .spedit payloads.
func (m *Manager) SetImportDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.importDir = strings.TrimSpace(dir)
}

// ImportDir returns the configured .spedit import directory.
func (m *Manager) ImportDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.importDir
}

// ApplyEditFile reads a .spedit code from the import directory and applies it.
func (m *Manager) ApplyEditFile(actor, name string) (EditorApplySummary, error) {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return EditorApplySummary{}, fmt.Errorf("file name is required")
	}
	clean := filepath.Base(raw)
	if clean != raw || clean == "." || clean == ".." || strings.ContainsAny(clean, `/\`) {
		return EditorApplySummary{}, fmt.Errorf("invalid .spedit file name")
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".spedit") {
		clean += ".spedit"
	}
	path := filepath.Join(m.ImportDir(), clean)
	info, err := os.Stat(path)
	if err != nil {
		return EditorApplySummary{}, fmt.Errorf("could not read %s: %w", clean, err)
	}
	if info.Size() > maxPortableCodeBytes*2 {
		return EditorApplySummary{}, fmt.Errorf("%s is too large", clean)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EditorApplySummary{}, fmt.Errorf("could not read %s: %w", clean, err)
	}
	return m.ApplyEditCode(actor, strings.TrimSpace(string(data)))
}
