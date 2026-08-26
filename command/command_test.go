package command

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	d.messages = append(d.messages, fmtComponent(message))
	return nil
}

func (d *dummySource) HasPermission(perm string) bool {
	return true
}

func (d *dummySource) PermissionValue(perm string) permission.TriState {
	return permission.True
}

type limitedSource struct {
	messages []string
	perms    map[string]bool
}

func (l *limitedSource) SendMessage(message c.Component, opts ...command.MessageOption) error {
	l.messages = append(l.messages, fmtComponent(message))
	return nil
}

func (l *limitedSource) HasPermission(perm string) bool {
	return l.perms[perm]
}

func (l *limitedSource) PermissionValue(perm string) permission.TriState {
	if l.perms[perm] {
		return permission.True
	}
	return permission.False
}

func fmtComponent(comp c.Component) string {
	if comp == nil {
		return ""
	}
	if t, ok := comp.(*c.Text); ok {
		var b strings.Builder
		b.WriteString(t.Content)
		for _, child := range t.Extra {
			b.WriteString(fmtComponent(child))
		}
		return b.String()
	}
	return ""
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

	// Case 0: "/sp " -> should suggest root subcommands (user, group, info, etc.) starting at index 4
	builder0 := &brigodier.SuggestionsBuilder{
		Input:          "/sp ",
		InputLowerCase: "/sp ",
		Start:          4,
		Remaining:      "",
	}
	res0 := suggester(nil, builder0)
	if len(res0.Suggestions) == 0 {
		t.Fatalf("expected suggestions for root '/sp '")
	}
	if res0.Range.Start != 4 {
		t.Fatalf("expected root suggestion Range.Start to be 4, got %d", res0.Range.Start)
	}

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
	// Case 4: "/sp user RestlessStone " -> should suggest user subcommands
	builder4 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user RestlessStone ",
		InputLowerCase: "/sp user restlessstone ",
		Start:          4,
		Remaining:      "user RestlessStone ",
	}
	res4 := suggester(nil, builder4)
	if len(res4.Suggestions) < 5 {
		t.Fatalf("expected subcommands for user, got %d", len(res4.Suggestions))
	}
	if res4.Range.Start != len(builder4.Input) {
		t.Fatalf("expected suggestion start at end of input %d, got %d", len(builder4.Input), res4.Range.Start)
	}

	// Case 5: "/sp user RestlessStone permission " -> should suggest "set", "unset", "settemp", etc.
	builder5 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user RestlessStone permission ",
		InputLowerCase: "/sp user restlessstone permission ",
		Start:          4,
		Remaining:      "user RestlessStone permission ",
	}
	res5 := suggester(nil, builder5)
	if len(res5.Suggestions) == 0 || res5.Suggestions[0].Text != "set" {
		t.Fatalf("expected 'set' as first permission action, got %v", res5.Suggestions)
	}

	// Case 6: "/sp user RestlessStone permission set " -> should suggest permission nodes
	builder6 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user RestlessStone permission set ",
		InputLowerCase: "/sp user restlessstone permission set ",
		Start:          4,
		Remaining:      "user RestlessStone permission set ",
	}
	res6 := suggester(nil, builder6)
	if len(res6.Suggestions) == 0 {
		t.Fatalf("expected permission nodes suggestions")
	}

	// Case 7: "/sp user RestlessStone permission set gate.command.server " -> should suggest "true", "false"
	builder7 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user RestlessStone permission set gate.command.server ",
		InputLowerCase: "/sp user restlessstone permission set gate.command.server ",
		Start:          4,
		Remaining:      "user RestlessStone permission set gate.command.server ",
	}
	res7 := suggester(nil, builder7)
	if len(res7.Suggestions) != 2 || res7.Suggestions[0].Text != "true" || res7.Suggestions[1].Text != "false" {
		t.Fatalf("expected ['true', 'false'], got %v", res7.Suggestions)
	}

	// Case 8: "/sp user RestlessStone permission set gate.command.server true " -> should suggest "server=..."
	builder8 := &brigodier.SuggestionsBuilder{
		Input:          "/sp user RestlessStone permission set gate.command.server true ",
		InputLowerCase: "/sp user restlessstone permission set gate.command.server true ",
		Start:          4,
		Remaining:      "user RestlessStone permission set gate.command.server true ",
	}
	res8 := suggester(nil, builder8)
	if len(res8.Suggestions) == 0 || !strings.HasPrefix(res8.Suggestions[0].Text, "server=") {
		t.Fatalf("expected context suggestions starting with 'server=', got %v", res8.Suggestions)
	}
}

func TestHelpPagesClickable(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	src := &dummySource{}

	for page := 1; page <= 4; page++ {
		src.messages = nil
		err := Dispatch(mgr, src, []string{"help", strconv.Itoa(page)})
		if err != nil {
			t.Fatalf("help page %d failed: %v", page, err)
		}
		if len(src.messages) == 0 {
			t.Fatalf("help page %d returned no messages", page)
		}
	}
}

func TestGranularPermissionEnforcement(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// 1. User with no permissions is blocked from /sp user
	userNoPerm := &limitedSource{perms: map[string]bool{}}
	_ = Dispatch(mgr, userNoPerm, []string{"user", "Steve", "info"})
	if len(userNoPerm.messages) == 0 || !strings.Contains(userNoPerm.messages[0], "do not have permission") {
		t.Fatalf("expected permission denial message, got: %v", userNoPerm.messages)
	}

	// 2. User with stonyperms.info can run /sp info but not /sp user
	userInfoOnly := &limitedSource{perms: map[string]bool{"stonyperms.info": true}}
	_ = Dispatch(mgr, userInfoOnly, []string{"info"})
	if len(userInfoOnly.messages) == 0 || strings.Contains(userInfoOnly.messages[0], "do not have permission") {
		t.Fatalf("expected /sp info to succeed, got: %v", userInfoOnly.messages)
	}

	userInfoOnly.messages = nil
	_ = Dispatch(mgr, userInfoOnly, []string{"user", "Steve", "info"})
	if len(userInfoOnly.messages) == 0 || !strings.Contains(userInfoOnly.messages[0], "do not have permission") {
		t.Fatalf("expected /sp user to be denied for info-only user")
	}

	// 3. User with stonyperms.admin can run everything
	userAdmin := &limitedSource{perms: map[string]bool{"stonyperms.admin": true}}
	_ = Dispatch(mgr, userAdmin, []string{"user", "Steve", "info"})
	if len(userAdmin.messages) > 0 && strings.Contains(userAdmin.messages[0], "do not have permission") {
		t.Fatalf("admin user should not be denied")
	}
}


