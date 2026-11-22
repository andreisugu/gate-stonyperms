// Package api exposes a scoped HTTP JSON API for StonyPerms. Read endpoints
// serve resolved snapshots and metadata; optional write endpoints mutate users,
// groups, and tracks when api_write_enabled is explicitly enabled with a
// separate write token tier.
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
	"strconv"
	"strings"
	"sync/atomic"
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
	maxTokensPerTier     = 4
	maxRequestBodyBytes  = 64 << 10
	maxContextEntries    = 16
	maxContextKeyLength  = 32
	maxContextValueBytes = 128
	maxPermissionLength  = 256
	maxBatchPlayers      = 100
	defaultUsersLimit    = 50
	maxUsersLimit        = 200
	defaultRateLimitRPS  = 60.0
	defaultRateLimitBrst = 120
	defaultWriteRPS      = 15.0
	defaultWriteBurst    = 30
	defaultSSEMaxClients = 32
	shutdownTimeout      = 5 * time.Second
)

var (
	contextKeyPattern  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	validBearerPrefix  = "bearer "
	loopbackHosts      = map[string]bool{"localhost": true}
	defaultSnapshotTTL = 30 * time.Second
)

// Options configures the StonyPerms HTTP API server.
type Options struct {
	// Listen is a TCP listen address, for example "127.0.0.1:8737".
	Listen string
	// Token is the legacy single read bearer token. Prefer Tokens or LoadTokens.
	Token string
	// Tokens is the read-tier bearer token list (1..4 tokens for zero-downtime rotation).
	Tokens []string
	// WriteEnabled gates mutating /v1/* endpoints and admin routes (fail-closed).
	WriteEnabled bool
	// WriteTokens is the write-tier bearer token list (1..4 tokens, distinct from read tokens).
	WriteTokens []string
	// RateLimitPerSec and RateLimitBurst configure per-IP read-tier rate limiting.
	// Negative values disable rate limiting; zero uses defaults (60 rps, burst 120).
	RateLimitPerSec float64
	RateLimitBurst  int
	// WriteRateLimitPerSec and WriteRateLimitBurst configure per-IP write-tier rate limiting.
	// Negative values disable rate limiting; zero uses defaults (15 rps, burst 30).
	WriteRateLimitPerSec float64
	WriteRateLimitBurst  int
	// SSEEnabled enables GET /v1/events streaming.
	SSEEnabled bool
	// SSEMaxClients caps concurrent SSE subscribers (default 32).
	SSEMaxClients int
	// AllowRemote permits binding to a non-loopback address. Authentication is
	// still required; remote deployments should use TLS and a private network.
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

// Server is a scoped read/write StonyPerms HTTP API.
type Server struct {
	mgr          *manager.Manager
	opts         Options
	readTokens   [][]byte
	writeTokens  [][]byte
	readLimiter  *ipRateLimiter
	writeLimiter *ipRateLimiter
	idemp        *idempotencyCache
	sseClients   atomic.Int32
	sseKeepalive time.Duration
	handler      http.Handler
	http         *http.Server
	log          logr.Logger
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

	readTokens, err := normalizeTokens(opts.Token, opts.Tokens, "read")
	if err != nil {
		return nil, err
	}

	var writeTokens [][]byte
	if opts.WriteEnabled {
		writeTokens, err = normalizeTokens("", opts.WriteTokens, "write")
		if err != nil {
			return nil, err
		}
		if tokensOverlap(readTokens, writeTokens) {
			return nil, errors.New("api: write tokens must be distinct from read tokens")
		}
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

	if opts.RateLimitPerSec == 0 {
		opts.RateLimitPerSec = defaultRateLimitRPS
	}
	if opts.RateLimitBurst == 0 {
		opts.RateLimitBurst = defaultRateLimitBrst
	}
	if opts.WriteRateLimitPerSec == 0 {
		opts.WriteRateLimitPerSec = defaultWriteRPS
	}
	if opts.WriteRateLimitBurst == 0 {
		opts.WriteRateLimitBurst = defaultWriteBurst
	}
	if opts.SSEMaxClients <= 0 {
		opts.SSEMaxClients = defaultSSEMaxClients
	}

	s := &Server{
		mgr:          mgr,
		opts:         opts,
		readTokens:   readTokens,
		writeTokens:  writeTokens,
		readLimiter:  newIPRateLimiter(opts.RateLimitPerSec, opts.RateLimitBurst),
		writeLimiter: newIPRateLimiter(opts.WriteRateLimitPerSec, opts.WriteRateLimitBurst),
		idemp:        newIdempotencyCache(maxIdempotencyEntries, idempotencyTTL),
		sseKeepalive: defaultSSEKeepalive,
		log:          opts.Logger.WithName("stonyperms-api"),
	}
	s.handler = s.routes()
	return s, nil
}

func normalizeTokens(legacy string, extra []string, tier string) ([][]byte, error) {
	seen := make(map[string]struct{}, maxTokensPerTier)
	out := make([][]byte, 0, len(extra)+1)
	add := func(raw string) error {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return nil
		}
		if len(tok) < minTokenLength {
			return fmt.Errorf("api: %s token must be at least %d characters", tier, minTokenLength)
		}
		if _, dup := seen[tok]; dup {
			return nil
		}
		seen[tok] = struct{}{}
		if len(out) >= maxTokensPerTier {
			return fmt.Errorf("api: at most %d %s tokens are allowed", maxTokensPerTier, tier)
		}
		out = append(out, []byte(tok))
		return nil
	}

	if err := add(legacy); err != nil {
		return nil, err
	}
	for _, tok := range extra {
		if err := add(tok); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("api: at least one %s token (>= %d characters) is required", tier, minTokenLength)
	}
	return out, nil
}

func tokensOverlap(a, b [][]byte) bool {
	overlap := 0
	for _, x := range a {
		for _, y := range b {
			overlap |= subtle.ConstantTimeCompare(x, y)
		}
	}
	return overlap == 1
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
		"write_enabled", s.opts.WriteEnabled,
		"sse_enabled", s.opts.SSEEnabled,
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
	s.registerWebRoutes(mux)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Read-tier routes
	mux.Handle("GET /v1/health", s.readRoute(s.handleHealth))
	mux.Handle("GET /v1/revision", s.readRoute(s.handleRevision))
	mux.Handle("GET /v1/events", s.readRoute(s.handleEvents))
	mux.Handle("GET /v1/players/{uuid}", s.readRoute(s.handlePlayer))
	mux.Handle("POST /v1/players/batch", s.readRoute(s.handlePlayersBatch))
	mux.Handle("POST /v1/check", s.readRoute(s.handleCheck))

	mux.Handle("GET /v1/users", s.readRoute(s.handleUsers))
	mux.Handle("GET /v1/users/by-name/{username}", s.readRoute(s.handleUserByName))
	mux.Handle("GET /v1/users/{id}", s.readRoute(s.handleUser))

	mux.Handle("GET /v1/groups", s.readRoute(s.handleGroups))
	mux.Handle("GET /v1/groups/{name}", s.readRoute(s.handleGroup))
	mux.Handle("GET /v1/groups/{name}/members", s.readRoute(s.handleGroupMembers))

	mux.Handle("GET /v1/tracks", s.readRoute(s.handleTracks))
	mux.Handle("GET /v1/tracks/{name}", s.readRoute(s.handleTrack))
	mux.Handle("GET /v1/search", s.readRoute(s.handleSearch))
	mux.Handle("POST /v1/backends/catalog", s.readRoute(s.handleBackendCatalog))

	// Write-tier user routes
	mux.Handle("POST /v1/users", s.writeRoute(s.handleEnsureUser, true))
	mux.Handle("DELETE /v1/users/{id}", s.writeRoute(s.handleDeleteUser, true))
	mux.Handle("POST /v1/users/{id}/parents", s.writeRoute(s.handleUserAddParent, true))
	mux.Handle("DELETE /v1/users/{id}/parents/{group}", s.writeRoute(s.handleUserRemoveParent, true))
	mux.Handle("PUT /v1/users/{id}/primary-group", s.writeRoute(s.handleUserPrimaryGroup, true))
	mux.Handle("POST /v1/users/{id}/permissions", s.writeRoute(s.handleSubjectSetPermission(true), true))
	mux.Handle("DELETE /v1/users/{id}/permissions", s.writeRoute(s.handleSubjectDeletePermission(true), true))
	mux.Handle("PUT /v1/users/{id}/meta/{key}", s.writeRoute(s.handleSubjectSetMeta(true), true))
	mux.Handle("DELETE /v1/users/{id}/meta/{key}", s.writeRoute(s.handleSubjectDeleteMeta(true), true))
	mux.Handle("POST /v1/users/{id}/prefixes", s.writeRoute(s.handleSubjectSetAffix(true, true), true))
	mux.Handle("DELETE /v1/users/{id}/prefixes/{priority}", s.writeRoute(s.handleSubjectDeleteAffix(true, true), true))
	mux.Handle("POST /v1/users/{id}/suffixes", s.writeRoute(s.handleSubjectSetAffix(true, false), true))
	mux.Handle("DELETE /v1/users/{id}/suffixes/{priority}", s.writeRoute(s.handleSubjectDeleteAffix(true, false), true))
	mux.Handle("POST /v1/users/{id}/promote", s.writeRoute(s.handleUserTrackAction(true), true))
	mux.Handle("POST /v1/users/{id}/demote", s.writeRoute(s.handleUserTrackAction(false), true))
	mux.Handle("POST /v1/users/{id}/clear", s.writeRoute(s.handleUserClear, true))
	mux.Handle("POST /v1/users/{id}/batch", s.writeRoute(s.handleUserBatch, true))

	// Write-tier group routes
	mux.Handle("POST /v1/groups", s.writeRoute(s.handleCreateGroup, true))
	mux.Handle("PATCH /v1/groups/{name}", s.writeRoute(s.handlePatchGroup, true))
	mux.Handle("DELETE /v1/groups/{name}", s.writeRoute(s.handleDeleteGroup, true))
	mux.Handle("POST /v1/groups/{name}/permissions", s.writeRoute(s.handleSubjectSetPermission(false), true))
	mux.Handle("DELETE /v1/groups/{name}/permissions", s.writeRoute(s.handleSubjectDeletePermission(false), true))
	mux.Handle("POST /v1/groups/{name}/parents", s.writeRoute(s.handleGroupAddParent, true))
	mux.Handle("DELETE /v1/groups/{name}/parents", s.writeRoute(s.handleGroupRemoveParent, true))
	mux.Handle("DELETE /v1/groups/{name}/parents/{parent}", s.writeRoute(s.handleGroupRemoveParent, true))
	mux.Handle("PUT /v1/groups/{name}/meta/{key}", s.writeRoute(s.handleSubjectSetMeta(false), true))
	mux.Handle("DELETE /v1/groups/{name}/meta/{key}", s.writeRoute(s.handleSubjectDeleteMeta(false), true))
	mux.Handle("POST /v1/groups/{name}/prefixes", s.writeRoute(s.handleSubjectSetAffix(false, true), true))
	mux.Handle("DELETE /v1/groups/{name}/prefixes", s.writeRoute(s.handleSubjectDeleteAffix(false, true), true))
	mux.Handle("DELETE /v1/groups/{name}/prefixes/{priority}", s.writeRoute(s.handleSubjectDeleteAffix(false, true), true))
	mux.Handle("POST /v1/groups/{name}/suffixes", s.writeRoute(s.handleSubjectSetAffix(false, false), true))
	mux.Handle("DELETE /v1/groups/{name}/suffixes", s.writeRoute(s.handleSubjectDeleteAffix(false, false), true))
	mux.Handle("DELETE /v1/groups/{name}/suffixes/{priority}", s.writeRoute(s.handleSubjectDeleteAffix(false, false), true))

	// Write-tier track & admin routes
	mux.Handle("POST /v1/tracks", s.writeRoute(s.handleCreateTrack, true))
	mux.Handle("PUT /v1/tracks/{name}", s.writeRoute(s.handlePutTrack, true))
	mux.Handle("DELETE /v1/tracks/{name}", s.writeRoute(s.handleDeleteTrack, true))
	mux.Handle("GET /v1/logs", s.writeRoute(s.handleLogs, false))
	mux.Handle("POST /v1/reload", s.writeRoute(s.handleReload, true))
	mux.Handle("POST /v1/bulkupdate", s.writeRoute(s.handleBulkUpdate, true))
	mux.Handle("POST /v1/users/{id}/tracks/{track}", s.writeRoute(s.handleUserTrackSet, true))
	mux.Handle("DELETE /v1/users/{id}/tracks/{track}", s.writeRoute(s.handleUserTrackClear, true))
	mux.Handle("POST /v1/groups/{name}/tracks/{track}", s.writeRoute(s.handleGroupTrackSet, true))
	mux.Handle("DELETE /v1/groups/{name}/tracks/{track}", s.writeRoute(s.handleGroupTrackClear, true))

	return securityHeaders(mux)
}

func (s *Server) readRoute(fn http.HandlerFunc) http.Handler {
	return s.rateLimit(s.readLimiter, s.requireRead(fn))
}

func (s *Server) writeRoute(fn http.HandlerFunc, idempotent bool) http.Handler {
	var h http.Handler = fn
	if idempotent {
		h = s.withIdempotency(h)
	}
	return s.rateLimit(s.writeLimiter, s.requireWrite(h))
}

func extractBearerToken(r *http.Request) []byte {
	header := r.Header.Get("Authorization")
	if len(header) > len(validBearerPrefix) && strings.EqualFold(header[:len(validBearerPrefix)], validBearerPrefix) {
		return []byte(strings.TrimSpace(header[len(validBearerPrefix):]))
	}
	return nil
}

func matchesAnyToken(got []byte, valid [][]byte) bool {
	if len(got) == 0 {
		return false
	}
	matched := 0
	for _, tok := range valid {
		matched |= subtle.ConstantTimeCompare(got, tok)
	}
	return matched == 1
}

func (s *Server) requireRead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := extractBearerToken(r)
		isRead := matchesAnyToken(got, s.readTokens)
		isWrite := s.opts.WriteEnabled && matchesAnyToken(got, s.writeTokens)
		if !isRead && !isWrite {
			w.Header().Set("WWW-Authenticate", `Bearer realm="stonyperms"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := extractBearerToken(r)
		isRead := matchesAnyToken(got, s.readTokens)
		isWrite := s.opts.WriteEnabled && matchesAnyToken(got, s.writeTokens)
		if !isRead && !isWrite {
			w.Header().Set("WWW-Authenticate", `Bearer realm="stonyperms"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		if !s.opts.WriteEnabled {
			writeError(w, http.StatusForbidden, "writes_disabled", "HTTP API write endpoints are disabled")
			return
		}
		if !isWrite {
			writeError(w, http.StatusForbidden, "insufficient_scope", "a write-tier bearer token is required")
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
		WriteEnabled:  s.opts.WriteEnabled,
		SSEEnabled:    s.opts.SSEEnabled,
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

	s.writePlayerSnapshot(w, r, id, contexts)
}

func (s *Server) writePlayerSnapshot(w http.ResponseWriter, r *http.Request, id uuid.UUID, contexts map[string]string) {
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

func (s *Server) handlePlayersBatch(w http.ResponseWriter, r *http.Request) {
	var req playersBatchRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if len(req.UUIDs) > maxBatchPlayers {
		writeError(w, http.StatusBadRequest, "invalid_batch", "at most 100 uuids are allowed per batch")
		return
	}
	if err := validateContexts(req.Contexts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}

	ids := make([]uuid.UUID, 0, len(req.UUIDs))
	for _, raw := range req.UUIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || parsed == uuid.Nil {
			writeError(w, http.StatusBadRequest, "invalid_uuid", "every entry in uuids must be a valid non-nil UUID")
			return
		}
		ids = append(ids, parsed)
	}

	effs, foundMap, rev := s.mgr.EffectiveUsersBatch(ids, req.Contexts, s.opts.DefaultGroup)
	players := make(map[string]snapshotResponse, len(effs))
	for _, id := range ids {
		players[id.String()] = newSnapshotResponse(effs[id], foundMap[id], rev, s.opts.SnapshotTTL)
	}

	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, playersBatchResponse{
		SchemaVersion: SchemaVersion,
		Revision:      rev,
		Players:       players,
	})
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

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	groupFilter := strings.TrimSpace(q.Get("group"))

	limit := defaultUsersLimit
	if rawLimit := strings.TrimSpace(q.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "limit must be a positive integer")
			return
		}
		if parsed > maxUsersLimit {
			parsed = maxUsersLimit
		}
		limit = parsed
	}

	offset := 0
	if rawOffset := strings.TrimSpace(q.Get("offset")); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "offset must be a non-negative integer")
			return
		}
		offset = parsed
	}

	users, total, rev := s.mgr.ListUsers(groupFilter, offset, limit)
	summaries := make([]userSummaryDTO, 0, len(users))
	for _, u := range users {
		summaries = append(summaries, userSummaryDTO{
			UUID:         u.UUID.String(),
			Username:     u.Username,
			PrimaryGroup: u.PrimaryGroup,
			Parents:      u.Parents,
			NodeCount:    len(u.Nodes),
		})
	}
	writeJSON(w, http.StatusOK, usersResponse{
		Revision: rev,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
		Users:    summaries,
	})
}

func (s *Server) handleUserByName(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "" {
		writeError(w, http.StatusBadRequest, "invalid_username", "username cannot be empty")
		return
	}
	u, ok, rev := s.mgr.ResolveUserByName(username)
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
		return
	}

	q := r.URL.Query()
	if strings.EqualFold(strings.TrimSpace(q.Get("effective")), "true") {
		q.Del("effective")
		contexts, err := parseContexts(q)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
			return
		}
		s.writePlayerSnapshot(w, r, u.UUID, contexts)
		return
	}

	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	idQuery := strings.TrimSpace(r.PathValue("id"))
	if idQuery == "" {
		writeError(w, http.StatusBadRequest, "invalid_user", "user identifier cannot be empty")
		return
	}
	u, ok, rev := s.mgr.ResolveUserByName(idQuery)
	if !ok {
		writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
		return
	}
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
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

func (s *Server) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name cannot be empty")
		return
	}
	members, ok, revision := s.mgr.GroupMembersSnapshot(name)
	if !ok {
		writeError(w, http.StatusNotFound, "group_not_found", "group does not exist")
		return
	}
	writeJSON(w, http.StatusOK, groupMembersResponse{
		Revision: revision,
		Group:    name,
		Members:  members,
	})
}

func (s *Server) handleTracks(w http.ResponseWriter, _ *http.Request) {
	tracks, revision := s.mgr.TracksSnapshot()
	writeJSON(w, http.StatusOK, tracksResponse{
		Revision: revision,
		Tracks:   tracks,
	})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_track", "track name cannot be empty")
		return
	}
	track, ok, revision := s.mgr.TrackSnapshot(name)
	if !ok {
		writeError(w, http.StatusNotFound, "track_not_found", "track does not exist")
		return
	}
	writeJSON(w, http.StatusOK, trackResponse{Revision: revision, Track: track})
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

// LoadTokens resolves one or more bearer tokens (newline-separated for zero-downtime
// rotation) from, in order: envVar, tokenFile, then inline. Blank lines and `#`
// comments are ignored, tokens are deduplicated, and at most 4 tokens are returned.
func LoadTokens(inline, tokenFile, envVar string) ([]string, error) {
	if envVar != "" {
		if raw := os.Getenv(envVar); strings.TrimSpace(raw) != "" {
			tokens, err := parseTokenLines(raw, "environment variable "+envVar)
			if err != nil {
				return nil, err
			}
			return tokens, nil
		}
	}
	if strings.TrimSpace(tokenFile) != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("api: read token file: %w", err)
		}
		tokens, err := parseTokenLines(string(data), "token file")
		if err != nil {
			return nil, err
		}
		return tokens, nil
	}
	if strings.TrimSpace(inline) != "" {
		return parseTokenLines(inline, "inline token")
	}
	return nil, errors.New("api: no token configured")
}

func parseTokenLines(raw, source string) ([]string, error) {
	lines := strings.Split(raw, "\n")
	seen := make(map[string]struct{}, maxTokensPerTier)
	tokens := make([]string, 0, len(lines))
	for _, line := range lines {
		tok := strings.TrimSpace(line)
		if tok == "" || strings.HasPrefix(tok, "#") {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		if len(tokens) >= maxTokensPerTier {
			return nil, fmt.Errorf("api: %s exceeds maximum of %d tokens", source, maxTokensPerTier)
		}
		tokens = append(tokens, tok)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("api: %s is empty", source)
	}
	return tokens, nil
}

// LoadToken resolves the primary bearer token from, in order: the STONYPERMS_API_TOKEN
// environment variable, tokenFile, then inline. It never logs or returns the
// token in error messages.
func LoadToken(inline, tokenFile string) (string, error) {
	tokens, err := LoadTokens(inline, tokenFile, "STONYPERMS_API_TOKEN")
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "query parameter q is required")
		return
	}
	res := s.mgr.Search(q)
	rev := s.mgr.Revision()
	writeJSON(w, http.StatusOK, searchResponse{
		Revision:  rev,
		Query:     res.Query,
		GroupHits: res.GroupHits,
		UserHits:  res.UserHits,
	})
}
