package model

import (
	"strings"
	"time"
)

// Tristate represents the three states of a permission check: Undefined, False, or True.
type Tristate int

const (
	Undefined Tristate = 0
	False     Tristate = 1
	True      Tristate = 2
)

// ToBool converts a Tristate to a boolean with a fallback value if undefined.
func (t Tristate) ToBool(fallback bool) bool {
	switch t {
	case True:
		return true
	case False:
		return false
	default:
		return fallback
	}
}

// String returns human-readable representation of the Tristate.
func (t Tristate) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "undefined"
	}
}

// Node represents an individual permission node with optional value, expiration, and context constraints.
type Node struct {
	Key      string            `json:"key"`
	Value    bool              `json:"value"`
	Expiry   int64             `json:"expiry,omitempty"`   // 0 for permanent, unix timestamp in seconds if temporary
	Contexts map[string]string `json:"contexts,omitempty"` // e.g. {"server": "lobby"}
}

// NewNode creates a new permanent permission node with positive value.
func NewNode(key string, value bool) Node {
	return Node{
		Key:      strings.ToLower(strings.TrimSpace(key)),
		Value:    value,
		Contexts: make(map[string]string),
	}
}

// NewTempNode creates a temporary permission node with a duration from now.
func NewTempNode(key string, value bool, duration time.Duration) Node {
	expiry := time.Now().Add(duration).Unix()
	return Node{
		Key:      strings.ToLower(strings.TrimSpace(key)),
		Value:    value,
		Expiry:   expiry,
		Contexts: make(map[string]string),
	}
}

// IsExpired returns true if the node is temporary and has passed its expiration time.
func (n Node) IsExpired(now time.Time) bool {
	if n.Expiry <= 0 {
		return false
	}
	return now.Unix() >= n.Expiry
}

// ExpiryTime returns the time.Time of expiration, or zero time if permanent.
func (n Node) ExpiryTime() time.Time {
	if n.Expiry <= 0 {
		return time.Time{}
	}
	return time.Unix(n.Expiry, 0)
}

// RemainingDuration returns the remaining duration before expiration.
func (n Node) RemainingDuration(now time.Time) time.Duration {
	if n.Expiry <= 0 {
		return 0
	}
	remaining := time.Until(time.Unix(n.Expiry, 0))
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MatchesContext checks if this node's contexts are satisfied by the query context map.
// If the node has no contexts, it is global and always matches.
func (n Node) MatchesContext(queryContexts map[string]string) bool {
	if len(n.Contexts) == 0 {
		return true
	}
	if len(queryContexts) == 0 {
		return false
	}
	for k, v := range n.Contexts {
		queryVal, ok := queryContexts[k]
		if !ok || !strings.EqualFold(queryVal, v) {
			return false
		}
	}
	return true
}

// MatchesPermission evaluates if this node's key matches the target permission query.
// Supports exact matches and wildcard patterns (e.g. "*", "gate.*", "stonyperms.admin.*").
func (n Node) MatchesPermission(targetPerm string) bool {
	key := strings.ToLower(n.Key)
	target := strings.ToLower(targetPerm)

	// Exact match
	if key == target {
		return true
	}

	// Root universal wildcard
	if key == "*" {
		return true
	}

	// Sub-tree wildcard (e.g. "gate.*" matches "gate.command.server")
	if strings.HasSuffix(key, ".*") {
		prefix := key[:len(key)-1] // keep the dot "gate."
		return strings.HasPrefix(target, prefix)
	}

	return false
}

// PriorityMeta represents prefix, suffix, or weighted metadata.
type PriorityMeta struct {
	Priority int               `json:"priority"`
	Value    string            `json:"value"`
	Expiry   int64             `json:"expiry,omitempty"`
	Contexts map[string]string `json:"contexts,omitempty"`
}

// IsExpired checks if the meta node is expired.
func (pm PriorityMeta) IsExpired(now time.Time) bool {
	if pm.Expiry <= 0 {
		return false
	}
	return now.Unix() >= pm.Expiry
}

// MatchesContext checks context match for priority meta.
func (pm PriorityMeta) MatchesContext(queryContexts map[string]string) bool {
	if len(pm.Contexts) == 0 {
		return true
	}
	if len(queryContexts) == 0 {
		return false
	}
	for k, v := range pm.Contexts {
		queryVal, ok := queryContexts[k]
		if !ok || !strings.EqualFold(queryVal, v) {
			return false
		}
	}
	return true
}
