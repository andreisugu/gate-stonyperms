package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

const (
	maxActorLength      = 64
	maxDurationSeconds  = int64(315360000) // 10 years
	maxMetaValueBytes   = 1024
	maxAffixValueBytes  = 256
	maxBatchOperations  = 32
	maxDisplayNameBytes = 256
	maxUsernameLength   = 64
)

var (
	entityNamePattern = regexp.MustCompile(`^[a-z0-9_+-]{1,64}$`)
	metaKeyPattern    = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,64}$`)

	errParentNotFound = errors.New("parent not found")
	errNodeNotFound   = errors.New("node not found")
	errMetaNotFound   = errors.New("meta not found")
	errPrefixNotFound = errors.New("prefix not found")
	errSuffixNotFound = errors.New("suffix not found")
)

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	return e.code + ": " + e.message
}

type mutableTarget interface {
	AddNode(model.Node)
	AddNodeWithModifier(model.Node, model.TempModifier, time.Time) (model.Node, error)
	RemoveNode(key string, contexts map[string]string) bool
	RemoveTempNode(key string, contexts map[string]string) bool
	SetMeta(key, value string)
	SetMetaEntry(model.MetaEntry, model.TempModifier, time.Time) (model.MetaEntry, error)
	UnsetMeta(key string) bool
	UnsetMetaEntry(key string, contexts map[string]string, tempOnly bool) bool
	AddPrefix(model.PriorityMeta)
	AddPrefixWithModifier(model.PriorityMeta, model.TempModifier, time.Time) (model.PriorityMeta, error)
	SetPrefixInContext(model.PriorityMeta)
	RemovePrefix(priority int) bool
	RemovePrefixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool
	AddSuffix(model.PriorityMeta)
	AddSuffixWithModifier(model.PriorityMeta, model.TempModifier, time.Time) (model.PriorityMeta, error)
	SetSuffixInContext(model.PriorityMeta)
	RemoveSuffix(priority int) bool
	RemoveSuffixMatch(priority int, value string, contexts map[string]string, tempOnly bool) bool
	AddParent(model.Inheritance)
	AddParentWithModifier(model.Inheritance, model.TempModifier, time.Time) (model.Inheritance, error)
	RemoveParent(groupName string, contexts map[string]string) bool
	RemoveTempParent(groupName string, contexts map[string]string) bool
}

// -----------------------------------------------------------------------------
// Validation & Request Decoding Helpers
// -----------------------------------------------------------------------------

func parseActorHeader(r *http.Request) (string, *apiError) {
	vals := r.Header.Values("X-StonyPerms-Actor")
	if len(vals) == 0 {
		return "api", nil
	}
	actor := r.Header.Get("X-StonyPerms-Actor")
	if len(actor) == 0 || len(actor) > maxActorLength || strings.TrimSpace(actor) == "" || !isPrintableASCII(actor, true) {
		return "", &apiError{http.StatusBadRequest, "invalid_actor", "X-StonyPerms-Actor must be 1-64 printable ASCII characters"}
	}
	return actor, nil
}

func isPrintableASCII(s string, allowSpace bool) bool {
	minByte := byte(0x21)
	if allowSpace {
		minByte = 0x20
	}
	for i := 0; i < len(s); i++ {
		if s[i] < minByte || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func validateEntityName(raw string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	return name, entityNamePattern.MatchString(name)
}

func validatePermissionKey(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > maxPermissionLength || !isPrintableASCII(raw, false) {
		return "", false
	}
	return strings.ToLower(raw), true
}

func validateMetaKey(key string) bool {
	return metaKeyPattern.MatchString(key)
}

func validateSafeText(val string, maxBytes int) bool {
	if len(val) > maxBytes {
		return false
	}
	for _, r := range val {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func resolveExpiry(durationSeconds, expiry int64, now time.Time) (int64, *apiError) {
	if durationSeconds < 0 || durationSeconds > maxDurationSeconds {
		return 0, &apiError{http.StatusBadRequest, "invalid_expiry", "durationSeconds must be between 0 and 315360000"}
	}
	if expiry < 0 {
		return 0, &apiError{http.StatusBadRequest, "invalid_expiry", "expiry must not be negative"}
	}
	if durationSeconds > 0 && expiry > 0 {
		return 0, &apiError{http.StatusBadRequest, "invalid_expiry", "cannot specify both durationSeconds and expiry"}
	}
	if durationSeconds > 0 {
		return now.Unix() + durationSeconds, nil
	}
	return expiry, nil
}

func copyContexts(src map[string]string) map[string]string {
	if len(src) == 0 {
		return make(map[string]string)
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any) *apiError {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return &apiError{http.StatusBadRequest, "invalid_body", "body must be valid JSON matching the endpoint schema"}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &apiError{http.StatusBadRequest, "invalid_body", "body must contain exactly one JSON object"}
	}
	return nil
}

func decodeOptionalStrictJSON(w http.ResponseWriter, r *http.Request, dst any) (bool, *apiError) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		return false, &apiError{http.StatusBadRequest, "invalid_body", "request body is invalid or exceeds size limit"}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return false, &apiError{http.StatusBadRequest, "invalid_body", "body must be valid JSON matching the endpoint schema"}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false, &apiError{http.StatusBadRequest, "invalid_body", "body must contain exactly one JSON object"}
	}
	return true, nil
}

func (s *Server) requireGroupExists(raw string) (string, *apiError) {
	group, ok := validateEntityName(raw)
	if !ok {
		return "", &apiError{http.StatusBadRequest, "invalid_group", "group name must match ^[a-z0-9_+-]{1,64}$"}
	}
	if _, found, _ := s.mgr.GroupSnapshot(group); !found {
		return "", &apiError{http.StatusNotFound, "group_not_found", "group does not exist"}
	}
	return group, nil
}

func (s *Server) resolveWriteUser(idParam string) (id uuid.UUID, createIfMissing bool, apiErr *apiError) {
	trimmed := strings.TrimSpace(idParam)
	if trimmed == "" {
		return uuid.Nil, false, &apiError{http.StatusBadRequest, "invalid_user", "user identifier cannot be empty"}
	}
	if parsed, err := uuid.Parse(trimmed); err == nil {
		if parsed == uuid.Nil {
			return uuid.Nil, false, &apiError{http.StatusBadRequest, "invalid_uuid", "uuid cannot be nil"}
		}
		return parsed, true, nil
	}
	u, found, _ := s.mgr.ResolveUserByName(trimmed)
	if !found {
		return uuid.Nil, false, &apiError{http.StatusNotFound, "user_not_found", "user does not exist"}
	}
	return u.UUID, false, nil
}

func (s *Server) writeAPIError(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	switch {
	case errors.Is(err, manager.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
	case errors.Is(err, manager.ErrGroupNotFound):
		writeError(w, http.StatusNotFound, "group_not_found", "group does not exist")
	case errors.Is(err, manager.ErrTrackNotFound):
		writeError(w, http.StatusNotFound, "track_not_found", "track does not exist")
	case errors.Is(err, manager.ErrGroupExists):
		writeError(w, http.StatusConflict, "group_exists", "group already exists")
	case errors.Is(err, manager.ErrTrackExists):
		writeError(w, http.StatusConflict, "track_exists", "track already exists")
	case errors.Is(err, manager.ErrCannotDeleteDefault):
		writeError(w, http.StatusBadRequest, "cannot_delete_default", "cannot delete the default group")
	case errors.Is(err, errParentNotFound):
		writeError(w, http.StatusNotFound, "parent_not_found", "parent group link does not exist")
	case errors.Is(err, errNodeNotFound):
		writeError(w, http.StatusNotFound, "node_not_found", "permission node does not exist")
	case errors.Is(err, errMetaNotFound):
		writeError(w, http.StatusNotFound, "meta_not_found", "meta key does not exist")
	case errors.Is(err, errPrefixNotFound):
		writeError(w, http.StatusNotFound, "prefix_not_found", "prefix priority does not exist")
	case errors.Is(err, errSuffixNotFound):
		writeError(w, http.StatusNotFound, "suffix_not_found", "suffix priority does not exist")
	case errors.Is(err, model.ErrTempEntryExists):
		writeError(w, http.StatusConflict, "temp_entry_exists", err.Error())
	case errors.Is(err, manager.ErrTrackEmpty),
		errors.Is(err, manager.ErrTrackTop),
		errors.Is(err, manager.ErrTrackBottom),
		errors.Is(err, manager.ErrNotOnTrack):
		writeError(w, http.StatusConflict, "track_conflict", err.Error())
	default:
		s.log.Error(err, "StonyPerms API write failure")
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

// -----------------------------------------------------------------------------
// Shared User / Group Mutation Helpers
// -----------------------------------------------------------------------------

func parseAPITempModifier(raw string) (model.TempModifier, *apiError) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return model.TempModifierReplace, nil
	}
	mod, ok := model.ParseTempModifier(trimmed)
	if !ok {
		return "", &apiError{http.StatusBadRequest, "invalid_modifier", "modifier must be accumulate, replace, or deny"}
	}
	return mod, nil
}

type parsedParentOp struct {
	group           string
	mode            string
	modifier        model.TempModifier
	durationSeconds int64
	expiry          int64
	contexts        map[string]string
	setPrimary      bool
}

func (s *Server) parseParentRequest(req setParentRequest) (parsedParentOp, *apiError) {
	group, apiErr := s.requireGroupExists(req.Group)
	if apiErr != nil {
		return parsedParentOp{}, apiErr
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "add"
	}
	mod := model.TempModifierReplace
	switch mode {
	case "add", "set":
		if req.Modifier != "" {
			parsedMod, modErr := parseAPITempModifier(req.Modifier)
			if modErr != nil {
				return parsedParentOp{}, modErr
			}
			mod = parsedMod
		}
	case "accumulate", "replace", "deny":
		parsedMod, _ := model.ParseTempModifier(mode)
		mod = parsedMod
	default:
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_mode", "mode must be add, set, accumulate, replace, or deny"}
	}
	if err := validateContexts(req.Contexts); err != nil {
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}
	exp, apiErr := resolveExpiry(req.DurationSeconds, req.Expiry, time.Now())
	if apiErr != nil {
		return parsedParentOp{}, apiErr
	}
	if (mode == "accumulate" || mod == model.TempModifierAccumulate) && req.Expiry > 0 {
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_expiry", "accumulate mode requires durationSeconds rather than explicit expiry"}
	}
	return parsedParentOp{
		group:           group,
		mode:            mode,
		modifier:        mod,
		durationSeconds: req.DurationSeconds,
		expiry:          exp,
		contexts:        copyContexts(req.Contexts),
		setPrimary:      req.SetPrimary,
	}, nil
}

func applyParentOp(
	parents *[]model.Inheritance,
	target mutableTarget,
	primaryGroup *string,
	op parsedParentOp,
	now time.Time,
) (string, error) {
	if op.mode == "set" {
		*parents = []model.Inheritance{{
			Group:    op.group,
			Expiry:   op.expiry,
			Contexts: copyContexts(op.contexts),
		}}
		if primaryGroup != nil {
			*primaryGroup = op.group
		}
		return fmt.Sprintf("Set parent group to '%s'", op.group), nil
	}

	inh := model.Inheritance{
		Group:    op.group,
		Expiry:   op.expiry,
		Contexts: copyContexts(op.contexts),
	}
	if _, err := target.AddParentWithModifier(inh, op.modifier, now); err != nil {
		return "", err
	}
	if primaryGroup != nil && op.setPrimary {
		*primaryGroup = op.group
	}
	if op.modifier == model.TempModifierAccumulate {
		return fmt.Sprintf("Accumulated parent group '%s' (+%ds)", op.group, op.durationSeconds), nil
	}
	return fmt.Sprintf("Added parent group '%s'", op.group), nil
}

func parsePermissionInput(rawKey string, valPtr *bool, durationSeconds, expiry int64, contexts map[string]string, now time.Time) (model.Node, *apiError) {
	key, ok := validatePermissionKey(rawKey)
	if !ok {
		return model.Node{}, &apiError{http.StatusBadRequest, "invalid_permission", "permission key must be 1-256 non-whitespace printable ASCII characters"}
	}
	if err := validateContexts(contexts); err != nil {
		return model.Node{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}
	exp, apiErr := resolveExpiry(durationSeconds, expiry, now)
	if apiErr != nil {
		return model.Node{}, apiErr
	}
	val := true
	if valPtr != nil {
		val = *valPtr
	}
	return model.Node{
		Key:      key,
		Value:    val,
		Expiry:   exp,
		Contexts: copyContexts(contexts),
	}, nil
}

func parseAffixInput(req setAffixRequest, now time.Time) (model.PriorityMeta, *apiError) {
	if !validateSafeText(req.Value, maxAffixValueBytes) {
		return model.PriorityMeta{}, &apiError{http.StatusBadRequest, "invalid_affix", "prefix/suffix must be at most 256 bytes without control characters"}
	}
	if err := validateContexts(req.Contexts); err != nil {
		return model.PriorityMeta{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}
	exp, apiErr := resolveExpiry(req.DurationSeconds, req.Expiry, now)
	if apiErr != nil {
		return model.PriorityMeta{}, apiErr
	}
	return model.PriorityMeta{
		Priority: req.Priority,
		Value:    req.Value,
		Expiry:   exp,
		Contexts: copyContexts(req.Contexts),
	}, nil
}

func parseDeleteNodeArgs(w http.ResponseWriter, r *http.Request) (string, map[string]string, bool, *apiError) {
	var req deletePermissionRequest
	hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
	if apiErr != nil {
		return "", nil, false, apiErr
	}

	rawKey := req.Key
	rawCtx := req.Contexts
	tempOnly := req.TempOnly
	if !hasBody {
		q := r.URL.Query()
		rawKey = q.Get("key")
		q.Del("key")
		if strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true") {
			tempOnly = true
		}
		q.Del("tempOnly")
		var err error
		rawCtx, err = parseContexts(q)
		if err != nil {
			return "", nil, false, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
		}
	} else if err := validateContexts(rawCtx); err != nil {
		return "", nil, false, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}

	key, ok := validatePermissionKey(rawKey)
	if !ok {
		return "", nil, false, &apiError{http.StatusBadRequest, "invalid_permission", "permission key must be 1-256 non-whitespace printable ASCII characters"}
	}
	return key, copyContexts(rawCtx), tempOnly, nil
}

func (s *Server) mutateSubject(
	w http.ResponseWriter,
	isUser bool,
	subjectID string,
	actor string,
	fn func(target mutableTarget) (changed bool, auditAction string, err error),
) {
	if isUser {
		uid, createIfMissing, apiErr := s.resolveWriteUser(subjectID)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
			return fn(u)
		})
		if err != nil {
			s.writeAPIError(w, err)
			return
		}
		w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
		writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
		return
	}

	groupName := strings.ToLower(strings.TrimSpace(subjectID))
	g, rev, err := s.mgr.MutateGroup(groupName, actor, func(g *model.Group) (bool, string, error) {
		return fn(g)
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupResponse{Revision: rev, Group: g})
}

// -----------------------------------------------------------------------------
// User Write Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleEnsureUser(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}

	var req ensureUserRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}

	username := strings.TrimSpace(req.Username)
	if username != "" && (len(username) > maxUsernameLength || !validateSafeText(username, maxUsernameLength)) {
		writeError(w, http.StatusBadRequest, "invalid_username", "username must be 1-64 printable characters")
		return
	}

	primaryGroup := s.opts.DefaultGroup
	explicitPrimary := strings.TrimSpace(req.PrimaryGroup) != ""
	if explicitPrimary {
		var pgErr *apiError
		primaryGroup, pgErr = s.requireGroupExists(req.PrimaryGroup)
		if pgErr != nil {
			s.writeAPIError(w, pgErr)
			return
		}
	}

	var targetID uuid.UUID
	if strings.TrimSpace(req.UUID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(req.UUID))
		if err != nil || parsed == uuid.Nil {
			writeError(w, http.StatusBadRequest, "invalid_uuid", "uuid must be a valid non-nil UUID")
			return
		}
		targetID = parsed
	} else if username != "" {
		if existing, found, _ := s.mgr.ResolveUserByName(username); found {
			targetID = existing.UUID
		} else if req.CreateOffline {
			targetID = uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+strings.ToLower(username)))
		} else {
			writeError(w, http.StatusNotFound, "user_not_found", "user does not exist and createOffline is false")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "invalid_body", "either uuid or username is required")
		return
	}

	user, created, rev, err := s.mgr.ResolveOrCreateUser(targetID, username, primaryGroup, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	if explicitPrimary && !created && !strings.EqualFold(user.PrimaryGroup, primaryGroup) {
		user, rev, err = s.mgr.MutateUser(targetID, "", false, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
			u.PrimaryGroup = primaryGroup
			return true, fmt.Sprintf("Set primary group to '%s'", primaryGroup), nil
		})
		if err != nil {
			s.writeAPIError(w, err)
			return
		}
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, status, ensureUserResponse{
		Revision: rev,
		Created:  created,
		User:     user,
	})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, _, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	rev, err := s.mgr.DeleteUserByID(uid, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, deleteUserResponse{
		Revision: rev,
		Deleted:  true,
		UUID:     uid.String(),
	})
}

func (s *Server) handleUserAddParent(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req setParentRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	op, apiErr := s.parseParentRequest(req)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}

	now := time.Now()
	u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		audit, opErr := applyParentOp(&u.Parents, u, &u.PrimaryGroup, op, now)
		if opErr != nil {
			return false, "", opErr
		}
		return true, audit, nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

func (s *Server) handleUserRemoveParent(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if createIfMissing {
		// Removing a parent from a non-existent UUID user cannot match any parent.
		if _, found, _ := s.mgr.UserSnapshot(uid); !found {
			writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
			return
		}
	}
	groupName, ok := validateEntityName(r.PathValue("group"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name must match ^[a-z0-9_+-]{1,64}$")
		return
	}
	q := r.URL.Query()
	tempOnly := strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true")
	q.Del("tempOnly")
	contexts, err := parseContexts(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}

	u, rev, err := s.mgr.MutateUser(uid, "", false, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		var removed bool
		if tempOnly {
			removed = u.RemoveTempParent(groupName, contexts)
		} else {
			removed = u.RemoveParent(groupName, contexts)
		}
		if !removed {
			return false, "", errParentNotFound
		}
		if !tempOnly && strings.EqualFold(u.PrimaryGroup, groupName) {
			u.PrimaryGroup = s.opts.DefaultGroup
		}
		return true, fmt.Sprintf("Removed parent group '%s'", groupName), nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

func (s *Server) handleUserPrimaryGroup(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req setPrimaryGroupRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	group, apiErr := s.requireGroupExists(req.Group)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}

	now := time.Now()
	u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		hasGlobalParent := false
		for _, p := range u.Parents {
			if strings.EqualFold(p.Group, group) && len(p.Contexts) == 0 && !p.IsExpired(now) {
				hasGlobalParent = true
				break
			}
		}
		if !hasGlobalParent {
			u.AddParent(model.NewInheritance(group))
		}
		if strings.EqualFold(u.PrimaryGroup, group) && hasGlobalParent {
			return false, "", nil
		}
		u.PrimaryGroup = group
		return true, fmt.Sprintf("Set primary group to '%s'", group), nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

func (s *Server) handleSubjectSetPermission(isUser bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		var req setPermissionRequest
		if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		mod, apiErr := parseAPITempModifier(req.Modifier)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		now := time.Now()
		node, apiErr := parsePermissionInput(req.Key, req.Value, req.DurationSeconds, req.Expiry, req.Contexts, now)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if _, err := target.AddNodeWithModifier(node, mod, now); err != nil {
				return false, "", err
			}
			return true, fmt.Sprintf("Set permission '%s' to %v (modifier=%s)", node.Key, node.Value, mod), nil
		})
	}
}

func (s *Server) handleSubjectDeletePermission(isUser bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		key, contexts, tempOnly, apiErr := parseDeleteNodeArgs(w, r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			var removed bool
			if tempOnly {
				removed = target.RemoveTempNode(key, contexts)
			} else {
				removed = target.RemoveNode(key, contexts)
			}
			if !removed {
				return false, "", errNodeNotFound
			}
			return true, fmt.Sprintf("Unset permission '%s'", key), nil
		})
	}
}

func (s *Server) handleSubjectSetMeta(isUser bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		key := r.PathValue("key")
		if !validateMetaKey(key) {
			writeError(w, http.StatusBadRequest, "invalid_meta_key", "meta key must match ^[a-zA-Z0-9._:-]{1,64}$")
			return
		}
		var req setMetaRequest
		if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		if !validateSafeText(req.Value, maxMetaValueBytes) {
			writeError(w, http.StatusBadRequest, "invalid_meta_value", "meta value must be at most 1024 bytes without control characters")
			return
		}
		if err := validateContexts(req.Contexts); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
			return
		}
		mod, apiErr := parseAPITempModifier(req.Modifier)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		now := time.Now()
		exp, apiErr := resolveExpiry(req.DurationSeconds, req.Expiry, now)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			_, err := target.SetMetaEntry(model.MetaEntry{
				Key:      key,
				Value:    req.Value,
				Expiry:   exp,
				Contexts: copyContexts(req.Contexts),
			}, mod, now)
			if err != nil {
				return false, "", err
			}
			return true, fmt.Sprintf("Set meta '%s'", key), nil
		})
	}
}

func (s *Server) handleSubjectDeleteMeta(isUser bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		key := r.PathValue("key")
		if !validateMetaKey(key) {
			writeError(w, http.StatusBadRequest, "invalid_meta_key", "meta key must match ^[a-zA-Z0-9._:-]{1,64}$")
			return
		}
		var req deleteMetaRequest
		hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		contexts := req.Contexts
		tempOnly := req.TempOnly
		if !hasBody {
			q := r.URL.Query()
			if strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true") {
				tempOnly = true
			}
			q.Del("tempOnly")
			var err error
			contexts, err = parseContexts(q)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
				return
			}
		} else if err := validateContexts(contexts); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if !target.UnsetMetaEntry(key, contexts, tempOnly) {
				return false, "", errMetaNotFound
			}
			return true, fmt.Sprintf("Unset meta '%s'", key), nil
		})
	}
}

func (s *Server) handleSubjectSetAffix(isUser, isPrefix bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		var req setAffixRequest
		if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode != "" && mode != "add" && mode != "set" {
			writeError(w, http.StatusBadRequest, "invalid_mode", "mode must be add or set")
			return
		}
		mod, apiErr := parseAPITempModifier(req.Modifier)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		now := time.Now()
		pm, apiErr := parseAffixInput(req, now)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if isPrefix {
				if mode == "set" {
					target.SetPrefixInContext(pm)
				} else if _, err := target.AddPrefixWithModifier(pm, mod, now); err != nil {
					return false, "", err
				}
				return true, fmt.Sprintf("Set prefix (priority %d)", pm.Priority), nil
			}
			if mode == "set" {
				target.SetSuffixInContext(pm)
			} else if _, err := target.AddSuffixWithModifier(pm, mod, now); err != nil {
				return false, "", err
			}
			return true, fmt.Sprintf("Set suffix (priority %d)", pm.Priority), nil
		})
	}
}

func (s *Server) handleSubjectDeleteAffix(isUser, isPrefix bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		var req deleteAffixRequest
		hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		q := r.URL.Query()
		priorityStr := strings.TrimSpace(r.PathValue("priority"))
		var priority int
		if priorityStr != "" {
			parsed, err := strconv.Atoi(priorityStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be an integer")
				return
			}
			priority = parsed
		} else if hasBody && req.Priority != nil {
			priority = *req.Priority
		} else if qp := strings.TrimSpace(q.Get("priority")); qp != "" {
			parsed, err := strconv.Atoi(qp)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be an integer")
				return
			}
			priority = parsed
		} else {
			writeError(w, http.StatusBadRequest, "invalid_priority", "priority is required")
			return
		}

		value := req.Value
		contexts := req.Contexts
		tempOnly := req.TempOnly
		if !hasBody {
			if qv := q.Get("value"); qv != "" {
				value = qv
			}
			if strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true") {
				tempOnly = true
			}
			q.Del("priority")
			q.Del("value")
			q.Del("tempOnly")
			var err error
			contexts, err = parseContexts(q)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
				return
			}
		} else if err := validateContexts(contexts); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
			return
		}

		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if isPrefix {
				if !target.RemovePrefixMatch(priority, value, contexts, tempOnly) {
					return false, "", errPrefixNotFound
				}
				return true, fmt.Sprintf("Removed prefix (priority %d)", priority), nil
			}
			if !target.RemoveSuffixMatch(priority, value, contexts, tempOnly) {
				return false, "", errSuffixNotFound
			}
			return true, fmt.Sprintf("Removed suffix (priority %d)", priority), nil
		})
	}
}

func (s *Server) handleUserTrackAction(promote bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, apiErr := parseActorHeader(r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		var req trackActionRequest
		if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		trackName, ok := validateEntityName(req.Track)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_track", "track name must match ^[a-z0-9_+-]{1,64}$")
			return
		}
		if createIfMissing {
			tr, found, _ := s.mgr.TrackSnapshot(trackName)
			if !found {
				writeError(w, http.StatusNotFound, "track_not_found", "track does not exist")
				return
			}
			if len(tr.Groups) == 0 {
				writeError(w, http.StatusConflict, "track_conflict", manager.ErrTrackEmpty.Error())
				return
			}
			if _, userExists, _ := s.mgr.UserSnapshot(uid); !userExists {
				if !promote {
					writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
					return
				}
				if _, _, _, err := s.mgr.ResolveOrCreateUser(uid, "", s.opts.DefaultGroup, ""); err != nil {
					s.writeAPIError(w, err)
					return
				}
			}
		}

		var (
			user *model.User
			from string
			to   string
			rev  uint64
			err  error
		)
		if promote {
			user, from, to, rev, err = s.mgr.PromoteUserByID(uid, trackName, actor)
		} else {
			user, from, to, rev, err = s.mgr.DemoteUserByID(uid, trackName, actor)
		}
		if err != nil {
			s.writeAPIError(w, err)
			return
		}
		w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
		writeJSON(w, http.StatusOK, trackActionResponse{
			Revision:  rev,
			FromGroup: from,
			ToGroup:   to,
			User:      user,
		})
	}
}

func (s *Server) handleUserClear(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req clearUserRequest
	if _, apiErr := decodeOptionalStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "all"
	}
	switch scope {
	case "all", "permissions", "parents", "meta":
	default:
		writeError(w, http.StatusBadRequest, "invalid_scope", "scope must be all, permissions, parents, or meta")
		return
	}

	u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		switch scope {
		case "all":
			u.ClearAll()
			u.PrimaryGroup = s.opts.DefaultGroup
			return true, "Cleared all user data", nil
		case "permissions":
			u.Nodes = make([]model.Node, 0)
			return true, "Cleared user permissions", nil
		case "parents":
			u.Parents = make([]model.Inheritance, 0)
			u.PrimaryGroup = s.opts.DefaultGroup
			return true, "Cleared user parents", nil
		case "meta":
			u.Meta = make(map[string]string)
			u.Prefixes = make([]model.PriorityMeta, 0)
			u.Suffixes = make([]model.PriorityMeta, 0)
			return true, "Cleared user metadata", nil
		}
		return false, "", nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

// -----------------------------------------------------------------------------
// Batch User Mutation Handler
// -----------------------------------------------------------------------------

type preparedBatchOp struct {
	op       string
	parentOp parsedParentOp
	group    string
	key      string
	node     model.Node
	metaVal  string
	affix    model.PriorityMeta
	priority int
	contexts map[string]string
}

func (s *Server) prepareBatchOp(raw batchUserOperation, now time.Time) (preparedBatchOp, *apiError) {
	op := strings.ToLower(strings.TrimSpace(raw.Op))
	switch op {
	case "parent_add", "parent_set", "parent_accumulate":
		if len(raw.Value) > 0 {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_batch", op + " does not accept value"}
		}
		mode := strings.TrimPrefix(op, "parent_")
		parsed, apiErr := s.parseParentRequest(setParentRequest{
			Group:           raw.Group,
			Mode:            mode,
			DurationSeconds: raw.DurationSeconds,
			Expiry:          raw.Expiry,
			Contexts:        raw.Contexts,
			SetPrimary:      raw.SetPrimary,
		})
		if apiErr != nil {
			return preparedBatchOp{}, apiErr
		}
		return preparedBatchOp{op: op, parentOp: parsed}, nil

	case "parent_remove":
		group, ok := validateEntityName(raw.Group)
		if !ok {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_group", "group name must match ^[a-z0-9_+-]{1,64}$"}
		}
		if err := validateContexts(raw.Contexts); err != nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
		}
		return preparedBatchOp{op: op, group: group, contexts: copyContexts(raw.Contexts)}, nil

	case "primary_group_set":
		group, apiErr := s.requireGroupExists(raw.Group)
		if apiErr != nil {
			return preparedBatchOp{}, apiErr
		}
		return preparedBatchOp{op: op, group: group}, nil

	case "permission_set":
		var valPtr *bool
		if len(raw.Value) > 0 {
			var b bool
			if err := json.Unmarshal(raw.Value, &b); err != nil {
				return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_batch", "permission_set value must be a boolean"}
			}
			valPtr = &b
		}
		node, apiErr := parsePermissionInput(raw.Key, valPtr, raw.DurationSeconds, raw.Expiry, raw.Contexts, now)
		if apiErr != nil {
			return preparedBatchOp{}, apiErr
		}
		return preparedBatchOp{op: op, node: node}, nil

	case "permission_unset":
		key, ok := validatePermissionKey(raw.Key)
		if !ok {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_permission", "permission key must be 1-256 non-whitespace printable ASCII characters"}
		}
		if err := validateContexts(raw.Contexts); err != nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
		}
		return preparedBatchOp{op: op, key: key, contexts: copyContexts(raw.Contexts)}, nil

	case "meta_set":
		if !validateMetaKey(raw.Key) {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_meta_key", "meta key must match ^[a-zA-Z0-9._:-]{1,64}$"}
		}
		if len(raw.Value) == 0 {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_meta_value", "meta_set requires a string value"}
		}
		var strVal string
		if err := json.Unmarshal(raw.Value, &strVal); err != nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_meta_value", "meta_set value must be a string"}
		}
		if !validateSafeText(strVal, maxMetaValueBytes) {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_meta_value", "meta value must be at most 1024 bytes without control characters"}
		}
		return preparedBatchOp{op: op, key: raw.Key, metaVal: strVal}, nil

	case "meta_unset":
		if !validateMetaKey(raw.Key) {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_meta_key", "meta key must match ^[a-zA-Z0-9._:-]{1,64}$"}
		}
		return preparedBatchOp{op: op, key: raw.Key}, nil

	case "prefix_set", "suffix_set":
		if raw.Priority == nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_priority", op + " requires priority"}
		}
		if len(raw.Value) == 0 {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_affix", op + " requires a string value"}
		}
		var strVal string
		if err := json.Unmarshal(raw.Value, &strVal); err != nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_affix", op + " value must be a string"}
		}
		pm, apiErr := parseAffixInput(setAffixRequest{
			Priority:        *raw.Priority,
			Value:           strVal,
			DurationSeconds: raw.DurationSeconds,
			Expiry:          raw.Expiry,
			Contexts:        raw.Contexts,
		}, now)
		if apiErr != nil {
			return preparedBatchOp{}, apiErr
		}
		return preparedBatchOp{op: op, affix: pm}, nil

	case "prefix_remove", "suffix_remove":
		if raw.Priority == nil {
			return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_priority", op + " requires priority"}
		}
		return preparedBatchOp{op: op, priority: *raw.Priority}, nil

	default:
		return preparedBatchOp{}, &apiError{http.StatusBadRequest, "invalid_batch", fmt.Sprintf("unsupported batch op %q", raw.Op)}
	}
}

func (s *Server) applyPreparedBatchOp(u *model.User, p preparedBatchOp, now time.Time) bool {
	switch p.op {
	case "parent_add", "parent_set", "parent_accumulate":
		_, _ = applyParentOp(&u.Parents, u, &u.PrimaryGroup, p.parentOp, now)
		return true
	case "parent_remove":
		removed := u.RemoveParent(p.group, p.contexts)
		if removed && strings.EqualFold(u.PrimaryGroup, p.group) {
			u.PrimaryGroup = s.opts.DefaultGroup
		}
		return removed
	case "primary_group_set":
		if strings.EqualFold(u.PrimaryGroup, p.group) {
			return false
		}
		u.PrimaryGroup = p.group
		return true
	case "permission_set":
		u.AddNode(p.node)
		return true
	case "permission_unset":
		return u.RemoveNode(p.key, p.contexts)
	case "meta_set":
		u.SetMeta(p.key, p.metaVal)
		return true
	case "meta_unset":
		return u.UnsetMeta(p.key)
	case "prefix_set":
		u.AddPrefix(p.affix)
		return true
	case "prefix_remove":
		return u.RemovePrefix(p.priority)
	case "suffix_set":
		u.AddSuffix(p.affix)
		return true
	case "suffix_remove":
		return u.RemoveSuffix(p.priority)
	default:
		return false
	}
}

func (s *Server) handleUserBatch(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req batchUserRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if len(req.Operations) == 0 || len(req.Operations) > maxBatchOperations {
		writeError(w, http.StatusBadRequest, "invalid_batch", "operations must contain 1-32 items")
		return
	}

	now := time.Now()
	prepared := make([]preparedBatchOp, 0, len(req.Operations))
	for _, rawOp := range req.Operations {
		p, apiErr := s.prepareBatchOp(rawOp, now)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		prepared = append(prepared, p)
	}

	u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		changed := false
		for _, p := range prepared {
			if s.applyPreparedBatchOp(u, p, now) {
				changed = true
			}
		}
		if !changed {
			return false, "", nil
		}
		return true, fmt.Sprintf("Applied batch (%d operations)", len(prepared)), nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, batchUserResponse{
		Revision: rev,
		Applied:  len(prepared),
		User:     u,
	})
}

// -----------------------------------------------------------------------------
// Group & Track Write Handlers + Admin Handlers
// -----------------------------------------------------------------------------

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req createGroupRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name, ok := validateEntityName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name must match ^[a-z0-9_+-]{1,64}$")
		return
	}
	if !validateSafeText(req.DisplayName, maxDisplayNameBytes) {
		writeError(w, http.StatusBadRequest, "invalid_body", "displayName must be at most 256 bytes without control characters")
		return
	}

	g, rev, err := s.mgr.CreateGroupWithActor(name, req.Weight, req.DisplayName, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusCreated, groupResponse{Revision: rev, Group: g})
}

func (s *Server) handlePatchGroup(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name cannot be empty")
		return
	}
	var req patchGroupRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if req.Weight == nil && req.DisplayName == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "at least one of weight or displayName must be provided")
		return
	}
	if req.DisplayName != nil && !validateSafeText(*req.DisplayName, maxDisplayNameBytes) {
		writeError(w, http.StatusBadRequest, "invalid_body", "displayName must be at most 256 bytes without control characters")
		return
	}

	g, rev, err := s.mgr.MutateGroup(name, actor, func(g *model.Group) (bool, string, error) {
		changed := false
		if req.Weight != nil && g.Weight != *req.Weight {
			g.Weight = *req.Weight
			changed = true
		}
		if req.DisplayName != nil && g.DisplayName != *req.DisplayName {
			g.DisplayName = *req.DisplayName
			changed = true
		}
		if !changed {
			return false, "", nil
		}
		return true, fmt.Sprintf("Updated group settings (weight: %d)", g.Weight), nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupResponse{Revision: rev, Group: g})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_group", "group name cannot be empty")
		return
	}
	rev, err := s.mgr.DeleteGroupWithActor(name, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, deleteGroupResponse{
		Revision: rev,
		Deleted:  true,
		Group:    name,
	})
}

func (s *Server) handleGroupAddParent(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	groupName := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	var req setParentRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	op, apiErr := s.parseParentRequest(req)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if op.group == groupName {
		writeError(w, http.StatusBadRequest, "invalid_parent", "a group cannot inherit from itself")
		return
	}

	now := time.Now()
	g, rev, err := s.mgr.MutateGroup(groupName, actor, func(g *model.Group) (bool, string, error) {
		audit, opErr := applyParentOp(&g.Parents, g, nil, op, now)
		if opErr != nil {
			return false, "", opErr
		}
		return true, audit, nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupResponse{Revision: rev, Group: g})
}

func (s *Server) handleGroupRemoveParent(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	groupName := strings.ToLower(strings.TrimSpace(r.PathValue("name")))

	rawParent := strings.TrimSpace(r.PathValue("parent"))
	var contexts map[string]string
	tempOnly := false
	if rawParent != "" {
		q := r.URL.Query()
		if strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true") {
			tempOnly = true
		}
		q.Del("tempOnly")
		var err error
		contexts, err = parseContexts(q)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
			return
		}
	} else {
		var req deleteParentRequest
		hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		if hasBody {
			rawParent = req.Group
			tempOnly = req.TempOnly
			if err := validateContexts(req.Contexts); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
				return
			}
			contexts = copyContexts(req.Contexts)
		} else {
			q := r.URL.Query()
			rawParent = q.Get("group")
			if strings.EqualFold(strings.TrimSpace(q.Get("tempOnly")), "true") {
				tempOnly = true
			}
			q.Del("group")
			q.Del("tempOnly")
			var err error
			contexts, err = parseContexts(q)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
				return
			}
		}
	}

	parentName, ok := validateEntityName(rawParent)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_group", "parent group name must match ^[a-z0-9_+-]{1,64}$")
		return
	}

	g, rev, err := s.mgr.MutateGroup(groupName, actor, func(g *model.Group) (bool, string, error) {
		var removed bool
		if tempOnly {
			removed = g.RemoveTempParent(parentName, contexts)
		} else {
			removed = g.RemoveParent(parentName, contexts)
		}
		if !removed {
			return false, "", errParentNotFound
		}
		return true, fmt.Sprintf("Removed parent group '%s'", parentName), nil
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupResponse{Revision: rev, Group: g})
}

func (s *Server) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req createTrackRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name, ok := validateEntityName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_track", "track name must match ^[a-z0-9_+-]{1,64}$")
		return
	}
	for _, g := range req.Groups {
		if _, ok := validateEntityName(g); !ok {
			writeError(w, http.StatusBadRequest, "invalid_group", "track group names must match ^[a-z0-9_+-]{1,64}$")
			return
		}
	}

	tr, rev, err := s.mgr.CreateTrackWithActor(name, req.Groups, actor)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate group") {
			writeError(w, http.StatusBadRequest, "invalid_track", err.Error())
			return
		}
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusCreated, trackResponse{Revision: rev, Track: tr})
}

func (s *Server) handlePutTrack(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_track", "track name cannot be empty")
		return
	}
	var req putTrackRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	for _, g := range req.Groups {
		if _, ok := validateEntityName(g); !ok {
			writeError(w, http.StatusBadRequest, "invalid_group", "track group names must match ^[a-z0-9_+-]{1,64}$")
			return
		}
	}

	tr, rev, err := s.mgr.SetTrackGroups(name, req.Groups, actor)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate group") {
			writeError(w, http.StatusBadRequest, "invalid_track", err.Error())
			return
		}
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, trackResponse{Revision: rev, Track: tr})
}

func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_track", "track name cannot be empty")
		return
	}
	rev, err := s.mgr.DeleteTrackWithActor(name, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, deleteTrackResponse{
		Revision: rev,
		Deleted:  true,
		Track:    name,
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if rawLimit := strings.TrimSpace(q.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_query", "limit must be a positive integer")
			return
		}
		if parsed > 1000 {
			parsed = 1000
		}
		limit = parsed
	}
	entries, rev := s.mgr.QueryLogs(limit, q.Get("targetType"), q.Get("target"), q.Get("actor"))
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, logsResponse{Revision: rev, Logs: entries})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if err := s.mgr.ReloadWithActor(actor); err != nil {
		s.writeAPIError(w, err)
		return
	}
	rev := s.mgr.Revision()
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, reloadResponse{Revision: rev, Reloaded: true})
}

func (s *Server) handleBulkUpdate(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	var req bulkUpdateRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	constraints := make([]manager.BulkConstraint, 0, len(req.Constraints))
	for _, c := range req.Constraints {
		constraints = append(constraints, manager.BulkConstraint{
			Field:    c.Field,
			Operator: c.Operator,
			Pattern:  c.Pattern,
		})
	}
	opts := manager.BulkUpdateOptions{
		Scope:       req.Scope,
		Action:      req.Action,
		Field:       req.Field,
		Value:       req.Value,
		Constraints: constraints,
	}
	var (
		summary manager.BulkUpdateSummary
		err     error
	)
	if req.DryRun {
		summary, err = s.mgr.PreviewBulkUpdate(opts)
	} else {
		summary, err = s.mgr.BulkUpdate(opts, actor)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bulk_update", err.Error())
		return
	}
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", summary.Revision))
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) parseTrackSetPayload(w http.ResponseWriter, r *http.Request) (string, string, map[string]string, bool) {
	trackName := strings.ToLower(strings.TrimSpace(r.PathValue("track")))
	tr := s.mgr.GetTrack(trackName)
	if tr == nil {
		writeError(w, http.StatusNotFound, "track_not_found", "track does not exist")
		return "", "", nil, false
	}
	var req trackSetRequest
	if apiErr := decodeStrictJSON(w, r, &req); apiErr != nil {
		s.writeAPIError(w, apiErr)
		return "", "", nil, false
	}
	targetGroup, apiErr := s.requireGroupExists(req.Group)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return "", "", nil, false
	}
	if _, ok := tr.IndexOf(targetGroup); !ok {
		writeError(w, http.StatusConflict, "track_conflict", fmt.Sprintf("group %q is not part of track %q", targetGroup, tr.Name))
		return "", "", nil, false
	}
	if err := validateContexts(req.Contexts); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return "", "", nil, false
	}
	return tr.Name, targetGroup, copyContexts(req.Contexts), true
}

func (s *Server) handleUserTrackSet(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	trackName, targetGroup, contexts, ok := s.parseTrackSetPayload(w, r)
	if !ok {
		return
	}
	if createIfMissing {
		if _, found, _ := s.mgr.UserSnapshot(uid); !found {
			_ = s.mgr.GetOrCreateUser(uid, "")
		}
	}
	u, err := s.mgr.SetUserTrackGroup(uid, trackName, targetGroup, contexts, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	rev := s.mgr.Revision()
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userRecordResponse{Revision: rev, User: u})
}

func (s *Server) handleUserTrackClear(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	uid, createIfMissing, apiErr := s.resolveWriteUser(r.PathValue("id"))
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	if createIfMissing {
		if _, found, _ := s.mgr.UserSnapshot(uid); !found {
			writeError(w, http.StatusNotFound, "user_not_found", "user does not exist")
			return
		}
	}
	contexts, err := parseContexts(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}
	u, removed, err := s.mgr.ClearUserTrack(uid, strings.ToLower(strings.TrimSpace(r.PathValue("track"))), contexts, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	rev := s.mgr.Revision()
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, userTrackClearResponse{Revision: rev, Removed: removed, User: u})
}

func (s *Server) handleGroupTrackSet(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	groupName := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if s.mgr.GetGroup(groupName) == nil {
		writeError(w, http.StatusNotFound, "group_not_found", "group does not exist")
		return
	}
	trackName, targetGroup, contexts, ok := s.parseTrackSetPayload(w, r)
	if !ok {
		return
	}
	if targetGroup == groupName {
		writeError(w, http.StatusBadRequest, "invalid_parent", "a group cannot inherit from itself")
		return
	}
	g, err := s.mgr.SetGroupTrackParent(groupName, trackName, targetGroup, contexts, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	rev := s.mgr.Revision()
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupResponse{Revision: rev, Group: g})
}

func (s *Server) handleGroupTrackClear(w http.ResponseWriter, r *http.Request) {
	actor, apiErr := parseActorHeader(r)
	if apiErr != nil {
		s.writeAPIError(w, apiErr)
		return
	}
	groupName := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	if s.mgr.GetGroup(groupName) == nil {
		writeError(w, http.StatusNotFound, "group_not_found", "group does not exist")
		return
	}
	contexts, err := parseContexts(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}
	g, removed, err := s.mgr.ClearGroupTrack(groupName, strings.ToLower(strings.TrimSpace(r.PathValue("track"))), contexts, actor)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	rev := s.mgr.Revision()
	w.Header().Set("X-StonyPerms-Revision", fmt.Sprintf("%d", rev))
	writeJSON(w, http.StatusOK, groupTrackClearResponse{Revision: rev, Removed: removed, Group: g})
}
