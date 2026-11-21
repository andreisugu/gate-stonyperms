package manager

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func newWebTestManager(t *testing.T) *Manager {
	t.Helper()
	store, err := storage.NewJSONStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONStorage: %v", err)
	}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

func TestEditorSessionLifecycleAndSave(t *testing.T) {
	mgr := newWebTestManager(t)
	mgr.SetWebConfig(WebConfig{
		BaseURL:      "http://127.0.0.1:8737",
		DefaultGroup: "default",
		LiveSave:     true,
	})

	vip, _, err := mgr.CreateGroupWithActor("vip", 30, "&3[VIP]", "Console")
	if err != nil {
		t.Fatalf("CreateGroupWithActor: %v", err)
	}
	vip.AddNode(model.NewNode("essentials.fly", true))
	if err := mgr.SaveGroup(vip); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}

	u := mgr.GetOrCreateUser(uuid.New(), "Larryy_")
	u.AddParent(model.NewInheritance("vip"))
	if err := mgr.SaveUser(u); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}

	created, err := mgr.CreateEditorSession(WebSessionCreateOptions{
		Actor:            "Admin",
		TargetType:       "group",
		Target:           "vip",
		ExtraPermissions: []string{"custom.plugin.use"},
	})
	if err != nil {
		t.Fatalf("CreateEditorSession: %v", err)
	}
	if !strings.Contains(created.URL, "/editor?session="+created.Token) {
		t.Fatalf("unexpected session URL: %s", created.URL)
	}

	loaded, ok := mgr.GetEditorSession(created.Token)
	if !ok {
		t.Fatal("expected created editor session to exist")
	}
	if len(loaded.Groups) < 2 || len(loaded.Users) < 1 {
		t.Fatalf("expected groups and users in snapshot, got %d groups %d users", len(loaded.Groups), len(loaded.Users))
	}

	newMod := *model.NewGroup("mod", 80)
	newMod.DisplayName = "&9[Mod]"
	newMod.AddNode(model.NewNode("gate.command.send", true))
	newMod.AddParent(model.NewInheritance("vip"))

	staffTrack := model.Track{Name: "staff", Groups: []string{"vip", "mod"}}

	summary, applyCode, err := mgr.SaveEditorSession(created.Token, EditorSavePayload{
		Groups: append(loaded.Groups, newMod),
		Tracks: []model.Track{staffTrack},
		Users:  loaded.Users,
	})
	if err != nil {
		t.Fatalf("SaveEditorSession: %v", err)
	}
	if summary.GroupsUpdated != 1 || summary.TracksUpdated != 1 {
		t.Fatalf("unexpected save summary: %+v", summary)
	}
	if !strings.HasPrefix(applyCode, ShortApplyPrefix) {
		t.Fatalf("expected short apply code, got %q", applyCode)
	}
	if got := mgr.GetGroup("mod"); got == nil || !got.Nodes[0].Value {
		t.Fatalf("expected mod group persisted, got %+v", got)
	}
	if tr := mgr.GetTrack("staff"); tr == nil || len(tr.Groups) != 2 {
		t.Fatalf("expected staff track persisted, got %+v", tr)
	}

	if _, _, err := mgr.SaveEditorSession(created.Token, EditorSavePayload{
		DeletedGroups: []string{"mod", "default"},
	}); !errors.Is(err, ErrCannotDeleteDefault) {
		t.Fatalf("expected ErrCannotDeleteDefault, got %v", err)
	}
	if mgr.GetGroup("mod") == nil {
		t.Fatal("mod should remain intact when validation fails")
	}
}

func TestPortableApplyCodeAndZipBombProtection(t *testing.T) {
	mgr := newWebTestManager(t)
	g := model.NewGroup("builder", 45)
	g.AddNode(model.NewNode("worldedit.wand", true))

	code, err := EncodePortableApplyCode(EditorSavePayload{
		Actor:  "WebUser",
		Groups: []model.Group{*g},
	})
	if err != nil {
		t.Fatalf("EncodePortableApplyCode: %v", err)
	}
	if !strings.HasPrefix(code, PortableCodePrefix) {
		t.Fatalf("expected %s prefix, got %q", PortableCodePrefix, code)
	}

	summary, err := mgr.ApplyEditCode("Console", code)
	if err != nil {
		t.Fatalf("ApplyEditCode: %v", err)
	}
	if summary.GroupsUpdated != 1 || mgr.GetGroup("builder") == nil {
		t.Fatalf("expected builder group applied, summary=%+v", summary)
	}

	shortCode, err := mgr.CreateShortApplyCode(EditorSavePayload{
		DeletedGroups: []string{"builder"},
	})
	if err != nil {
		t.Fatalf("CreateShortApplyCode: %v", err)
	}
	sumDel, err := mgr.ApplyEditCode("Console", shortCode)
	if err != nil || sumDel.GroupsDeleted != 1 {
		t.Fatalf("expected short code deletion, got %+v err=%v", sumDel, err)
	}
	if _, err := mgr.ApplyEditCode("Console", shortCode); !errors.Is(err, ErrInvalidApplyCode) {
		t.Fatalf("expected ErrInvalidApplyCode on reuse, got %v", err)
	}

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	over := bytes.Repeat([]byte("A"), maxPortableCodeBytes+512)
	if _, err := gz.Write(over); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	bombCode := PortableCodePrefix + base64.RawURLEncoding.EncodeToString(raw.Bytes())
	if _, err := DecodePortableApplyCode(bombCode); !errors.Is(err, ErrInvalidApplyCode) {
		t.Fatalf("expected ErrInvalidApplyCode for oversized payload, got %v", err)
	}
}

func TestOfflinePortableSessionURLAndTreeVerbose(t *testing.T) {
	mgr := newWebTestManager(t)
	mgr.SetWebConfig(WebConfig{
		BaseURL:  DefaultPublicWebURL,
		LiveSave: false,
	})
	created, err := mgr.CreateEditorSession(WebSessionCreateOptions{Actor: "Console"})
	if err != nil {
		t.Fatalf("CreateEditorSession offline: %v", err)
	}
	if !strings.Contains(created.URL, "/editor#data=sp1_") {
		t.Fatalf("expected portable hash URL when LiveSave=false, got %s", created.URL)
	}

	mgr.SetWebConfig(WebConfig{
		BaseURL:  "http://127.0.0.1:8737",
		LiveSave: true,
	})
	rec := engine.VerboseRecording{
		Actor:      "Console",
		TotalCount: 1,
		Entries: []engine.VerboseEntry{{
			Subject: "Tester", Permission: "gate.command.ping", Result: "true", Source: "group:default",
		}},
	}
	vb, err := mgr.CreateVerboseSession(rec, "Console", "sp")
	if err != nil || vb.Count != 1 {
		t.Fatalf("CreateVerboseSession: %+v err=%v", vb, err)
	}
	if _, ok := mgr.GetVerboseSession(vb.Token); !ok {
		t.Fatal("expected verbose session to be stored")
	}

	tr, err := mgr.CreateTreeSession("gate", "Console", "sp", []string{"gate.custom.node"})
	if err != nil || tr.Count == 0 {
		t.Fatalf("CreateTreeSession: %+v err=%v", tr, err)
	}
	treeData, ok := mgr.GetTreeSession(tr.Token)
	if !ok || treeData.Metadata.Root != "gate" {
		t.Fatalf("unexpected tree session: ok=%v data=%+v", ok, treeData)
	}
}

func TestEditorPreservesContextualAndTempMetaEntries(t *testing.T) {
	mgr := newWebTestManager(t)
	g := model.NewGroup("vip", 20)
	_ = mgr.SaveGroup(g)

	now := time.Now().UTC()
	payload := EditorSavePayload{
		Groups: []model.Group{{
			Name:   "vip",
			Weight: 20,
			Meta:   map[string]string{"color": "aqua"},
			MetaEntries: []model.MetaEntry{{
				Key:      "homes",
				Value:    "5",
				Expiry:   now.Add(2 * time.Hour).Unix(),
				Contexts: map[string]string{"server": "survival"},
			}},
		}},
	}
	if _, err := mgr.ApplyEditorPayload("Console", payload); err != nil {
		t.Fatalf("ApplyEditorPayload: %v", err)
	}
	saved := mgr.GetGroup("vip")
	if saved == nil || len(saved.MetaEntries) != 2 {
		t.Fatalf("expected 2 synchronized MetaEntries on group, got %+v", saved)
	}
}

func TestTrackEditorURLAndExtendedPotentialContexts(t *testing.T) {
	mgr := newWebTestManager(t)
	_, _ = mgr.CreateGroup("vip", 10, "")
	tr, _ := mgr.CreateTrack("staff")
	_ = tr.Append("vip")
	_ = mgr.SaveTrack(tr)

	u := mgr.GetOrCreateUser(uuid.New(), "CtxTester")
	uc := mgr.GetUserCopy(u.UUID)
	_, _ = uc.SetMetaEntry(model.MetaEntry{
		Key:      "tag",
		Value:    "star",
		Contexts: map[string]string{"server": "skyblock"},
	}, model.TempModifierReplace, time.Now())
	uc.AddPrefix(model.PriorityMeta{
		Priority: 10,
		Value:    "[SB] ",
		Contexts: map[string]string{"world": "sky_void"},
	})
	_ = mgr.SaveUser(uc)

	// Offline portable URL with track target
	mgr.SetWebConfig(WebConfig{BaseURL: DefaultPublicWebURL, LiveSave: false})
	offline, err := mgr.CreateEditorSession(WebSessionCreateOptions{
		Actor:      "Console",
		TargetType: "track",
		Target:     "staff",
	})
	if err != nil {
		t.Fatalf("offline track editor session: %v", err)
	}
	if !strings.Contains(offline.URL, "/editor?target=track%3Astaff#data=sp1_") {
		t.Fatalf("unexpected offline track URL: %s", offline.URL)
	}

	// Live save URL with track target + context discovery from MetaEntries/Prefixes
	mgr.SetWebConfig(WebConfig{BaseURL: "http://127.0.0.1:8737", LiveSave: true})
	live, err := mgr.CreateEditorSession(WebSessionCreateOptions{
		Actor:      "Console",
		TargetType: "track",
		Target:     "staff",
	})
	if err != nil {
		t.Fatalf("live track editor session: %v", err)
	}
	if !strings.Contains(live.URL, "&target=track%3Astaff") {
		t.Fatalf("unexpected live track URL: %s", live.URL)
	}
	data, ok := mgr.GetEditorSession(live.Token)
	if !ok {
		t.Fatal("expected live session stored")
	}
	if !containsString(data.PotentialContexts["server"], "skyblock") {
		t.Fatalf("expected skyblock from MetaEntries in PotentialContexts: %v", data.PotentialContexts["server"])
	}
	if !containsString(data.PotentialContexts["world"], "sky_void") {
		t.Fatalf("expected sky_void from Prefixes in PotentialContexts: %v", data.PotentialContexts["world"])
	}

	// Strict dotted tree root filtering
	tree, err := mgr.CreateTreeSession("gate", "Console", "sp", []string{"gate.custom.cmd", "gateway.unrelated"})
	if err != nil {
		t.Fatalf("CreateTreeSession: %v", err)
	}
	td, _ := mgr.GetTreeSession(tree.Token)
	if containsString(td.Permissions, "gateway.unrelated") {
		t.Fatalf("did not expect gateway.unrelated under gate root: %v", td.Permissions)
	}
}

func containsString(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}
