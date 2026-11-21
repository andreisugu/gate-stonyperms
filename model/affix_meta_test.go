package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTempModifiersAndMetaEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	u := NewUser(uuid.New(), "ModTester")

	// Accumulate temporary nodes
	n1 := Node{Key: "gate.fly", Value: true, Expiry: now.Add(1 * time.Hour).Unix()}
	if _, err := u.AddNodeWithModifier(n1, TempModifierAccumulate, now); err != nil {
		t.Fatalf("first AddNodeWithModifier: %v", err)
	}
	n2 := Node{Key: "gate.fly", Value: true, Expiry: now.Add(2 * time.Hour).Unix()}
	got, err := u.AddNodeWithModifier(n2, TempModifierAccumulate, now)
	if err != nil {
		t.Fatalf("accumulate AddNodeWithModifier: %v", err)
	}
	wantExp := now.Add(3 * time.Hour).Unix()
	if got.Expiry != wantExp {
		t.Fatalf("accumulated expiry = %d, want %d", got.Expiry, wantExp)
	}

	// Deny rejects active duplicate
	n3 := Node{Key: "gate.fly", Value: true, Expiry: now.Add(30 * time.Minute).Unix()}
	if _, err := u.AddNodeWithModifier(n3, TempModifierDeny, now); err != ErrTempEntryExists {
		t.Fatalf("expected ErrTempEntryExists for deny, got %v", err)
	}

	// Replace keeps longest remaining expiry
	n4 := Node{Key: "gate.fly", Value: true, Expiry: now.Add(30 * time.Minute).Unix()}
	rep, err := u.AddNodeWithModifier(n4, TempModifierReplace, now)
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	if rep.Expiry != wantExp {
		t.Fatalf("replace should keep longest expiry %d, got %d", wantExp, rep.Expiry)
	}

	// Contextual + temporary meta entries do not leak into global static Meta map
	if _, err := u.SetMetaEntry(MetaEntry{
		Key:      "homes",
		Value:    "7",
		Contexts: map[string]string{"server": "survival"},
		Expiry:   now.Add(1 * time.Hour).Unix(),
	}, TempModifierReplace, now); err != nil {
		t.Fatalf("SetMetaEntry failed: %v", err)
	}
	if _, exists := u.Meta["homes"]; exists {
		t.Fatalf("contextual/temp meta entry leaked into global Meta map")
	}
	if len(u.MetaEntries) != 1 {
		t.Fatalf("expected 1 MetaEntry, got %d", len(u.MetaEntries))
	}

	// CleanExpired removes expired entries
	if cleaned := u.CleanExpired(now.Add(4 * time.Hour)); cleaned != 2 {
		t.Fatalf("expected 2 expired items cleaned (node + metaEntry), got %d", cleaned)
	}
}
