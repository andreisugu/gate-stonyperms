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

func TestShorthandAndRegexMatching(t *testing.T) {
	if exp := ExpandShorthand("essentials.(home|sethome)"); len(exp) != 2 || exp[0] != "essentials.home" || exp[1] != "essentials.sethome" {
		t.Fatalf("unexpected pipe expansion: %v", exp)
	}
	if exp := ExpandShorthand("essentials.sethome.multiple.{1-4}"); len(exp) != 4 || exp[3] != "essentials.sethome.multiple.4" {
		t.Fatalf("unexpected range expansion: %v", exp)
	}
	if !NewNode("essentials.{warp,tpa}.*", true).MatchesPermission("essentials.warp.spawn") {
		t.Fatal("expected shorthand wildcard match for essentials.warp.spawn")
	}
	if NewNode("essentials.{warp,tpa}", true).MatchesPermission("essentials.home") {
		t.Fatal("unexpected match for essentials.home")
	}
	reNode := Node{Key: "r=essentials\\.sethome\\.\\d+", Value: true}
	if !reNode.MatchesPermission("essentials.sethome.12") {
		t.Fatal("expected regex match for essentials.sethome.12")
	}
	if reNode.MatchesPermission("essentials.sethome.abc") {
		t.Fatal("unexpected regex match for non-digits")
	}
}
