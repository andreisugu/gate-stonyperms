package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

type healthResponse struct {
	Status        string `json:"status"`
	SchemaVersion int    `json:"schemaVersion"`
	Revision      uint64 `json:"revision"`
	WriteEnabled  bool   `json:"writeEnabled"`
	SSEEnabled    bool   `json:"sseEnabled"`
}

type revisionResponse struct {
	Revision uint64 `json:"revision"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

type groupsResponse struct {
	Revision uint64         `json:"revision"`
	Groups   []*model.Group `json:"groups"`
}

type groupResponse struct {
	Revision uint64       `json:"revision"`
	Group    *model.Group `json:"group"`
}

type groupMembersResponse struct {
	Revision uint64                    `json:"revision"`
	Group    string                    `json:"group"`
	Members  []manager.GroupMemberInfo `json:"members"`
}

type tracksResponse struct {
	Revision uint64         `json:"revision"`
	Tracks   []*model.Track `json:"tracks"`
}

type trackResponse struct {
	Revision uint64       `json:"revision"`
	Track    *model.Track `json:"track"`
}

type logsResponse struct {
	Revision uint64           `json:"revision"`
	Logs     []model.LogEntry `json:"logs"`
}

type reloadResponse struct {
	Revision uint64 `json:"revision"`
	Reloaded bool   `json:"reloaded"`
}

type checkRequest struct {
	UUID       string            `json:"uuid"`
	Permission string            `json:"permission"`
	Contexts   map[string]string `json:"contexts,omitempty"`
}

type checkResponse struct {
	Value    string `json:"value"`
	Source   string `json:"source"`
	Revision uint64 `json:"revision"`
}

type playersBatchRequest struct {
	UUIDs    []string          `json:"uuids"`
	Contexts map[string]string `json:"contexts,omitempty"`
}

type playersBatchResponse struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Revision      uint64                      `json:"revision"`
	Players       map[string]snapshotResponse `json:"players"`
}

type userSummaryDTO struct {
	UUID         string              `json:"uuid"`
	Username     string              `json:"username"`
	PrimaryGroup string              `json:"primaryGroup"`
	Parents      []model.Inheritance `json:"parents,omitempty"`
	NodeCount    int                 `json:"nodeCount"`
}

type usersResponse struct {
	Revision uint64           `json:"revision"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
	Users    []userSummaryDTO `json:"users"`
}

type userRecordResponse struct {
	Revision uint64      `json:"revision"`
	User     *model.User `json:"user"`
}

type ensureUserRequest struct {
	UUID          string `json:"uuid"`
	Username      string `json:"username"`
	PrimaryGroup  string `json:"primaryGroup"`
	CreateOffline bool   `json:"createOffline"`
}

type ensureUserResponse struct {
	Revision uint64      `json:"revision"`
	Created  bool        `json:"created"`
	User     *model.User `json:"user"`
}

type deleteUserResponse struct {
	Revision uint64 `json:"revision"`
	Deleted  bool   `json:"deleted"`
	UUID     string `json:"uuid"`
}

type deleteGroupResponse struct {
	Revision uint64 `json:"revision"`
	Deleted  bool   `json:"deleted"`
	Group    string `json:"group"`
}

type deleteTrackResponse struct {
	Revision uint64 `json:"revision"`
	Deleted  bool   `json:"deleted"`
	Track    string `json:"track"`
}

type setParentRequest struct {
	Group           string            `json:"group"`
	Mode            string            `json:"mode"`
	Modifier        string            `json:"modifier"`
	DurationSeconds int64             `json:"durationSeconds"`
	Expiry          int64             `json:"expiry"`
	Contexts        map[string]string `json:"contexts"`
	SetPrimary      bool              `json:"setPrimary"`
}

type deleteParentRequest struct {
	Group    string            `json:"group"`
	Contexts map[string]string `json:"contexts"`
	TempOnly bool              `json:"tempOnly"`
}

type setPrimaryGroupRequest struct {
	Group string `json:"group"`
}

type setPermissionRequest struct {
	Key             string            `json:"key"`
	Value           *bool             `json:"value"`
	Modifier        string            `json:"modifier"`
	DurationSeconds int64             `json:"durationSeconds"`
	Expiry          int64             `json:"expiry"`
	Contexts        map[string]string `json:"contexts"`
}

type deletePermissionRequest struct {
	Key      string            `json:"key"`
	Contexts map[string]string `json:"contexts"`
	TempOnly bool              `json:"tempOnly"`
}

type setMetaRequest struct {
	Value           string            `json:"value"`
	DurationSeconds int64             `json:"durationSeconds"`
	Expiry          int64             `json:"expiry"`
	Contexts        map[string]string `json:"contexts"`
	Modifier        string            `json:"modifier"`
}

type deleteMetaRequest struct {
	Contexts map[string]string `json:"contexts"`
	TempOnly bool              `json:"tempOnly"`
}

type setAffixRequest struct {
	Priority        int               `json:"priority"`
	Value           string            `json:"value"`
	Mode            string            `json:"mode"`
	Modifier        string            `json:"modifier"`
	DurationSeconds int64             `json:"durationSeconds"`
	Expiry          int64             `json:"expiry"`
	Contexts        map[string]string `json:"contexts"`
}

type deleteAffixRequest struct {
	Priority *int              `json:"priority"`
	Value    string            `json:"value"`
	Contexts map[string]string `json:"contexts"`
	TempOnly bool              `json:"tempOnly"`
}

type searchResponse struct {
	Revision  uint64              `json:"revision"`
	Query     string              `json:"query"`
	GroupHits []manager.SearchHit `json:"groupHits"`
	UserHits  []manager.SearchHit `json:"userHits"`
}

type bulkConstraintRequest struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Pattern  string `json:"pattern"`
}

type bulkUpdateRequest struct {
	Scope       string                  `json:"scope"`
	Action      string                  `json:"action"`
	Field       string                  `json:"field"`
	Value       string                  `json:"value"`
	Constraints []bulkConstraintRequest `json:"constraints"`
	DryRun      bool                    `json:"dryRun"`
}

type trackSetRequest struct {
	Group    string            `json:"group"`
	Contexts map[string]string `json:"contexts"`
}

type userTrackClearResponse struct {
	Revision uint64      `json:"revision"`
	Removed  bool        `json:"removed"`
	User     *model.User `json:"user"`
}

type groupTrackClearResponse struct {
	Revision uint64       `json:"revision"`
	Removed  bool         `json:"removed"`
	Group    *model.Group `json:"group"`
}

type trackActionRequest struct {
	Track string `json:"track"`
}

type trackActionResponse struct {
	Revision  uint64      `json:"revision"`
	FromGroup string      `json:"fromGroup"`
	ToGroup   string      `json:"toGroup"`
	User      *model.User `json:"user"`
}

type clearUserRequest struct {
	Scope string `json:"scope"`
}

type batchUserOperation struct {
	Op              string            `json:"op"`
	Group           string            `json:"group"`
	Key             string            `json:"key"`
	Value           json.RawMessage   `json:"value"`
	Priority        *int              `json:"priority"`
	DurationSeconds int64             `json:"durationSeconds"`
	Expiry          int64             `json:"expiry"`
	Contexts        map[string]string `json:"contexts"`
	SetPrimary      bool              `json:"setPrimary"`
}

type batchUserRequest struct {
	Operations []batchUserOperation `json:"operations"`
}

type batchUserResponse struct {
	Revision uint64      `json:"revision"`
	Applied  int         `json:"applied"`
	User     *model.User `json:"user"`
}

type createGroupRequest struct {
	Name        string `json:"name"`
	Weight      int    `json:"weight"`
	DisplayName string `json:"displayName"`
}

type patchGroupRequest struct {
	Weight      *int    `json:"weight"`
	DisplayName *string `json:"displayName"`
}

type createTrackRequest struct {
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

type putTrackRequest struct {
	Groups []string `json:"groups"`
}

type snapshotResponse struct {
	SchemaVersion int        `json:"schemaVersion"`
	Revision      uint64     `json:"revision"`
	GeneratedAt   string     `json:"generatedAt"`
	TTLSeconds    int        `json:"ttlSeconds"`
	User          userDTO    `json:"user"`
	Groups        []groupDTO `json:"groups"`
	Nodes         []nodeDTO  `json:"nodes"`
}

type userDTO struct {
	UUID         string            `json:"uuid"`
	Username     string            `json:"username"`
	PrimaryGroup string            `json:"primaryGroup"`
	Synthetic    bool              `json:"synthetic"`
	Meta         map[string]string `json:"meta,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`
	Suffix       string            `json:"suffix,omitempty"`
}

type groupDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Weight      int    `json:"weight"`
}

// nodeDTO carries the source ordering used by the StonyPerms engine. Sources
// are ordered: user first, then groups in the order they appear in the
// snapshot's groups array (descending weight). Within one source, an exact
// node beats a wildcard, and the most specific wildcard beats a shorter one.
type nodeDTO struct {
	Key      string            `json:"key"`
	Value    bool              `json:"value"`
	Contexts map[string]string `json:"contexts,omitempty"`
	Expiry   int64             `json:"expiry,omitempty"`
	Source   string            `json:"source"`
	Weight   int               `json:"weight"`
	User     bool              `json:"user"`
}

func newSnapshotResponse(eff *manager.EffectiveUser, found bool, revision uint64, ttl time.Duration) snapshotResponse {
	groups := make([]groupDTO, 0, len(eff.Groups))
	for _, g := range eff.Groups {
		groups = append(groups, groupDTO{
			Name:        g.Name,
			DisplayName: g.DisplayName,
			Weight:      g.Weight,
		})
	}

	nodes := make([]nodeDTO, 0, len(eff.Nodes))
	for _, n := range eff.Nodes {
		nodes = append(nodes, nodeDTO{
			Key:      n.Node.Key,
			Value:    n.Node.Value,
			Contexts: n.Node.Contexts,
			Expiry:   n.Node.Expiry,
			Source:   n.Source,
			Weight:   n.Weight,
			User:     n.User,
		})
	}

	seconds := int(ttl.Seconds())
	if seconds < 1 {
		seconds = 1
	}

	meta := make(map[string]string, len(eff.Meta))
	for k, v := range eff.Meta {
		meta[k] = v
	}

	return snapshotResponse{
		SchemaVersion: SchemaVersion,
		Revision:      revision,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		TTLSeconds:    seconds,
		User: userDTO{
			UUID:         eff.UUID.String(),
			Username:     eff.Username,
			PrimaryGroup: eff.PrimaryGroup,
			Synthetic:    !found,
			Meta:         meta,
			Prefix:       eff.Prefix,
			Suffix:       eff.Suffix,
		},
		Groups: groups,
		Nodes:  nodes,
	}
}

// snapshotETag combines a restart generation, the manager revision, and a
// fingerprint of the effective snapshot. The fingerprint is what makes
// temporary-node/parent expiry change the validator between cleanup ticks;
// the generation prevents a restarted proxy from reusing an old validator.
func snapshotETag(generation string, revision uint64, snapshot snapshotResponse) string {
	payload := struct {
		User   userDTO    `json:"user"`
		Groups []groupDTO `json:"groups"`
		Nodes  []nodeDTO  `json:"nodes"`
	}{
		User:   snapshot.User,
		Groups: snapshot.Groups,
		Nodes:  snapshot.Nodes,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`"sp-%s-%d"`, generation, revision)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf(`"sp-%s-%d-%s"`, generation, revision, hex.EncodeToString(sum[:8]))
}
