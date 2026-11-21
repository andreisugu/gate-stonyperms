package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/web"
)

const (
	maxEditorSaveBodyBytes    = 2 << 20
	maxSessionCreateBodyBytes = 512 << 10
)

type createEditorSessionRequest struct {
	Actor            string              `json:"actor"`
	CommandAlias     string              `json:"commandAlias"`
	TargetType       string              `json:"targetType"`
	Target           string              `json:"target"`
	Filter           string              `json:"filter"`
	ExtraPermissions []string            `json:"extraPermissions"`
	ExtraContexts    map[string][]string `json:"extraContexts"`
}

type createVerboseSessionRequest struct {
	Actor        string `json:"actor"`
	CommandAlias string `json:"commandAlias"`
}

type createTreeSessionRequest struct {
	Actor            string   `json:"actor"`
	CommandAlias     string   `json:"commandAlias"`
	Root             string   `json:"root"`
	ExtraPermissions []string `json:"extraPermissions"`
}

type applyEditsRequest struct {
	Code  string `json:"code"`
	Actor string `json:"actor"`
}

func (s *Server) registerWebRoutes(mux *http.ServeMux) {
	// Embedded static UI and SPA entrypoints
	mux.HandleFunc("GET /{$}", s.handleWebHTML)
	mux.HandleFunc("GET /editor", s.handleWebHTML)
	mux.HandleFunc("GET /verbose", s.handleWebHTML)
	mux.HandleFunc("GET /tree", s.handleWebHTML)
	mux.HandleFunc("GET /wiki", s.handleWebHTML)
	mux.HandleFunc("GET /favicon.svg", s.serveWebAsset("favicon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /styles.css", s.serveWebAsset("styles.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /app.js", s.serveWebAsset("app.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /demo-data.js", s.serveWebAsset("demo-data.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /wiki-data.js", s.serveWebAsset("wiki-data.js", "application/javascript; charset=utf-8"))

	// Write-tier session creation & applyedits (used by StonyPermsBridge and automation)
	mux.Handle("POST /v1/editor/sessions", s.writeRoute(s.handleCreateEditorSession, false))
	mux.Handle("POST /v1/editor/applyedits", s.writeRoute(s.handleApplyEditsAPI, false))
	mux.Handle("POST /v1/verbose/sessions", s.writeRoute(s.handleCreateVerboseSession, false))
	mux.Handle("POST /v1/tree/sessions", s.writeRoute(s.handleCreateTreeSession, false))

	// Ephemeral token-authenticated + CORS-scoped Web UI endpoints
	mux.HandleFunc("OPTIONS /v1/editor/sessions/{token}", s.handleWebCORS)
	mux.HandleFunc("OPTIONS /v1/editor/sessions/{token}/save", s.handleWebCORS)
	mux.HandleFunc("OPTIONS /v1/verbose/sessions/{token}", s.handleWebCORS)
	mux.HandleFunc("OPTIONS /v1/tree/sessions/{token}", s.handleWebCORS)

	mux.Handle("GET /v1/editor/sessions/{token}", s.corsSessionRead(s.handleGetEditorSession))
	mux.Handle("POST /v1/editor/sessions/{token}/save", s.corsSessionWrite(s.handleSaveEditorSession))
	mux.Handle("GET /v1/verbose/sessions/{token}", s.corsSessionRead(s.handleGetVerboseSession))
	mux.Handle("GET /v1/tree/sessions/{token}", s.corsSessionRead(s.handleGetTreeSession))
}

func (s *Server) handleWebHTML(w http.ResponseWriter, r *http.Request) {
	s.writeEmbeddedAsset(w, "index.html", "text/html; charset=utf-8", "no-cache")
}

func (s *Server) serveWebAsset(filename, contentType string) http.HandlerFunc {
	clean := path.Clean(filename)
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeEmbeddedAsset(w, clean, contentType, "public, max-age=300")
	}
}

func (s *Server) writeEmbeddedAsset(w http.ResponseWriter, name, contentType, cacheControl string) {
	data, err := web.FS.ReadFile(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "requested web asset does not exist")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleWebCORS(w http.ResponseWriter, _ *http.Request) {
	setWebCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) corsSessionRead(fn http.HandlerFunc) http.Handler {
	return s.rateLimit(s.readLimiter, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setWebCORSHeaders(w)
		fn(w, r)
	}))
}

func (s *Server) corsSessionWrite(fn http.HandlerFunc) http.Handler {
	return s.rateLimit(s.writeLimiter, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setWebCORSHeaders(w)
		fn(w, r)
	}))
}

func setWebCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
}

func (s *Server) handleCreateEditorSession(w http.ResponseWriter, r *http.Request) {
	var req createEditorSessionRequest
	if err := decodeStrictSizedJSON(w, r, maxSessionCreateBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := s.mgr.CreateEditorSession(manager.WebSessionCreateOptions{
		Actor:            req.Actor,
		CommandAlias:     req.CommandAlias,
		TargetType:       req.TargetType,
		Target:           req.Target,
		Filter:           req.Filter,
		ExtraPermissions: req.ExtraPermissions,
		ExtraContexts:    req.ExtraContexts,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetEditorSession(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	session, ok := s.mgr.GetEditorSession(token)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "editor session does not exist or has expired")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleSaveEditorSession(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	var payload manager.EditorSavePayload
	if err := decodeStrictSizedJSON(w, r, maxEditorSaveBodyBytes, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	summary, code, err := s.mgr.SaveEditorSession(token, payload)
	if err != nil {
		switch {
		case errors.Is(err, manager.ErrWebSessionNotFound):
			writeError(w, http.StatusNotFound, "session_not_found", err.Error())
		case errors.Is(err, manager.ErrCannotDeleteDefault):
			writeError(w, http.StatusConflict, "cannot_delete_default", err.Error())
		default:
			writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, struct {
		manager.EditorApplySummary
		ApplyCode string `json:"applyCode,omitempty"`
	}{
		EditorApplySummary: summary,
		ApplyCode:          code,
	})
}

func (s *Server) handleApplyEditsAPI(w http.ResponseWriter, r *http.Request) {
	var req applyEditsRequest
	if err := decodeStrictSizedJSON(w, r, maxSessionCreateBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "invalid_code", "code is required")
		return
	}
	summary, err := s.mgr.ApplyEditCode(req.Actor, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, manager.ErrInvalidApplyCode):
			writeError(w, http.StatusBadRequest, "invalid_code", err.Error())
		case errors.Is(err, manager.ErrCannotDeleteDefault):
			writeError(w, http.StatusConflict, "cannot_delete_default", err.Error())
		default:
			writeError(w, http.StatusBadRequest, "apply_failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleCreateVerboseSession(w http.ResponseWriter, r *http.Request) {
	var req createVerboseSessionRequest
	if err := decodeStrictSizedJSON(w, r, maxRequestBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	rec := s.mgr.Engine().StopVerboseRecording()
	created, err := s.mgr.CreateVerboseSession(rec, req.Actor, req.CommandAlias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetVerboseSession(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	session, ok := s.mgr.GetVerboseSession(token)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "verbose session does not exist or has expired")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleCreateTreeSession(w http.ResponseWriter, r *http.Request) {
	var req createTreeSessionRequest
	if err := decodeStrictSizedJSON(w, r, maxSessionCreateBodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	created, err := s.mgr.CreateTreeSession(req.Root, req.Actor, req.CommandAlias, req.ExtraPermissions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetTreeSession(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("token"))
	session, ok := s.mgr.GetTreeSession(token)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "tree session does not exist or has expired")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func decodeStrictSizedJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("body must contain a valid JSON request")
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}
