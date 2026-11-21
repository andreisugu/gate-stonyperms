package model

import (
	"regexp"
	"strconv"
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

const maxShorthandExpansions = 128

// ExpandShorthand expands shorthand permission patterns such as "{a,b}",
// "(a|b)", and integer ranges "{1-5}" up to maxShorthandExpansions items.
// Regex keys prefixed with "r=" are returned unmodified.
func ExpandShorthand(pattern string) []string {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "r=") {
		return []string{trimmed}
	}
	cur := []string{trimmed}
	for step := 0; step < 8; step++ {
		changed := false
		next := make([]string, 0, len(cur))
		for _, item := range cur {
			start, end, closeChar := findFirstShorthandGroup(item)
			if start < 0 {
				next = append(next, item)
				continue
			}
			_ = closeChar
			inner := item[start+1 : end]
			variants := expandShorthandSegment(inner)
			if len(variants) == 0 {
				next = append(next, item)
				continue
			}
			changed = true
			prefix := item[:start]
			suffix := item[end+1:]
			for _, v := range variants {
				next = append(next, prefix+v+suffix)
				if len(next) >= maxShorthandExpansions {
					break
				}
			}
			if len(next) >= maxShorthandExpansions {
				break
			}
		}
		cur = next
		if !changed || len(cur) >= maxShorthandExpansions {
			break
		}
	}
	return cur
}

func findFirstShorthandGroup(s string) (int, int, byte) {
	for i := 0; i < len(s); i++ {
		var want byte
		switch s[i] {
		case '{':
			want = '}'
		case '(':
			want = ')'
		default:
			continue
		}
		for j := i + 1; j < len(s); j++ {
			if s[j] == want {
				return i, j, want
			}
		}
	}
	return -1, -1, 0
}

func expandShorthandSegment(inner string) []string {
	sep := ","
	if strings.Contains(inner, "|") {
		sep = "|"
	} else if !strings.Contains(inner, ",") {
		if parts := strings.SplitN(inner, "-", 2); len(parts) == 2 {
			a, errA := strconv.Atoi(strings.TrimSpace(parts[0]))
			b, errB := strconv.Atoi(strings.TrimSpace(parts[1]))
			if errA == nil && errB == nil && a >= 0 && b >= 0 {
				step := 1
				if b < a {
					step = -1
				}
				var out []string
				for v := a; ; v += step {
					out = append(out, strconv.Itoa(v))
					if v == b || len(out) >= maxShorthandExpansions {
						break
					}
				}
				return out
			}
		}
	}
	rawParts := strings.Split(inner, sep)
	if len(rawParts) <= 1 {
		return nil
	}
	out := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// EvaluateNodeMatch checks if nodeKey matches targetPerm and returns
// (matched, isExact, specificityScore). Supports literal nodes, "*", sub-wildcards ("gate.*"),
// Shorthand patterns ("{a,b}", "(a|b)", "{1-20}"), and regex ("r=...").
func EvaluateNodeMatch(nodeKey, targetPerm string) (bool, bool, int) {
	k := strings.TrimSpace(nodeKey)
	t := strings.ToLower(strings.TrimSpace(targetPerm))
	if k == "" || t == "" {
		return false, false, 0
	}

	lk := strings.ToLower(k)
	if strings.HasPrefix(lk, "r=") {
		pat := strings.TrimSpace(k[2:])
		if pat == "" {
			return false, false, 0
		}
		re, err := regexp.Compile("(?i)^(?:" + pat + ")$")
		if err != nil || !re.MatchString(t) {
			return false, false, 0
		}
		return true, false, len(pat)
	}

	if !strings.ContainsAny(lk, "{(") {
		if lk == t {
			return true, true, len(t) + 1
		}
		if lk == "*" {
			return true, false, 1
		}
		if strings.HasSuffix(lk, ".*") {
			prefix := lk[:len(lk)-1]
			if strings.HasPrefix(t, prefix) {
				return true, false, len(prefix)
			}
		}
		return false, false, 0
	}

	expanded := ExpandShorthand(lk)
	matched := false
	isExact := false
	bestScore := 0
	for _, cand := range expanded {
		if cand == t {
			return true, true, len(t)
		}
		if cand == "*" && 1 > bestScore {
			matched = true
			bestScore = 1
		} else if strings.HasSuffix(cand, ".*") {
			prefix := cand[:len(cand)-1]
			if strings.HasPrefix(t, prefix) && len(prefix) > bestScore {
				matched = true
				bestScore = len(prefix)
			}
		}
	}
	return matched, isExact, bestScore
}

// MatchesPermission evaluates if this node's key matches the target permission query.
// Supports exact matches, wildcards, shorthand "{a,b}"/"(a|b)"/"{1-N}", and "r=" regex patterns.
func (n Node) MatchesPermission(targetPerm string) bool {
	matched, _, _ := EvaluateNodeMatch(n.Key, targetPerm)
	return matched
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
