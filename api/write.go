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
	RemoveNode(key string, contexts map[string]string) bool
	SetMeta(key, value string)
	UnsetMeta(key string) bool
	AddPrefix(model.PriorityMeta)
	RemovePrefix(priority int) bool
	AddSuffix(model.PriorityMeta)
	RemoveSuffix(priority int) bool
	AddParent(model.Inheritance)
	RemoveParent(groupName string, contexts map[string]string) bool
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

type parsedParentOp struct {
	group           string
	mode            string
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
	switch mode {
	case "add", "set", "accumulate":
	default:
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_mode", "mode must be add, set, or accumulate"}
	}
	if err := validateContexts(req.Contexts); err != nil {
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}
	exp, apiErr := resolveExpiry(req.DurationSeconds, req.Expiry, time.Now())
	if apiErr != nil {
		return parsedParentOp{}, apiErr
	}
	if mode == "accumulate" && req.Expiry > 0 {
		return parsedParentOp{}, &apiError{http.StatusBadRequest, "invalid_expiry", "accumulate mode requires durationSeconds rather than explicit expiry"}
	}
	return parsedParentOp{
		group:           group,
		mode:            mode,
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
) string {
	switch op.mode {
	case "set":
		*parents = []model.Inheritance{{
			Group:    op.group,
			Expiry:   op.expiry,
			Contexts: copyContexts(op.contexts),
		}}
		if primaryGroup != nil {
			*primaryGroup = op.group
		}
		return fmt.Sprintf("Set parent group to '%s'", op.group)

	case "accumulate":
		targetExpiry := int64(0)
		if op.durationSeconds > 0 {
			nowUnix := now.Unix()
			targetExpiry = nowUnix + op.durationSeconds
			for _, existing := range *parents {
				if strings.EqualFold(existing.Group, op.group) && model.SameContexts(existing.Contexts, op.contexts) {
					if existing.Expiry == 0 {
						targetExpiry = 0
					} else if existing.Expiry > nowUnix {
						targetExpiry = existing.Expiry + op.durationSeconds
					}
					break
				}
			}
		}
		target.AddParent(model.Inheritance{
			Group:    op.group,
			Expiry:   targetExpiry,
			Contexts: copyContexts(op.contexts),
		})
		if primaryGroup != nil && op.setPrimary {
			*primaryGroup = op.group
		}
		return fmt.Sprintf("Accumulated parent group '%s' (+%ds)", op.group, op.durationSeconds)

	default:
		target.AddParent(model.Inheritance{
			Group:    op.group,
			Expiry:   op.expiry,
			Contexts: copyContexts(op.contexts),
		})
		if primaryGroup != nil && op.setPrimary {
			*primaryGroup = op.group
		}
		return fmt.Sprintf("Added parent group '%s'", op.group)
	}
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

func parseDeleteNodeArgs(w http.ResponseWriter, r *http.Request) (string, map[string]string, *apiError) {
	var req deletePermissionRequest
	hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
	if apiErr != nil {
		return "", nil, apiErr
	}

	rawKey := req.Key
	rawCtx := req.Contexts
	if !hasBody {
		q := r.URL.Query()
		rawKey = q.Get("key")
		q.Del("key")
		var err error
		rawCtx, err = parseContexts(q)
		if err != nil {
			return "", nil, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
		}
	} else if err := validateContexts(rawCtx); err != nil {
		return "", nil, &apiError{http.StatusBadRequest, "invalid_context", err.Error()}
	}

	key, ok := validatePermissionKey(rawKey)
	if !ok {
		return "", nil, &apiError{http.StatusBadRequest, "invalid_permission", "permission key must be 1-256 non-whitespace printable ASCII characters"}
	}
	return key, copyContexts(rawCtx), nil
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
		audit := applyParentOp(&u.Parents, u, &u.PrimaryGroup, op, now)
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
	contexts, err := parseContexts(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
		return
	}

	u, rev, err := s.mgr.MutateUser(uid, "", false, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		if !u.RemoveParent(groupName, contexts) {
			return false, "", errParentNotFound
		}
		if strings.EqualFold(u.PrimaryGroup, groupName) {
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

	u, rev, err := s.mgr.MutateUser(uid, "", createIfMissing, s.opts.DefaultGroup, actor, func(u *model.User) (bool, string, error) {
		if strings.EqualFold(u.PrimaryGroup, group) {
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
		node, apiErr := parsePermissionInput(req.Key, req.Value, req.DurationSeconds, req.Expiry, req.Contexts, time.Now())
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			target.AddNode(node)
			return true, fmt.Sprintf("Set permission '%s' to %v", node.Key, node.Value), nil
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
		key, contexts, apiErr := parseDeleteNodeArgs(w, r)
		if apiErr != nil {
			s.writeAPIError(w, apiErr)
			return
		}
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if !target.RemoveNode(key, contexts) {
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
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			target.SetMeta(key, req.Value)
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
		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if !target.UnsetMeta(key) {
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
		pm, apiErr := parseAffixInput(req, time.Now())
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
				target.AddPrefix(pm)
				return true, fmt.Sprintf("Set prefix (priority %d)", pm.Priority), nil
			}
			target.AddSuffix(pm)
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
		priorityStr := strings.TrimSpace(r.PathValue("priority"))
		var priority int
		if priorityStr != "" {
			parsed, err := strconv.Atoi(priorityStr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be an integer")
				return
			}
			priority = parsed
		} else {
			var req deleteAffixRequest
			hasBody, apiErr := decodeOptionalStrictJSON(w, r, &req)
			if apiErr != nil {
				s.writeAPIError(w, apiErr)
				return
			}
			if hasBody && req.Priority != nil {
				priority = *req.Priority
			} else if q := strings.TrimSpace(r.URL.Query().Get("priority")); q != "" {
				parsed, err := strconv.Atoi(q)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid_priority", "priority must be an integer")
					return
				}
				priority = parsed
			} else {
				writeError(w, http.StatusBadRequest, "invalid_priority", "priority is required")
				return
			}
		}

		subject := r.PathValue("id")
		if !isUser {
			subject = r.PathValue("name")
		}
		s.mutateSubject(w, isUser, subject, actor, func(target mutableTarget) (bool, string, error) {
			if isPrefix {
				if !target.RemovePrefix(priority) {
					return false, "", errPrefixNotFound
				}
				return true, fmt.Sprintf("Removed prefix (priority %d)", priority), nil
			}
			if !target.RemoveSuffix(priority) {
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
		applyParentOp(&u.Parents, u, &u.PrimaryGroup, p.parentOp, now)
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
		audit := applyParentOp(&g.Parents, g, nil, op, now)
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
	if rawParent != "" {
		var err error
		contexts, err = parseContexts(r.URL.Query())
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
			if err := validateContexts(req.Contexts); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_context", err.Error())
				return
			}
			contexts = copyContexts(req.Contexts)
		} else {
			q := r.URL.Query()
			rawParent = q.Get("group")
			q.Del("group")
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
		if !g.RemoveParent(parentName, contexts) {
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
