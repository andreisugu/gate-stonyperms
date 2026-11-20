// Package api exposes a read-only HTTP JSON view of StonyPerms for trusted
// backend adapters (Paper/Fabric). It never mutates permissions data.
package api

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/manager"
)

// SchemaVersion is the version of the HTTP contract. It is bumped only for
// breaking changes.
const SchemaVersion = 1

const (
	minTokenLength       = 16
	maxRequestBodyBytes  = 64 << 10
	maxContextEntries    = 16
	maxContextKeyLength  = 32
	maxContextValueBytes = 128
	maxPermissionLength  = 256
	shutdownTimeout      = 5 * time.Second
)

var (
	contextKeyPattern  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	validBearerPrefix  = "bearer "
	loopbackHosts      = map[string]bool{"localhost": true}
	defaultSnapshotTTL = 30 * time.Second
)

// Options configures the read-only API server.
type Options struct {
	// Listen is a TCP listen address, for example "127.0.0.1:8737".
	Listen string
	// Token is the shared bearer token. It must also be supplied via
	// LoadToken so that file/env indirection is supported.
	Token string
	// AllowRemote permits binding to a non-loopback address. It does not
	// weaken authentication, but remote deployments should use TLS and a
	// private network (tailnet/WireGuard).
	AllowRemote bool
	// TLSCertFile and TLSKeyFile enable HTTPS when both are set.
	TLSCertFile string
	TLSKeyFile  string
	// SnapshotTTL is advisory cache lifetime returned to clients.
	SnapshotTTL time.Duration
	// DefaultGroup is used for users StonyPerms has not stored yet.
	DefaultGroup string
	Logger       logr.Logger
}

// Server is a read-only StonyPerms HTTP API.
type Server struct {
	mgr     *manager.Manager
	opts    Options
	token   []byte
	handler http.Handler
	http    *http.Server
	log     logr.Logger
}

// New validates options and constructs a server. No socket is opened until
// Start is called.
func New(mgr *manager.Manager, opts Options) (*Server, error) {
	if mgr == nil {
		return nil, errors.New("api: manager is required")
	}
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:8737"
	}
	if err := validateListen(opts.Listen, opts.AllowRemote); err != nil {
		return nil, err
	}

	token := strings.TrimSpace(opts.Token)
	if len(token) < minTokenLength {
		return nil, fmt.Errorf("api: token must be at least %d characters", minTokenLength)
	}

	if (opts.TLSCertFile == "") != (opts.TLSKeyFile == "") {
		return nil, errors.New("api: TLS cert and key must be configured together")
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = defaultSnapshotTTL
	}
	if opts.DefaultGroup == "" {
		opts.DefaultGroup = "default"
	}
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	s := &Server{
		mgr:   mgr,
		opts:  opts,
		token: []byte(token),
		log:   opts.Logger.WithName("stonyperms-api"),
	}
	s.handler = s.routes()
	return s, nil
}

// Handler returns the authenticated HTTP handler. It is exposed for tests and
// for embedding behind another listener.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Start binds the configured address and serves in the background. The context
// controls graceful shutdown; canceling it stops the server.
func (s *Server) Start(ctx context.Context) error {
	var tlsConfig *tls.Config
	if s.opts.TLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(s.opts.TLSCertFile, s.opts.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("api: load TLS certificate: %w", err)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	ln, err := net.Listen("tcp", s.opts.Listen)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", s.opts.Listen, err)
	}

	s.http = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	go func() {
		defer ln.Close()
		var serveErr error
		if tlsConfig != nil {
			serveErr = s.http.Serve(tls.NewListener(ln, tlsConfig))
		} else {
			serveErr = s.http.Serve(ln)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.log.Error(serveErr, "HTTP API stopped unexpectedly")
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			s.log.Error(err, "HTTP API shutdown failed")
		}
	}()

	s.log.Info("HTTP API listening",
		"addr", ln.Addr().String(),
		"tls", s.opts.TLSCertFile != "",
		"remote_allowed", s.opts.AllowRemote,
	)
	return nil
}

// Close stops the server. It is safe to call more than once.
func (s *Server) Close(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /v1/health", s.authenticated(http.HandlerFunc(s.handleHealth)))
	mux.Handle("GET /v1/revision", s.authenticated(http.HandlerFunc(s.handleRevision)))
	mux.Handle("GET /v1/players/{uuid}", s.authenticated(http.HandlerFunc(s.handlePlayer)))
	mux.Handle("POST /v1/check", s.authenticated(http.HandlerFunc(s.handleCheck)))
	mux.Handle("GET /v1/groups", s.authenticated(http.HandlerFunc(s.handleGroups)))
	mux.Handle("GET /v1/groups/{name}", s.authenticated(http.HandlerFunc(s.handleGroup)))
	return securityHeaders(mux)
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		var got string
		if len(header) > len(validBearerPrefix) && strings.EqualFold(header[:len(validBearerPrefix)], validBearerPrefix) {
			got = strings.TrimSpace(header[len(validBearerPrefix):])
		}
		if subtle.ConstantTimeCompare([]byte(got), s.token) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="stonyperms"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:        "ok",
		SchemaVersion: SchemaVersion,
		Revision:      s.mgr.Revision(),
	})
}

func (s *Server) handleRevision(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, revisionResponse{Revision: s.mgr.Revision()})
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("uuid")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "uuid must be a standard UUID")
		return
	}
	contexts, err := parseContexts(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}

	eff, found, revision := s.mgr.EffectiveUser(id, contexts, s.opts.DefaultGroup)
	snapshot := newSnapshotResponse(eff, found, revision, s.opts.SnapshotTTL)
	etag := snapshotETag(s.mgr.Generation(), revision, snapshot)
	w.Header().Set("ETag", etag)
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", revision))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req checkRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be a valid check request")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must contain exactly one JSON object")
		return
	}

	id, err := uuid.Parse(strings.TrimSpace(req.UUID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_uuid", "uuid must be a standard UUID")
		return
	}
	permission := strings.TrimSpace(req.Permission)
	if permission == "" || len(permission) > maxPermissionLength {
		writeError(w, http.StatusBadRequest, "invalid_permission", "permission must be 1-256 characters")
		return
	}
	if err := validateContexts(req.Contexts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}

	value, source, revision := s.mgr.EvaluateUser(id, permission, req.Contexts, s.opts.DefaultGroup)
	writeJSON(w, http.StatusOK, checkResponse{
		Value:    value.String(),
		Source:   source,
		Revision: revision,
	})
}

func (s *Server) handleGroups(w http.ResponseWriter, _ *http.Request) {
	groups, revision := s.mgr.GroupsSnapshot()
	writeJSON(w, http.StatusOK, groupsResponse{
		Revision: revision,
		Groups:   groups,
	})
}

func (s *Server) handleGroup(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name cannot be empty")
		return
	}
	group, ok, revision := s.mgr.GroupSnapshot(name)
	if !ok {
		writeError(w, http.StatusNotFound, "group_not_found", "group does not exist")
		return
	}
	writeJSON(w, http.StatusOK, groupResponse{Revision: revision, Group: group})
}

func validateListen(addr string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("api: invalid listen address %q: %w", addr, err)
	}
	if allowRemote {
		return nil
	}
	if loopbackHosts[strings.ToLower(host)] {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("api: refusing non-loopback listen address %q without allow_remote", addr)
}

func parseContexts(values map[string][]string) (map[string]string, error) {
	contexts := make(map[string]string, len(values))
	for key, vals := range values {
		if len(vals) == 0 {
			continue
		}
		contexts[key] = vals[0]
	}
	if err := validateContexts(contexts); err != nil {
		return nil, err
	}
	return contexts, nil
}

func validateContexts(contexts map[string]string) error {
	if len(contexts) > maxContextEntries {
		return fmt.Errorf("at most %d contexts are allowed", maxContextEntries)
	}
	for key, value := range contexts {
		if !contextKeyPattern.MatchString(key) || len(key) > maxContextKeyLength {
			return fmt.Errorf("invalid context key %q", key)
		}
		if len(value) > maxContextValueBytes {
			return fmt.Errorf("context %q is too long", key)
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("context %q contains control characters", key)
			}
		}
	}
	return nil
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

// LoadToken resolves a bearer token from, in order: the STONYPERMS_API_TOKEN
// environment variable, tokenFile, then inline. It never logs or returns the
// token in error messages.
func LoadToken(inline, tokenFile string) (string, error) {
	if env := strings.TrimSpace(os.Getenv("STONYPERMS_API_TOKEN")); env != "" {
		return env, nil
	}
	if tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("api: read token file: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", errors.New("api: token file is empty")
		}
		return token, nil
	}
	token := strings.TrimSpace(inline)
	if token == "" {
		return "", errors.New("api: no token configured (set api_token_file or STONYPERMS_API_TOKEN)")
	}
	return token, nil
}
