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

func TestRegexCacheAndDeepCloneContextIsolation(t *testing.T) {
	pat := `^essentials\.(fly|back)$`
	ok, exact, score := EvaluateNodeMatch("r="+pat, "essentials.fly")
	if !ok || exact || score != len(pat) {
		t.Fatalf("expected regex match (ok=%v exact=%v score=%d)", ok, exact, score)
	}
	if _, cached := compiledRegexCache.Load(pat); !cached {
		t.Fatal("expected compiled regex to be cached after first evaluation")
	}
	if ok2, _, _ := EvaluateNodeMatch("r="+pat, "essentials.home"); ok2 {
		t.Fatal("did not expect essentials.home to match fly|back regex")
	}

	g := NewGroup("vip", 20)
	g.AddParent(Inheritance{Group: "default", Contexts: map[string]string{"server": "lobby"}})
	g.AddNode(Node{Key: "gate.command.ping", Value: true, Contexts: map[string]string{"server": "lobby"}})
	g.AddPrefix(PriorityMeta{Priority: 10, Value: "[VIP] ", Contexts: map[string]string{"server": "lobby"}})
	g.AddSuffix(PriorityMeta{Priority: 10, Value: " [L]", Contexts: map[string]string{"server": "lobby"}})

	gc := g.Clone("vip2")
	gc.Parents[0].Contexts["server"] = "survival"
	gc.Nodes[0].Contexts["server"] = "survival"
	gc.Prefixes[0].Contexts["server"] = "survival"
	gc.Suffixes[0].Contexts["server"] = "survival"

	if g.Parents[0].Contexts["server"] != "lobby" ||
		g.Nodes[0].Contexts["server"] != "lobby" ||
		g.Prefixes[0].Contexts["server"] != "lobby" ||
		g.Suffixes[0].Contexts["server"] != "lobby" {
		t.Fatal("Group.Clone mutated context maps on original group")
	}

	u := NewUser(uuid.New(), "Src")
	u.AddParent(Inheritance{Group: "vip", Contexts: map[string]string{"server": "hub"}})
	u.AddNode(Node{Key: "test.perm", Value: true, Contexts: map[string]string{"server": "hub"}})
	u.AddPrefix(PriorityMeta{Priority: 5, Value: "[A] ", Contexts: map[string]string{"server": "hub"}})
	u.AddSuffix(PriorityMeta{Priority: 5, Value: " [B]", Contexts: map[string]string{"server": "hub"}})

	uc := u.Clone(uuid.New(), "Dst")
	uc.Parents[0].Contexts["server"] = "creative"
	uc.Nodes[0].Contexts["server"] = "creative"
	uc.Prefixes[0].Contexts["server"] = "creative"
	uc.Suffixes[0].Contexts["server"] = "creative"

	if u.Parents[0].Contexts["server"] != "hub" ||
		u.Nodes[0].Contexts["server"] != "hub" ||
		u.Prefixes[0].Contexts["server"] != "hub" ||
		u.Suffixes[0].Contexts["server"] != "hub" {
		t.Fatal("User.Clone mutated context maps on original user")
	}
}
