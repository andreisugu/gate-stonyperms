package model

import (
	"strings"
	"time"
)

// Inheritance represents a parent group relationship with optional expiration and context.
type Inheritance struct {
	Group    string            `json:"group"`
	Expiry   int64             `json:"expiry,omitempty"`
	Contexts map[string]string `json:"contexts,omitempty"`
}

// NewInheritance creates a new permanent parent inheritance.
func NewInheritance(group string) Inheritance {
	return Inheritance{
		Group:    strings.ToLower(strings.TrimSpace(group)),
		Contexts: make(map[string]string),
	}
}

// NewTempInheritance creates a temporary parent inheritance.
func NewTempInheritance(group string, duration time.Duration) Inheritance {
	return Inheritance{
		Group:    strings.ToLower(strings.TrimSpace(group)),
		Expiry:   time.Now().Add(duration).Unix(),
		Contexts: make(map[string]string),
	}
}

// IsExpired checks if the inheritance has expired.
func (inh Inheritance) IsExpired(now time.Time) bool {
	if inh.Expiry <= 0 {
		return false
	}
	return now.Unix() >= inh.Expiry
}

// MatchesContext checks if this parent relationship is active in the given context.
func (inh Inheritance) MatchesContext(queryContexts map[string]string) bool {
	if len(inh.Contexts) == 0 {
		return true
	}
	if len(queryContexts) == 0 {
		return false
	}
	for k, v := range inh.Contexts {
		queryVal, ok := queryContexts[k]
		if !ok || !strings.EqualFold(queryVal, v) {
			return false
		}
	}
	return true
}
