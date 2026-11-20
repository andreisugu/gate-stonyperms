package api

import (
	"time"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
)

type healthResponse struct {
	Status        string `json:"status"`
	SchemaVersion int    `json:"schemaVersion"`
	Revision      uint64 `json:"revision"`
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
