package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type backendCatalogRequest struct {
	Server      string              `json:"server"`
	Permissions []string            `json:"permissions"`
	Contexts    map[string][]string `json:"contexts"`
}

type backendCatalogResponse struct {
	OK          bool                `json:"ok"`
	Server      string              `json:"server"`
	Permissions int                 `json:"permissions"`
	Contexts    map[string][]string `json:"contexts,omitempty"`
}

// handleBackendCatalog accepts a non-authoritative permission/context vocabulary
// push from a Paper/Fabric bridge. It only feeds autocomplete and the tree viewer.
func (s *Server) handleBackendCatalog(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req backendCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid catalog payload")
		return
	}
	server := strings.TrimSpace(req.Server)
	if server == "" {
		writeError(w, http.StatusBadRequest, "invalid_server", "server is required")
		return
	}
	perms, _ := s.mgr.RegisterBackendCatalog(server, req.Permissions, req.Contexts)
	_, contexts := s.mgr.BackendCatalogSnapshot()
	writeJSON(w, http.StatusOK, backendCatalogResponse{
		OK:          true,
		Server:      server,
		Permissions: perms,
		Contexts:    contexts,
	})
}
