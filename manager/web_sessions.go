package manager

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
)

const (
	// DefaultPublicWebURL is the public fallback web editor origin when Gate's HTTP server is disabled.
	DefaultPublicWebURL = "https://andreisugu.github.io/gate-stonyperms"

	// PortableCodePrefix identifies client-generated gzipped+base64url editor payloads.
	PortableCodePrefix = "sp1_"
	// ShortApplyPrefix identifies Gate-hosted short apply codes.
	ShortApplyPrefix = "sp_"

	webSessionTTL        = 2 * time.Hour
	applyCodeTTL         = 1 * time.Hour
	maxWebSessions       = 128
	maxApplyCodes        = 128
	maxPortableCodeBytes = 64 << 10
	maxEditorUsers       = 500
	maxKnownPermissions  = 5000
	maxEditorContexts    = 16
	maxEditorTextLen     = 128
	maxEditorPermLen     = 256
)

var (
	// ErrWebSessionNotFound is returned when an editor/verbose/tree token is unknown or expired.
	ErrWebSessionNotFound = errors.New("web session not found or expired")
	// ErrInvalidApplyCode is returned when an applyedits code is malformed, unknown, or expired.
	ErrInvalidApplyCode = errors.New("invalid or expired applyedits code")

	editorEntityPattern = regexp.MustCompile(`^[a-z0-9_+-]{1,64}$`)
	editorCtxKeyPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

	webStateMap sync.Map // *Manager -> *managerWebState
)

type managerWebState struct {
	mu              sync.Mutex
	config          WebConfig
	onlineServers   []string
	knownPerms      []string
	editorSessions  map[string]*editorSessionEntry
	verboseSessions map[string]*verboseSessionEntry
	treeSessions    map[string]*treeSessionEntry
	applyCodes      map[string]*applyCodeEntry
}

// WebConfig configures URL generation for StonyPermsWeb sessions.
type WebConfig struct {
	BaseURL       string
	DefaultGroup  string
	LiveSave      bool
	PluginVersion string
}

type editorSessionEntry struct {
	createdAt time.Time
	expiresAt time.Time
	data      EditorSessionData
}

type verboseSessionEntry struct {
	createdAt time.Time
	expiresAt time.Time
	data      VerboseSessionData
}

type treeSessionEntry struct {
	createdAt time.Time
	expiresAt time.Time
	data      TreeSessionData
}

type applyCodeEntry struct {
	createdAt time.Time
	expiresAt time.Time
	payload   EditorSavePayload
}

// WebSessionCreateOptions customizes a newly created Web Permissions Editor session.
type WebSessionCreateOptions struct {
	Actor            string
	CommandAlias     string
	TargetType       string // "all", "user", "group"
	Target           string
	Filter           string
	ExtraPermissions []string
	ExtraContexts    map[string][]string
}

// EditorSessionMetadata describes the origin and capabilities of an editor session.
type EditorSessionMetadata struct {
	CommandAlias  string `json:"commandAlias"`
	Actor         string `json:"actor"`
	TargetType    string `json:"targetType"`
	Target        string `json:"target,omitempty"`
	Filter        string `json:"filter,omitempty"`
	Time          int64  `json:"time"`
	ExpiresAt     int64  `json:"expiresAt"`
	DefaultGroup  string `json:"defaultGroup"`
	Revision      uint64 `json:"revision"`
	LiveSave      bool   `json:"liveSave"`
	PluginVersion string `json:"pluginVersion,omitempty"`
}

// EditorSessionData holds the complete payload loaded by StonyPermsWeb's editor UI.
type EditorSessionData struct {
	Token             string                `json:"token"`
	Metadata          EditorSessionMetadata `json:"metadata"`
	Groups            []model.Group         `json:"groups"`
	Tracks            []model.Track         `json:"tracks"`
	Users             []model.User          `json:"users"`
	KnownPermissions  []string              `json:"knownPermissions"`
	PotentialContexts map[string][]string   `json:"potentialContexts,omitempty"`
}

// EditorSessionCreated is returned when creating a new editor session.
type EditorSessionCreated struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ApplyCode string `json:"applyCode,omitempty"`
	ExpiresAt int64  `json:"expiresAt"`
	LiveSave  bool   `json:"liveSave"`
}

// EditorSavePayload represents edits submitted by StonyPermsWeb or decoded from sp1_/sp_ codes.
type EditorSavePayload struct {
	Groups        []model.Group `json:"groups"`
	Tracks        []model.Track `json:"tracks"`
	Users         []model.User  `json:"users"`
	DeletedGroups []string      `json:"deletedGroups,omitempty"`
	DeletedTracks []string      `json:"deletedTracks,omitempty"`
	DeletedUsers  []uuid.UUID   `json:"deletedUsers,omitempty"`
	BaseRevision  uint64        `json:"baseRevision,omitempty"`
	Actor         string        `json:"actor,omitempty"`
}

// EditorApplySummary reports what an editor save changed cleanly.
type EditorApplySummary struct {
	Applied       bool   `json:"applied"`
	Revision      uint64 `json:"revision"`
	GroupsUpdated int    `json:"groupsUpdated"`
	GroupsDeleted int    `json:"groupsDeleted"`
	TracksUpdated int    `json:"tracksUpdated"`
	TracksDeleted int    `json:"tracksDeleted"`
	UsersUpdated  int    `json:"usersUpdated"`
	UsersDeleted  int    `json:"usersDeleted"`
}

// VerboseSessionMetadata describes a recorded verbose paste session.
type VerboseSessionMetadata struct {
	UploadedBy   string `json:"uploadedBy"`
	CommandAlias string `json:"commandAlias,omitempty"`
	StartTime    int64  `json:"startTime"`
	EndTime      int64  `json:"endTime"`
	DurationMs   int64  `json:"durationMs"`
	TotalCount   int    `json:"totalCount"`
	ShownCount   int    `json:"shownCount"`
	Filter       string `json:"filter"`
	Truncated    bool   `json:"truncated"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// VerboseSessionData holds metadata and recorded entries for StonyPermsWeb's verbose viewer.
type VerboseSessionData struct {
	Token    string                 `json:"token"`
	Metadata VerboseSessionMetadata `json:"metadata"`
	Entries  []engine.VerboseEntry  `json:"entries"`
}

// VerboseSessionCreated is returned when uploading verbose checks.
type VerboseSessionCreated struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt"`
	Count     int    `json:"count"`
}

// TreeSessionMetadata describes a permission tree snapshot.
type TreeSessionMetadata struct {
	UploadedBy   string `json:"uploadedBy"`
	CommandAlias string `json:"commandAlias,omitempty"`
	Time         int64  `json:"time"`
	Root         string `json:"root"`
	Count        int    `json:"count"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// TreeSessionData holds permissions for StonyPermsWeb's permission tree viewer.
type TreeSessionData struct {
	Token       string              `json:"token"`
	Metadata    TreeSessionMetadata `json:"metadata"`
	Permissions []string            `json:"permissions"`
}

// TreeSessionCreated is returned when creating a permission tree viewer session.
type TreeSessionCreated struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt"`
	Count     int    `json:"count"`
}

func (m *Manager) getWebState() *managerWebState {
	if val, ok := webStateMap.Load(m); ok {
		return val.(*managerWebState)
	}
	st := &managerWebState{
		config: WebConfig{
			BaseURL:       DefaultPublicWebURL,
			DefaultGroup:  "default",
			PluginVersion: "0.3.0",
		},
		editorSessions:  make(map[string]*editorSessionEntry),
		verboseSessions: make(map[string]*verboseSessionEntry),
		treeSessions:    make(map[string]*treeSessionEntry),
		applyCodes:      make(map[string]*applyCodeEntry),
	}
	actual, _ := webStateMap.LoadOrStore(m, st)
	return actual.(*managerWebState)
}

// SetWebConfig configures public web URL and defaults used by session generators.
func (m *Manager) SetWebConfig(cfg WebConfig) {
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()

	if strings.TrimSpace(cfg.BaseURL) != "" {
		st.config.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	} else {
		st.config.BaseURL = DefaultPublicWebURL
	}
	if strings.TrimSpace(cfg.DefaultGroup) != "" {
		st.config.DefaultGroup = strings.ToLower(strings.TrimSpace(cfg.DefaultGroup))
	} else {
		st.config.DefaultGroup = "default"
	}
	st.config.LiveSave = cfg.LiveSave
	if strings.TrimSpace(cfg.PluginVersion) != "" {
		st.config.PluginVersion = strings.TrimSpace(cfg.PluginVersion)
	}
}

// SetWebRuntimeHints stores proxy-discovered server names and extra permission suggestions.
func (m *Manager) SetWebRuntimeHints(servers []string, extraPerms []string) {
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(servers) > 0 {
		st.onlineServers = append([]string(nil), servers...)
	}
	if len(extraPerms) > 0 {
		st.knownPerms = append([]string(nil), extraPerms...)
	}
}

// GetWebConfig returns the active WebConfig for this manager.
func (m *Manager) GetWebConfig() WebConfig {
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.config
}

// CreateEditorSession snapshots active permissions/groups/tracks/users into an ephemeral session.
func (m *Manager) CreateEditorSession(opts WebSessionCreateOptions) (*EditorSessionCreated, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(webSessionTTL)

	cfg := m.GetWebConfig()
	targetType := strings.ToLower(strings.TrimSpace(opts.TargetType))
	if targetType == "" {
		targetType = "all"
	}
	commandAlias := strings.TrimSpace(opts.CommandAlias)
	if commandAlias == "" {
		commandAlias = "sp"
	}
	actor := strings.TrimSpace(opts.Actor)
	if actor == "" {
		actor = "Console"
	}

	token, err := generateWebToken(16)
	if err != nil {
		return nil, err
	}

	rawGroups, rev := m.GroupsSnapshot()
	rawTracks, _ := m.TracksSnapshot()
	rawUsers, _, _ := m.ListUsers("", 0, maxEditorUsers)

	groups := make([]model.Group, 0, len(rawGroups))
	for _, g := range rawGroups {
		if g != nil {
			groups = append(groups, *g)
		}
	}

	tracks := make([]model.Track, 0, len(rawTracks))
	for _, t := range rawTracks {
		if t != nil {
			tracks = append(tracks, *t)
		}
	}

	userIncluded := make(map[uuid.UUID]struct{}, len(rawUsers))
	users := make([]model.User, 0, len(rawUsers)+1)
	for _, u := range rawUsers {
		if u != nil {
			users = append(users, *u)
			userIncluded[u.UUID] = struct{}{}
		}
	}
	if targetType == "user" && strings.TrimSpace(opts.Target) != "" {
		if targetUser, ok, _ := m.ResolveUserByName(opts.Target); ok && targetUser != nil {
			if _, exists := userIncluded[targetUser.UUID]; !exists {
				users = append(users, *targetUser)
			}
			opts.Target = targetUser.UUID.String()
		}
	}

	st := m.getWebState()
	st.mu.Lock()
	runtimePerms := append([]string(nil), st.knownPerms...)
	runtimeServers := append([]string(nil), st.onlineServers...)
	st.mu.Unlock()

	combinedExtras := make([]string, 0, len(runtimePerms)+len(opts.ExtraPermissions))
	combinedExtras = append(combinedExtras, runtimePerms...)
	combinedExtras = append(combinedExtras, opts.ExtraPermissions...)

	knownPerms := collectKnownPermissions(groups, users, combinedExtras)
	contexts := collectPotentialContexts(groups, users, runtimeServers, opts.ExtraContexts)

	session := EditorSessionData{
		Token: token,
		Metadata: EditorSessionMetadata{
			CommandAlias:  commandAlias,
			Actor:         actor,
			TargetType:    targetType,
			Target:        strings.TrimSpace(opts.Target),
			Filter:        strings.TrimSpace(opts.Filter),
			Time:          now.UnixMilli(),
			ExpiresAt:     expiresAt.UnixMilli(),
			DefaultGroup:  cfg.DefaultGroup,
			Revision:      rev,
			LiveSave:      cfg.LiveSave,
			PluginVersion: cfg.PluginVersion,
		},
		Groups:            groups,
		Tracks:            tracks,
		Users:             users,
		KnownPermissions:  knownPerms,
		PotentialContexts: contexts,
	}

	var webURL string
	if cfg.LiveSave {
		st.mu.Lock()
		pruneSessionMapLocked(st.editorSessions, now, func(v *editorSessionEntry) (time.Time, time.Time) { return v.createdAt, v.expiresAt })
		st.editorSessions[token] = &editorSessionEntry{
			createdAt: now,
			expiresAt: expiresAt,
			data:      session,
		}
		st.mu.Unlock()

		webURL = joinWebPath(cfg.BaseURL, "/editor?session="+token)
		if targetType == "group" && session.Metadata.Target != "" {
			webURL += "&target=" + urlEscapeSimple("group:"+strings.ToLower(session.Metadata.Target))
		} else if targetType == "user" && session.Metadata.Target != "" {
			webURL += "&target=" + urlEscapeSimple("user:"+session.Metadata.Target)
		}
	} else {
		code, encodeErr := EncodePortableApplyCode(EditorSavePayload{
			Groups:       groups,
			Tracks:       tracks,
			Users:        users,
			BaseRevision: rev,
			Actor:        actor,
		})
		if encodeErr != nil {
			return nil, encodeErr
		}
		webURL = joinWebPath(cfg.BaseURL, "/editor#data="+code)
	}

	return &EditorSessionCreated{
		Token:     token,
		URL:       webURL,
		ExpiresAt: expiresAt.UnixMilli(),
		LiveSave:  cfg.LiveSave,
	}, nil
}

// GetEditorSession retrieves an active editor session by token.
func (m *Manager) GetEditorSession(token string) (*EditorSessionData, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	now := time.Now().UTC()
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, ok := st.editorSessions[token]
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		delete(st.editorSessions, token)
		return nil, false
	}
	cp := entry.data
	return &cp, true
}

// SaveEditorSession validates and applies an editor save payload for an active session.
func (m *Manager) SaveEditorSession(token string, payload EditorSavePayload) (EditorApplySummary, string, error) {
	session, ok := m.GetEditorSession(token)
	if !ok {
		return EditorApplySummary{}, "", ErrWebSessionNotFound
	}
	actor := strings.TrimSpace(payload.Actor)
	if actor == "" {
		actor = session.Metadata.Actor
	}
	summary, err := m.ApplyEditorPayload(actor, payload)
	if err != nil {
		return EditorApplySummary{}, "", err
	}

	code, err := m.CreateShortApplyCode(payload)
	if err != nil {
		code = ""
	}
	return summary, code, nil
}

// CreateShortApplyCode registers a short sp_ code redeemable by /sp applyedits.
func (m *Manager) CreateShortApplyCode(payload EditorSavePayload) (string, error) {
	suffix, err := generateWebToken(9)
	if err != nil {
		return "", err
	}
	code := ShortApplyPrefix + suffix
	now := time.Now().UTC()
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()
	for k, v := range st.applyCodes {
		if now.After(v.expiresAt) {
			delete(st.applyCodes, k)
		}
	}
	if len(st.applyCodes) >= maxApplyCodes {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range st.applyCodes {
			if oldestKey == "" || v.createdAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.createdAt
			}
		}
		if oldestKey != "" {
			delete(st.applyCodes, oldestKey)
		}
	}
	st.applyCodes[code] = &applyCodeEntry{
		createdAt: now,
		expiresAt: now.Add(applyCodeTTL),
		payload:   payload,
	}
	return code, nil
}

// ApplyEditCode decodes a sp1_ portable payload or consumes a sp_ code and applies all changes.
func (m *Manager) ApplyEditCode(actor string, rawCode string) (EditorApplySummary, error) {
	code := strings.TrimSpace(rawCode)
	if strings.HasPrefix(code, PortableCodePrefix) {
		payload, err := DecodePortableApplyCode(code)
		if err != nil {
			return EditorApplySummary{}, err
		}
		if strings.TrimSpace(actor) == "" {
			actor = payload.Actor
		}
		return m.ApplyEditorPayload(actor, payload)
	}
	if strings.HasPrefix(code, ShortApplyPrefix) {
		now := time.Now().UTC()
		st := m.getWebState()
		st.mu.Lock()
		entry, ok := st.applyCodes[code]
		if !ok || now.After(entry.expiresAt) {
			if ok {
				delete(st.applyCodes, code)
			}
			st.mu.Unlock()
			return EditorApplySummary{}, ErrInvalidApplyCode
		}
		payload := entry.payload
		delete(st.applyCodes, code)
		st.mu.Unlock()

		if strings.TrimSpace(actor) == "" {
			actor = payload.Actor
		}
		return m.ApplyEditorPayload(actor, payload)
	}
	return EditorApplySummary{}, ErrInvalidApplyCode
}

// EncodePortableApplyCode encodes an editor payload into a bounded sp1_ gzipped base64url string.
func EncodePortableApplyCode(payload EditorSavePayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return PortableCodePrefix + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// DecodePortableApplyCode decodes an sp1_ payload while capping decompressed bytes to 64 KiB.
func DecodePortableApplyCode(code string) (EditorSavePayload, error) {
	var zero EditorSavePayload
	trimmed := strings.TrimSpace(code)
	if !strings.HasPrefix(trimmed, PortableCodePrefix) {
		return zero, ErrInvalidApplyCode
	}
	encoded := strings.TrimPrefix(trimmed, PortableCodePrefix)
	if encoded == "" {
		return zero, ErrInvalidApplyCode
	}
	compressed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		compressed, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return zero, fmt.Errorf("%w: invalid base64 payload", ErrInvalidApplyCode)
		}
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return zero, fmt.Errorf("%w: invalid gzip payload", ErrInvalidApplyCode)
	}
	defer func() { _ = gz.Close() }()

	decompressed, err := io.ReadAll(io.LimitReader(gz, maxPortableCodeBytes+1))
	if err != nil {
		return zero, fmt.Errorf("%w: failed reading gzip payload", ErrInvalidApplyCode)
	}
	if len(decompressed) > maxPortableCodeBytes {
		return zero, fmt.Errorf("%w: decompressed payload exceeds %d bytes", ErrInvalidApplyCode, maxPortableCodeBytes)
	}

	var payload EditorSavePayload
	dec := json.NewDecoder(bytes.NewReader(decompressed))
	if err := dec.Decode(&payload); err != nil {
		return zero, fmt.Errorf("%w: invalid JSON payload", ErrInvalidApplyCode)
	}
	return payload, nil
}

// ApplyEditorPayload validates and atomically persists groups, tracks, and users from the web editor.
func (m *Manager) ApplyEditorPayload(actor string, payload EditorSavePayload) (EditorApplySummary, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "web-editor"
	}

	// Normalize and validate everything BEFORE mutating storage or memory.
	normalizedDelGroups := make([]string, 0, len(payload.DeletedGroups))
	delGroupSet := make(map[string]struct{}, len(payload.DeletedGroups))
	for _, rawName := range payload.DeletedGroups {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if name == "default" {
			return EditorApplySummary{}, ErrCannotDeleteDefault
		}
		if !editorEntityPattern.MatchString(name) {
			return EditorApplySummary{}, fmt.Errorf("invalid deleted group name %q", rawName)
		}
		if _, seen := delGroupSet[name]; !seen {
			delGroupSet[name] = struct{}{}
			normalizedDelGroups = append(normalizedDelGroups, name)
		}
	}

	normalizedDelTracks := make([]string, 0, len(payload.DeletedTracks))
	delTrackSet := make(map[string]struct{}, len(payload.DeletedTracks))
	for _, rawName := range payload.DeletedTracks {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if !editorEntityPattern.MatchString(name) {
			return EditorApplySummary{}, fmt.Errorf("invalid deleted track name %q", rawName)
		}
		if _, seen := delTrackSet[name]; !seen {
			delTrackSet[name] = struct{}{}
			normalizedDelTracks = append(normalizedDelTracks, name)
		}
	}

	cleanGroups := make([]*model.Group, 0, len(payload.Groups))
	upsertedGroupNames := make(map[string]struct{}, len(payload.Groups))
	for _, raw := range payload.Groups {
		g, err := sanitizeEditorGroup(&raw)
		if err != nil {
			return EditorApplySummary{}, err
		}
		if _, deleted := delGroupSet[g.Name]; deleted {
			continue
		}
		cleanGroups = append(cleanGroups, g)
		upsertedGroupNames[g.Name] = struct{}{}
	}

	cleanUsers := make([]*model.User, 0, len(payload.Users))
	delUserSet := make(map[uuid.UUID]struct{}, len(payload.DeletedUsers))
	for _, id := range payload.DeletedUsers {
		if id != uuid.Nil {
			delUserSet[id] = struct{}{}
		}
	}
	for _, raw := range payload.Users {
		u, err := sanitizeEditorUser(&raw)
		if err != nil {
			return EditorApplySummary{}, err
		}
		if _, deleted := delUserSet[u.UUID]; deleted {
			continue
		}
		cleanUsers = append(cleanUsers, u)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Build candidate group set to validate track group references.
	finalGroupSet := make(map[string]struct{}, len(m.groups)+len(cleanGroups))
	for name := range m.groups {
		if _, del := delGroupSet[name]; !del {
			finalGroupSet[name] = struct{}{}
		}
	}
	for name := range upsertedGroupNames {
		finalGroupSet[name] = struct{}{}
	}

	cleanTracks := make([]*model.Track, 0, len(payload.Tracks))
	for _, raw := range payload.Tracks {
		t, err := sanitizeEditorTrack(&raw, finalGroupSet)
		if err != nil {
			return EditorApplySummary{}, err
		}
		if _, del := delTrackSet[t.Name]; del {
			continue
		}
		cleanTracks = append(cleanTracks, t)
	}

	var summary EditorApplySummary
	summary.Applied = true

	// Persist groups
	for _, g := range cleanGroups {
		existing := m.groups[g.Name]
		if existing != nil && editorGroupEqual(existing, g) {
			continue
		}
		if err := m.storage.SaveGroup(g); err != nil {
			return EditorApplySummary{}, err
		}
		m.groups[g.Name] = g
		summary.GroupsUpdated++
	}
	for _, name := range normalizedDelGroups {
		if _, exists := m.groups[name]; !exists {
			continue
		}
		if err := m.storage.DeleteGroup(name); err != nil {
			return EditorApplySummary{}, err
		}
		delete(m.groups, name)
		summary.GroupsDeleted++
	}

	// Persist tracks
	for _, t := range cleanTracks {
		existing := m.tracks[t.Name]
		if existing != nil && editorTrackEqual(existing, t) {
			continue
		}
		if err := m.storage.SaveTrack(t); err != nil {
			return EditorApplySummary{}, err
		}
		m.tracks[t.Name] = t
		summary.TracksUpdated++
	}
	for _, name := range normalizedDelTracks {
		if _, exists := m.tracks[name]; !exists {
			continue
		}
		if err := m.storage.DeleteTrack(name); err != nil {
			return EditorApplySummary{}, err
		}
		delete(m.tracks, name)
		summary.TracksDeleted++
	}

	// Persist users
	for _, u := range cleanUsers {
		existing := m.users[u.UUID]
		if existing != nil && editorUserEqual(existing, u) {
			continue
		}
		if err := m.storage.SaveUser(u); err != nil {
			return EditorApplySummary{}, err
		}
		if existing != nil && existing.Username != "" && !strings.EqualFold(existing.Username, u.Username) {
			delete(m.nameToUUID, strings.ToLower(existing.Username))
		}
		m.users[u.UUID] = u
		if u.Username != "" {
			m.nameToUUID[strings.ToLower(u.Username)] = u.UUID
		}
		summary.UsersUpdated++
	}
	for id := range delUserSet {
		existing, exists := m.users[id]
		if !exists || existing == nil {
			continue
		}
		if err := m.storage.DeleteUser(id); err != nil {
			return EditorApplySummary{}, err
		}
		if existing.Username != "" {
			delete(m.nameToUUID, strings.ToLower(existing.Username))
		}
		delete(m.users, id)
		summary.UsersDeleted++
	}

	totalChanges := summary.GroupsUpdated + summary.GroupsDeleted +
		summary.TracksUpdated + summary.TracksDeleted +
		summary.UsersUpdated + summary.UsersDeleted

	if totalChanges == 0 {
		summary.Revision = m.revision.Load()
		return summary, nil
	}

	rev := m.bumpRevisionLocked()
	summary.Revision = rev
	auditMsg := fmt.Sprintf("Applied web edits (%d groups updated, %d groups deleted, %d tracks updated, %d tracks deleted, %d users updated, %d users deleted)",
		summary.GroupsUpdated, summary.GroupsDeleted, summary.TracksUpdated, summary.TracksDeleted, summary.UsersUpdated, summary.UsersDeleted)
	m.logActionLocked(actor, "general", "web-editor", auditMsg)

	// Notify subscribers outside m.mu after releasing lock via goroutine-safe helper or deferring notification.
	go m.notifyEvent(ChangeEvent{
		Revision:  rev,
		Type:      EventReload,
		Actor:     actor,
		Timestamp: time.Now().UTC().Unix(),
	})

	return summary, nil
}

// CreateVerboseSession publishes recorded engine checks to an ephemeral viewer session.
func (m *Manager) CreateVerboseSession(rec engine.VerboseRecording, actor, commandAlias string) (*VerboseSessionCreated, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(webSessionTTL)
	cfg := m.GetWebConfig()

	token, err := generateWebToken(16)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(actor) == "" {
		actor = rec.Actor
	}
	if strings.TrimSpace(actor) == "" {
		actor = "Console"
	}
	if strings.TrimSpace(commandAlias) == "" {
		commandAlias = "sp"
	}

	filterDesc := "any"
	if rec.Filter.Target != "" && rec.Filter.Query != "" {
		filterDesc = rec.Filter.Target + " " + rec.Filter.Query
	} else if rec.Filter.Query != "" {
		filterDesc = rec.Filter.Query
	}

	startMs := rec.StartTime
	endMs := rec.EndTime
	if startMs == 0 {
		startMs = now.UnixMilli()
	}
	if endMs < startMs {
		endMs = startMs
	}

	entries := make([]engine.VerboseEntry, len(rec.Entries))
	copy(entries, rec.Entries)

	session := VerboseSessionData{
		Token: token,
		Metadata: VerboseSessionMetadata{
			UploadedBy:   actor,
			CommandAlias: commandAlias,
			StartTime:    startMs,
			EndTime:      endMs,
			DurationMs:   endMs - startMs,
			TotalCount:   rec.TotalCount,
			ShownCount:   len(entries),
			Filter:       filterDesc,
			Truncated:    rec.Truncated,
			ExpiresAt:    expiresAt.UnixMilli(),
		},
		Entries: entries,
	}

	var webURL string
	if cfg.LiveSave {
		st := m.getWebState()
		st.mu.Lock()
		pruneSessionMapLocked(st.verboseSessions, now, func(v *verboseSessionEntry) (time.Time, time.Time) { return v.createdAt, v.expiresAt })
		st.verboseSessions[token] = &verboseSessionEntry{
			createdAt: now,
			expiresAt: expiresAt,
			data:      session,
		}
		st.mu.Unlock()
		webURL = joinWebPath(cfg.BaseURL, "/verbose?session="+token)
	} else {
		code, encodeErr := encodePortableJSON(session)
		if encodeErr != nil {
			return nil, encodeErr
		}
		webURL = joinWebPath(cfg.BaseURL, "/verbose#data="+code)
	}

	return &VerboseSessionCreated{
		Token:     token,
		URL:       webURL,
		ExpiresAt: expiresAt.UnixMilli(),
		Count:     len(entries),
	}, nil
}

// GetVerboseSession fetches a stored verbose recording session by token.
func (m *Manager) GetVerboseSession(token string) (*VerboseSessionData, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	now := time.Now().UTC()
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, ok := st.verboseSessions[token]
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		delete(st.verboseSessions, token)
		return nil, false
	}
	cp := entry.data
	return &cp, true
}

// CreateTreeSession builds an ephemeral permission tree viewer session.
func (m *Manager) CreateTreeSession(root, actor, commandAlias string, extraPerms []string) (*TreeSessionCreated, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(webSessionTTL)
	cfg := m.GetWebConfig()

	token, err := generateWebToken(16)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(actor) == "" {
		actor = "Console"
	}
	if strings.TrimSpace(commandAlias) == "" {
		commandAlias = "sp"
	}

	normalizedRoot := strings.TrimSpace(root)
	normalizedRoot = strings.TrimPrefix(normalizedRoot, ".")
	if normalizedRoot == "" {
		normalizedRoot = "."
	}

	rawGroups, _ := m.GroupsSnapshot()
	rawUsers, _, _ := m.ListUsers("", 0, maxEditorUsers)
	groups := make([]model.Group, 0, len(rawGroups))
	for _, g := range rawGroups {
		if g != nil {
			groups = append(groups, *g)
		}
	}
	users := make([]model.User, 0, len(rawUsers))
	for _, u := range rawUsers {
		if u != nil {
			users = append(users, *u)
		}
	}

	st := m.getWebState()
	st.mu.Lock()
	runtimePerms := append([]string(nil), st.knownPerms...)
	st.mu.Unlock()

	mergedExtras := make([]string, 0, len(runtimePerms)+len(extraPerms))
	mergedExtras = append(mergedExtras, runtimePerms...)
	mergedExtras = append(mergedExtras, extraPerms...)

	allPerms := collectKnownPermissions(groups, users, mergedExtras)
	var filtered []string
	if normalizedRoot == "." {
		filtered = allPerms
	} else {
		prefixLower := strings.ToLower(normalizedRoot)
		for _, p := range allPerms {
			pl := strings.ToLower(p)
			if pl == prefixLower || strings.HasPrefix(pl, prefixLower+".") || strings.HasPrefix(pl, prefixLower) {
				filtered = append(filtered, p)
			}
		}
	}
	if filtered == nil {
		filtered = []string{}
	}

	session := TreeSessionData{
		Token: token,
		Metadata: TreeSessionMetadata{
			UploadedBy:   actor,
			CommandAlias: commandAlias,
			Time:         now.UnixMilli(),
			Root:         normalizedRoot,
			Count:        len(filtered),
			ExpiresAt:    expiresAt.UnixMilli(),
		},
		Permissions: filtered,
	}

	var webURL string
	if cfg.LiveSave {
		st.mu.Lock()
		pruneSessionMapLocked(st.treeSessions, now, func(v *treeSessionEntry) (time.Time, time.Time) { return v.createdAt, v.expiresAt })
		st.treeSessions[token] = &treeSessionEntry{
			createdAt: now,
			expiresAt: expiresAt,
			data:      session,
		}
		st.mu.Unlock()
		webURL = joinWebPath(cfg.BaseURL, "/tree?session="+token)
	} else {
		code, encodeErr := encodePortableJSON(session)
		if encodeErr != nil {
			return nil, encodeErr
		}
		webURL = joinWebPath(cfg.BaseURL, "/tree#data="+code)
	}

	return &TreeSessionCreated{
		Token:     token,
		URL:       webURL,
		ExpiresAt: expiresAt.UnixMilli(),
		Count:     len(filtered),
	}, nil
}

// GetTreeSession retrieves a stored permission tree session by token.
func (m *Manager) GetTreeSession(token string) (*TreeSessionData, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, false
	}
	now := time.Now().UTC()
	st := m.getWebState()
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, ok := st.treeSessions[token]
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		delete(st.treeSessions, token)
		return nil, false
	}
	cp := entry.data
	return &cp, true
}

func encodePortableJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return PortableCodePrefix + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

func generateWebToken(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("failed generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func joinWebPath(base, suffix string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		trimmed = DefaultPublicWebURL
	}
	if !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}
	return trimmed + suffix
}

func urlEscapeSimple(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		isSafe := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if isSafe {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func sanitizeEditorGroup(raw *model.Group) (*model.Group, error) {
	name := strings.ToLower(strings.TrimSpace(raw.Name))
	if !editorEntityPattern.MatchString(name) {
		return nil, fmt.Errorf("invalid group name %q", raw.Name)
	}
	g := model.NewGroup(name, raw.Weight)
	if err := validateEditorText(raw.DisplayName); err != nil {
		return nil, fmt.Errorf("invalid displayName on group %q: %w", name, err)
	}
	g.DisplayName = strings.TrimSpace(raw.DisplayName)

	nodes, err := sanitizeEditorNodes(raw.Nodes)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", name, err)
	}
	g.Nodes = nodes

	parents, err := sanitizeEditorParents(raw.Parents, name)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", name, err)
	}
	g.Parents = parents

	metaEntries, meta, err := sanitizeEditorMetaEntries(raw.MetaEntries, raw.Meta)
	if err != nil {
		return nil, fmt.Errorf("group %q: %w", name, err)
	}
	g.MetaEntries = metaEntries
	g.Meta = meta

	prefixes, err := sanitizeEditorPriorityMeta(raw.Prefixes)
	if err != nil {
		return nil, fmt.Errorf("group %q prefix: %w", name, err)
	}
	g.Prefixes = prefixes

	suffixes, err := sanitizeEditorPriorityMeta(raw.Suffixes)
	if err != nil {
		return nil, fmt.Errorf("group %q suffix: %w", name, err)
	}
	g.Suffixes = suffixes
	return g, nil
}

func sanitizeEditorTrack(raw *model.Track, validGroups map[string]struct{}) (*model.Track, error) {
	name := strings.ToLower(strings.TrimSpace(raw.Name))
	if !editorEntityPattern.MatchString(name) {
		return nil, fmt.Errorf("invalid track name %q", raw.Name)
	}
	t := model.NewTrack(name)
	seen := make(map[string]struct{}, len(raw.Groups))
	groups := make([]string, 0, len(raw.Groups))
	for _, gName := range raw.Groups {
		gn := strings.ToLower(strings.TrimSpace(gName))
		if !editorEntityPattern.MatchString(gn) {
			return nil, fmt.Errorf("track %q contains invalid group name %q", name, gName)
		}
		if _, exists := validGroups[gn]; !exists {
			return nil, fmt.Errorf("track %q references unknown group %q", name, gn)
		}
		if _, dup := seen[gn]; dup {
			return nil, fmt.Errorf("track %q contains duplicate group %q", name, gn)
		}
		seen[gn] = struct{}{}
		groups = append(groups, gn)
	}
	t.Groups = groups
	return t, nil
}

func sanitizeEditorUser(raw *model.User) (*model.User, error) {
	uid := raw.UUID
	username := strings.TrimSpace(raw.Username)
	if uid == uuid.Nil {
		if username == "" {
			return nil, errors.New("user entry requires uuid or username")
		}
		uid = uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(username)))
	}
	if err := validateEditorText(username); err != nil {
		return nil, fmt.Errorf("invalid username %q: %w", username, err)
	}

	u := model.NewUser(uid, username)
	primary := strings.ToLower(strings.TrimSpace(raw.PrimaryGroup))
	if primary == "" {
		primary = "default"
	} else if !editorEntityPattern.MatchString(primary) {
		return nil, fmt.Errorf("user %q invalid primaryGroup %q", username, raw.PrimaryGroup)
	}
	u.PrimaryGroup = primary

	nodes, err := sanitizeEditorNodes(raw.Nodes)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", uid, err)
	}
	u.Nodes = nodes

	parents, err := sanitizeEditorParents(raw.Parents, "")
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", uid, err)
	}
	u.Parents = parents

	metaEntries, meta, err := sanitizeEditorMetaEntries(raw.MetaEntries, raw.Meta)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", uid, err)
	}
	u.MetaEntries = metaEntries
	u.Meta = meta

	prefixes, err := sanitizeEditorPriorityMeta(raw.Prefixes)
	if err != nil {
		return nil, fmt.Errorf("user %q prefix: %w", uid, err)
	}
	u.Prefixes = prefixes

	suffixes, err := sanitizeEditorPriorityMeta(raw.Suffixes)
	if err != nil {
		return nil, fmt.Errorf("user %q suffix: %w", uid, err)
	}
	u.Suffixes = suffixes
	return u, nil
}

func sanitizeEditorNodes(in []model.Node) ([]model.Node, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]model.Node, 0, len(in))
	for _, n := range in {
		key := strings.TrimSpace(n.Key)
		if key == "" || len(key) > maxEditorPermLen || hasControlChars(key) {
			return nil, fmt.Errorf("invalid permission node %q", n.Key)
		}
		ctxs, err := sanitizeEditorContexts(n.Contexts)
		if err != nil {
			return nil, err
		}
		expiry := n.Expiry
		if expiry < 0 {
			expiry = 0
		}
		out = append(out, model.Node{
			Key:      key,
			Value:    n.Value,
			Expiry:   expiry,
			Contexts: ctxs,
		})
	}
	return out, nil
}

func sanitizeEditorParents(in []model.Inheritance, selfGroup string) ([]model.Inheritance, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]model.Inheritance, 0, len(in))
	for _, p := range in {
		grp := strings.ToLower(strings.TrimSpace(p.Group))
		if !editorEntityPattern.MatchString(grp) {
			return nil, fmt.Errorf("invalid parent group %q", p.Group)
		}
		if selfGroup != "" && strings.EqualFold(grp, selfGroup) {
			return nil, fmt.Errorf("group %q cannot inherit from itself", selfGroup)
		}
		ctxs, err := sanitizeEditorContexts(p.Contexts)
		if err != nil {
			return nil, err
		}
		expiry := p.Expiry
		if expiry < 0 {
			expiry = 0
		}
		out = append(out, model.Inheritance{
			Group:    grp,
			Expiry:   expiry,
			Contexts: ctxs,
		})
	}
	return out, nil
}

func sanitizeEditorMeta(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" || len(key) > 64 || hasControlChars(key) {
			return nil, fmt.Errorf("invalid meta key %q", k)
		}
		if err := validateEditorText(v); err != nil {
			return nil, fmt.Errorf("invalid meta value for %q: %w", key, err)
		}
		out[key] = v
	}
	return out, nil
}

func sanitizeEditorMetaEntries(in []model.MetaEntry, rawMeta map[string]string) ([]model.MetaEntry, map[string]string, error) {
	outMeta, err := sanitizeEditorMeta(rawMeta)
	if err != nil {
		return nil, nil, err
	}
	var outEntries []model.MetaEntry
	coveredGlobal := make(map[string]struct{}, len(in))
	for _, me := range in {
		key := strings.TrimSpace(me.Key)
		if key == "" || len(key) > 64 || hasControlChars(key) {
			return nil, nil, fmt.Errorf("invalid meta key %q", me.Key)
		}
		if err := validateEditorText(me.Value); err != nil {
			return nil, nil, fmt.Errorf("invalid meta value for %q: %w", key, err)
		}
		ctxs, err := sanitizeEditorContexts(me.Contexts)
		if err != nil {
			return nil, nil, err
		}
		expiry := me.Expiry
		if expiry < 0 {
			expiry = 0
		}
		outEntries = append(outEntries, model.MetaEntry{
			Key:      key,
			Value:    me.Value,
			Priority: me.Priority,
			Expiry:   expiry,
			Contexts: ctxs,
		})
		if expiry == 0 && len(ctxs) == 0 {
			coveredGlobal[strings.ToLower(key)] = struct{}{}
			if _, exists := outMeta[key]; !exists {
				outMeta[key] = me.Value
			}
		}
	}
	for k, v := range outMeta {
		if _, seen := coveredGlobal[strings.ToLower(k)]; !seen {
			outEntries = append(outEntries, model.MetaEntry{
				Key:   k,
				Value: v,
			})
		}
	}
	return outEntries, outMeta, nil
}

func sanitizeEditorPriorityMeta(in []model.PriorityMeta) ([]model.PriorityMeta, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]model.PriorityMeta, 0, len(in))
	for _, pm := range in {
		if err := validateEditorText(pm.Value); err != nil {
			return nil, err
		}
		ctxs, err := sanitizeEditorContexts(pm.Contexts)
		if err != nil {
			return nil, err
		}
		expiry := pm.Expiry
		if expiry < 0 {
			expiry = 0
		}
		out = append(out, model.PriorityMeta{
			Priority: pm.Priority,
			Value:    pm.Value,
			Expiry:   expiry,
			Contexts: ctxs,
		})
	}
	return out, nil
}

func sanitizeEditorContexts(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxEditorContexts {
		return nil, fmt.Errorf("at most %d contexts allowed per node", maxEditorContexts)
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		key := strings.ToLower(strings.TrimSpace(k))
		if !editorCtxKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid context key %q", k)
		}
		if len(v) > maxEditorTextLen || hasControlChars(v) {
			return nil, fmt.Errorf("invalid context value for %q", key)
		}
		out[key] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateEditorText(s string) error {
	if len(s) > maxEditorTextLen {
		return fmt.Errorf("exceeds %d bytes", maxEditorTextLen)
	}
	if hasControlChars(s) {
		return errors.New("contains control characters")
	}
	return nil
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func editorGroupEqual(a, b *model.Group) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(aj, bj)
}

func editorTrackEqual(a, b *model.Track) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(aj, bj)
}

func editorUserEqual(a, b *model.User) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(aj, bj)
}

func pruneSessionMapLocked[T any](m map[string]T, now time.Time, bounds func(T) (createdAt, expiresAt time.Time)) {
	for k, v := range m {
		_, expiresAt := bounds(v)
		if now.After(expiresAt) {
			delete(m, k)
		}
	}
	for len(m) >= maxWebSessions {
		var oldestKey string
		var oldest time.Time
		for k, v := range m {
			createdAt, _ := bounds(v)
			if oldestKey == "" || createdAt.Before(oldest) {
				oldestKey = k
				oldest = createdAt
			}
		}
		if oldestKey == "" {
			break
		}
		delete(m, oldestKey)
	}
}

func collectKnownPermissions(groups []model.Group, users []model.User, extras []string) []string {
	set := make(map[string]struct{}, 256)
	for _, p := range defaultKnownPermissions() {
		set[p] = struct{}{}
	}
	for _, g := range groups {
		for _, n := range g.Nodes {
			if strings.TrimSpace(n.Key) != "" {
				set[n.Key] = struct{}{}
			}
		}
	}
	for _, u := range users {
		for _, n := range u.Nodes {
			if strings.TrimSpace(n.Key) != "" {
				set[n.Key] = struct{}{}
			}
		}
	}
	for _, p := range extras {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" && len(trimmed) <= maxEditorPermLen && !hasControlChars(trimmed) {
			set[trimmed] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > maxKnownPermissions {
		out = out[:maxKnownPermissions]
	}
	return out
}

func collectPotentialContexts(groups []model.Group, users []model.User, servers []string, extras map[string][]string) map[string][]string {
	ctxSets := map[string]map[string]struct{}{
		"server": {"lobby": {}, "survival": {}, "creative": {}, "hub": {}},
		"world":  {"world": {}, "world_nether": {}, "world_the_end": {}, "hub": {}},
	}
	addCtx := func(k, v string) {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			return
		}
		s, ok := ctxSets[k]
		if !ok {
			s = make(map[string]struct{})
			ctxSets[k] = s
		}
		s[v] = struct{}{}
	}
	for _, srv := range servers {
		addCtx("server", srv)
	}
	for _, g := range groups {
		for _, n := range g.Nodes {
			for k, v := range n.Contexts {
				addCtx(k, v)
			}
		}
		for _, p := range g.Parents {
			for k, v := range p.Contexts {
				addCtx(k, v)
			}
		}
	}
	for _, u := range users {
		for _, n := range u.Nodes {
			for k, v := range n.Contexts {
				addCtx(k, v)
			}
		}
		for _, p := range u.Parents {
			for k, v := range p.Contexts {
				addCtx(k, v)
			}
		}
	}
	for k, vals := range extras {
		for _, v := range vals {
			addCtx(k, v)
		}
	}
	out := make(map[string][]string, len(ctxSets))
	for k, vals := range ctxSets {
		list := make([]string, 0, len(vals))
		for v := range vals {
			list = append(list, v)
		}
		sort.Strings(list)
		out[k] = list
	}
	return out
}

func defaultKnownPermissions() []string {
	return []string{
		"*",
		"gate.*",
		"gate.command.alert",
		"gate.command.find",
		"gate.command.glist",
		"gate.command.ping",
		"gate.command.send",
		"gate.command.server",
		"stonyperms.*",
		"stonyperms.admin",
		"stonyperms.applyedits",
		"stonyperms.check",
		"stonyperms.creategroup",
		"stonyperms.createtrack",
		"stonyperms.deletegroup",
		"stonyperms.deletetrack",
		"stonyperms.editor",
		"stonyperms.group",
		"stonyperms.info",
		"stonyperms.listgroups",
		"stonyperms.listtracks",
		"stonyperms.log",
		"stonyperms.sync",
		"stonyperms.track",
		"stonyperms.tree",
		"stonyperms.use",
		"stonyperms.user",
		"stonyperms.verbose",
		"stonyperms.bridge.admin",
		"stonyperms.bridge.info",
		"stonyperms.bridge.sync",
		"stonyperms.bridge.editor",
		"stonyperms.bridge.applyedits",
		"stonyperms.bridge.tree",
		"bettertab.admin",
		"bettertab.info",
		"bettertab.name",
		"bettertab.name.color",
		"bettertab.reload",
		"bettertab.reset",
		"bettertab.toggle",
		"discord4gate.admin",
		"discord4gate.discord",
		"discord4gate.reload",
		"discord4gate.status",
		"hybridforwarding.admin",
		"hybridforwarding.reload",
		"simplewhitelist.add",
		"simplewhitelist.admin",
		"simplewhitelist.bypass",
		"simplewhitelist.list",
		"simplewhitelist.reload",
		"simplewhitelist.remove",
		"simplewhitelist.toggle",
		"simplewhitelist.use",
		"skinschanger.admin",
		"skinschanger.bypass.cooldown",
		"skinschanger.clear",
		"skinschanger.clearfor",
		"skinschanger.info",
		"skinschanger.reload",
		"skinschanger.set",
		"skinschanger.setfor",
		"skinschanger.update",
		"skinschanger.url",
		"skinschanger.urlfor",
		"skinschanger.use",
		"smartlimbo.admin",
		"smartlimbo.bypass",
		"smartlimbo.priority",
		"smartlimbo.queue",
		"smartlimbo.reconnect",
		"smartlimbo.reload",
		"smartlimbo.status",
		"minecraft.command.gamemode",
		"minecraft.command.teleport",
		"minecraft.command.kick",
		"minecraft.command.ban",
	}
}
