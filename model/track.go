package model

import (
	"strings"
)

// Track represents an ordered promotion progression track of groups (e.g. default -> vip -> mod -> admin).
type Track struct {
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

// NewTrack creates a new named track.
func NewTrack(name string) *Track {
	return &Track{
		Name:   strings.ToLower(strings.TrimSpace(name)),
		Groups: make([]string, 0),
	}
}

// Append adds a group to the end of the track.
func (t *Track) Append(group string) bool {
	group = strings.ToLower(strings.TrimSpace(group))
	if _, exists := t.IndexOf(group); exists {
		return false
	}
	t.Groups = append(t.Groups, group)
	return true
}

// Insert inserts a group at the specified 0-based position.
func (t *Track) Insert(group string, pos int) bool {
	group = strings.ToLower(strings.TrimSpace(group))
	if _, exists := t.IndexOf(group); exists {
		return false
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= len(t.Groups) {
		t.Groups = append(t.Groups, group)
		return true
	}
	t.Groups = append(t.Groups[:pos], append([]string{group}, t.Groups[pos:]...)...)
	return true
}

// Remove removes a group from the track.
func (t *Track) Remove(group string) bool {
	idx, exists := t.IndexOf(group)
	if !exists {
		return false
	}
	t.Groups = append(t.Groups[:idx], t.Groups[idx+1:]...)
	return true
}

// IndexOf returns the position of a group in the track.
func (t *Track) IndexOf(group string) (int, bool) {
	group = strings.ToLower(strings.TrimSpace(group))
	for i, g := range t.Groups {
		if strings.EqualFold(g, group) {
			return i, true
		}
	}
	return -1, false
}

// NextGroup returns the next group in the progression after currentGroup.
func (t *Track) NextGroup(currentGroup string) (string, bool) {
	idx, exists := t.IndexOf(currentGroup)
	if !exists {
		// If current group is not in track, return the first group in track
		if len(t.Groups) > 0 {
			return t.Groups[0], true
		}
		return "", false
	}
	if idx+1 < len(t.Groups) {
		return t.Groups[idx+1], true
	}
	return "", false // Already at the top of the track
}

// PreviousGroup returns the previous group in the progression before currentGroup.
func (t *Track) PreviousGroup(currentGroup string) (string, bool) {
	idx, exists := t.IndexOf(currentGroup)
	if !exists {
		return "", false
	}
	if idx > 0 {
		return t.Groups[idx-1], true
	}
	return "", false // Already at the bottom of the track
}

// Clear clears all groups from this track.
func (t *Track) Clear() {
	t.Groups = make([]string, 0)
}
