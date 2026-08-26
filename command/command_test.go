package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.minekube.com/brigodier"
	c "go.minekube.com/common/minecraft/component"
	"go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/util/permission"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/storage"
)

type dummySource struct {
	messages []string
}

func (d *dummySource) SendMessage(message c.Component, opts ...command.MessageOption) error {
	if textComp, ok := message.(*c.Text); ok {
		d.messages = append(d.messages, textComp.Content)
	} else {
		d.messages = append(d.messages, fmtComponent(message))
	}
	return nil
}

func (d *dummySource) HasPermission(perm string) bool {
	return true
}

func (d *dummySource) PermissionValue(perm string) permission.TriState {
	return permission.True
}

func fmtComponent(comp c.Component) string {
	if comp == nil {
		return ""
	}
	if t, ok := comp.(*c.Text); ok {
		return t.Content
	}
	return "component"
}

func setupTestManager(t *testing.T) (*manager.Manager, func()) {
	tmpDir, err := os.MkdirTemp("", "stonyperms_cmd_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	store, err := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	cleanup := func() {
		mgr.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return mgr, cleanup
}

func TestParseCommandLine(t *testing.T) {
	cases := []struct {
		input    string
		expected []string
	}{
		{"user Andrei permission set gate.command.server true", []string{"user", "Andrei", "permission", "set", "gate.command.server", "true"}},
		{"user Andrei meta setprefix 100 \"&c[Admin] \"", []string{"user", "Andrei", "meta", "setprefix", "100", "&c[Admin] "}},
		{"group vip permission settemp 'gate.fly' true 2h server=lobby", []string{"group", "vip", "permission", "settemp", "gate.fly", "true", "2h", "server=lobby"}},
	}

	for _, tc := range cases {
		got := ParseCommandLine(tc.input)
		if len(got) != len(tc.expected) {
			t.Fatalf("for %q, expected %d args, got %d: %v", tc.input, len(tc.expected), len(got), got)
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Fatalf("for %q, arg[%d] expected %q, got %q", tc.input, i, tc.expected[i], got[i])
			}
		}
	}
}

func TestParseDuration(t *testing.T) {
	d, err := ParseDuration("10s")
	if err != nil || d != 10*time.Second {
		t.Fatalf("expected 10s, got %v (%v)", d, err)
	}

	d, err = ParseDuration("5m")
	if err != nil || d != 5*time.Minute {
		t.Fatalf("expected 5m, got %v (%v)", d, err)
	}

	d, err = ParseDuration("2h")
	if err != nil || d != 2*time.Hour {
		t.Fatalf("expected 2h, got %v (%v)", d, err)
	}

	d, err = ParseDuration("7d")
	if err != nil || d != 7*24*time.Hour {
		t.Fatalf("expected 7d, got %v (%v)", d, err)
	}
}

func TestCommandDispatchFullFlow(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	src := &dummySource{}

	// 1. /sp
	if err := Dispatch(mgr, src, []string{}); err != nil {
		t.Fatalf("failed root dispatch: %v", err)
	}

	// 2. /sp info
	if err := Dispatch(mgr, src, []string{"info"}); err != nil {
		t.Fatalf("failed info dispatch: %v", err)
	}

	// 3. /sp creategroup vip 50 &aVIP
	if err := Dispatch(mgr, src, []string{"creategroup", "vip", "50", "&aVIP"}); err != nil {
		t.Fatalf("failed creategroup: %v", err)
	}

	// 4. /sp group vip permission set gate.command.server true
	if err := Dispatch(mgr, src, []string{"group", "vip", "permission", "set", "gate.command.server", "true"}); err != nil {
		t.Fatalf("failed group perm set: %v", err)
	}

	// 5. /sp user Andrei parent add vip
	if err := Dispatch(mgr, src, []string{"user", "Andrei", "parent", "add", "vip"}); err != nil {
		t.Fatalf("failed user parent add: %v", err)
	}

	// 6. /sp user Andrei permission set gate.chat.color true
	if err := Dispatch(mgr, src, []string{"user", "Andrei", "permission", "set", "gate.chat.color", "true"}); err != nil {
		t.Fatalf("failed user perm set: %v", err)
	}

	// 7. /sp check Andrei gate.command.server
	if err := Dispatch(mgr, src, []string{"check", "Andrei", "gate.command.server"}); err != nil {
		t.Fatalf("failed check: %v", err)
	}

	// 8. /sp log recent
	if err := Dispatch(mgr, src, []string{"log", "recent"}); err != nil {
		t.Fatalf("failed log recent: %v", err)
	}

	// Verify user permissions evaluated
	u := mgr.GetUserByName("Andrei")
	if u == nil {
		t.Fatalf("expected user Andrei to exist")
	}
	if !mgr.HasPermission(u, "gate.command.server", nil) {
		t.Fatalf("expected user Andrei to have inherited gate.command.server from vip group")
	}
	if !mgr.HasPermission(u, "gate.chat.color", nil) {
		t.Fatalf("expected user Andrei to have direct gate.chat.color")
	}
}

func TestSuggestionsExactRange(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Provision user "RestlessStone"
	_ = mgr.GetOrCreateUser([16]byte{1, 2, 3, 4}, "RestlessStone")

	suggester := buildSuggestions(nil, mgr)

	// Case 1: "/sp user " -> should suggest "RestlessStone" starting at index 9 (after "user ")
	builder1 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user ",
		InputLowerCase: "/sp user ",
		Start:          4, // start of args
		Remaining:      "user ",
	}
	res1 := suggester(nil, builder1)
	if len(res1.Suggestions) == 0 {
		t.Fatalf("expected suggestions for '/sp user '")
	}
	if res1.Range.Start != 9 {
		t.Fatalf("expected suggestion Range.Start to be 9, got %d", res1.Range.Start)
	}

	// Case 2: "/sp group " -> should suggest "admin" starting at index 10 (after "group ")
	builder2 := &brigodier.SuggestionsBuilder{
		Input:          "/sp group ",
		InputLowerCase: "/sp group ",
		Start:          4,
		Remaining:      "group ",
	}
	res2 := suggester(nil, builder2)
	if len(res2.Suggestions) == 0 {
		t.Fatalf("expected suggestions for '/sp group '")
	}
	if res2.Range.Start != 10 {
		t.Fatalf("expected suggestion Range.Start to be 10, got %d", res2.Range.Start)
	}

	// Case 3: "/sp user Rest" -> should suggest "RestlessStone" replacing from index 9 to 13
	builder3 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user Rest",
		InputLowerCase: "/sp user rest",
		Start:          4,
		Remaining:      "user Rest",
	}
	res3 := suggester(nil, builder3)
	if len(res3.Suggestions) == 0 {
		t.Fatalf("expected suggestions for '/sp user Rest'")
	}
	if res3.Range.Start != 9 || res3.Range.End != 13 {
		t.Fatalf("expected suggestion Range {9, 13}, got {%d, %d}", res3.Range.Start, res3.Range.End)
	}
	if res3.Suggestions[0].Text != "RestlessStone" {
		t.Fatalf("expected suggestion text 'RestlessStone', got %q", res3.Suggestions[0].Text)
	}
}
