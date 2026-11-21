package manager

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

type evalSpecFile struct {
	Groups []*model.Group `json:"groups"`
	Cases  []evalSpecCase `json:"cases"`
}

type evalSpecCase struct {
	Name                    string            `json:"name"`
	User                    *model.User       `json:"user"`
	Contexts                map[string]string `json:"contexts"`
	Now                     int64             `json:"now"`
	PrimaryGroupCalculation string            `json:"primaryGroupCalculation"`
	Stack                   *struct {
		PrefixEnabled     bool   `json:"prefixEnabled"`
		PrefixStartSpacer string `json:"prefixStartSpacer"`
		PrefixMidSpacer   string `json:"prefixMidSpacer"`
		PrefixEndSpacer   string `json:"prefixEndSpacer"`
		SuffixEnabled     bool   `json:"suffixEnabled"`
		SuffixStartSpacer string `json:"suffixStartSpacer"`
		SuffixMidSpacer   string `json:"suffixMidSpacer"`
		SuffixEndSpacer   string `json:"suffixEndSpacer"`
		Deduplicate       bool   `json:"deduplicate"`
	} `json:"stack"`
	Checks []struct {
		Permission string `json:"permission"`
		Expected   string `json:"expected"`
		Source     string `json:"source"`
	} `json:"checks"`
	GroupChecks []struct {
		Group      string `json:"group"`
		Permission string `json:"permission"`
		Expected   string `json:"expected"`
		Source     string `json:"source"`
	} `json:"groupChecks"`
	ExpectedPrimaryGroup string            `json:"expectedPrimaryGroup"`
	ExpectedPrefix       *string           `json:"expectedPrefix"`
	ExpectedSuffix       *string           `json:"expectedSuffix"`
	ExpectedMeta         map[string]string `json:"expectedMeta"`
	MetaQueries          []struct {
		Key   string `json:"key"`
		Found bool   `json:"found"`
		Value string `json:"value"`
	} `json:"metaQueries"`
}

func TestSpecConformanceEval(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "spec", "conformance", "eval.json"))
	if err != nil {
		t.Fatalf("read eval.json: %v", err)
	}
	var spec evalSpecFile
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse eval.json: %v", err)
	}

	groups := make(map[string]*model.Group, len(spec.Groups))
	for _, g := range spec.Groups {
		groups[strings.ToLower(g.Name)] = g
	}

	for _, tc := range spec.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			eng := engine.New()
			if tc.PrimaryGroupCalculation != "" {
				eng.SetPrimaryGroupCalculation(tc.PrimaryGroupCalculation)
			}
			if tc.Stack != nil {
				eng.SetStackConfig(engine.StackConfig{
					PrefixEnabled:     tc.Stack.PrefixEnabled,
					PrefixStartSpacer: tc.Stack.PrefixStartSpacer,
					PrefixMidSpacer:   tc.Stack.PrefixMidSpacer,
					PrefixEndSpacer:   tc.Stack.PrefixEndSpacer,
					SuffixEnabled:     tc.Stack.SuffixEnabled,
					SuffixStartSpacer: tc.Stack.SuffixStartSpacer,
					SuffixMidSpacer:   tc.Stack.SuffixMidSpacer,
					SuffixEndSpacer:   tc.Stack.SuffixEndSpacer,
					Deduplicate:       tc.Stack.Deduplicate,
				})
			}
			now := time.Unix(tc.Now, 0)

			for _, chk := range tc.Checks {
				got := eng.EvaluateAt(tc.User, groups, chk.Permission, tc.Contexts, now)
				if got.Result.String() != chk.Expected || got.Source != chk.Source {
					t.Fatalf("perm %q: got (%s, %q), want (%s, %q)",
						chk.Permission, got.Result.String(), got.Source, chk.Expected, chk.Source)
				}
			}
			for _, gchk := range tc.GroupChecks {
				grp := groups[strings.ToLower(gchk.Group)]
				got := eng.GroupPermissionValueAt(grp, groups, gchk.Permission, tc.Contexts, now)
				if got.Result.String() != gchk.Expected || got.Source != gchk.Source {
					t.Fatalf("group %s perm %q: got (%s, %q), want (%s, %q)",
						gchk.Group, gchk.Permission, got.Result.String(), got.Source, gchk.Expected, gchk.Source)
				}
			}
			if tc.ExpectedPrimaryGroup != "" {
				got := eng.ResolvePrimaryGroupAt(tc.User, groups, tc.Contexts, "default", now)
				if got != tc.ExpectedPrimaryGroup {
					t.Fatalf("primaryGroup: got %q, want %q", got, tc.ExpectedPrimaryGroup)
				}
			}
			if tc.ExpectedPrefix != nil {
				got := eng.GetPrefixAt(tc.User, groups, tc.Contexts, now)
				if got != *tc.ExpectedPrefix {
					t.Fatalf("prefix: got %q, want %q", got, *tc.ExpectedPrefix)
				}
			}
			if tc.ExpectedSuffix != nil {
				got := eng.GetSuffixAt(tc.User, groups, tc.Contexts, now)
				if got != *tc.ExpectedSuffix {
					t.Fatalf("suffix: got %q, want %q", got, *tc.ExpectedSuffix)
				}
			}
			if tc.ExpectedMeta != nil {
				gotMeta := eng.ResolveAllMetaAt(tc.User, groups, tc.Contexts, now)
				if len(gotMeta) != len(tc.ExpectedMeta) {
					t.Fatalf("meta len: got %v, want %v", gotMeta, tc.ExpectedMeta)
				}
				for k, wantVal := range tc.ExpectedMeta {
					if gotMeta[k] != wantVal {
						t.Fatalf("meta[%q]: got %q, want %q", k, gotMeta[k], wantVal)
					}
				}
			}
			for _, mq := range tc.MetaQueries {
				gotVal, gotFound := eng.GetMetaWithContextAt(tc.User, groups, mq.Key, tc.Contexts, now)
				if gotFound != mq.Found || gotVal != mq.Value {
					t.Fatalf("metaQuery(%q): got (%v, %q), want (%v, %q)",
						mq.Key, gotFound, gotVal, mq.Found, mq.Value)
				}
			}
		})
	}
}

func TestSpecConformanceMutations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "spec", "conformance", "mutations.json"))
	if err != nil {
		t.Fatalf("read mutations.json: %v", err)
	}
	var doc struct {
		BaseTime      int64 `json:"baseTime"`
		TempModifiers []struct {
			Kind           string `json:"kind"`
			InitialExpiry  int64  `json:"initialExpiry"`
			NewExpiry      int64  `json:"newExpiry"`
			Modifier       string `json:"modifier"`
			ExpectedExpiry int64  `json:"expectedExpiry"`
			ExpectError    bool   `json:"expectError"`
		} `json:"tempModifiers"`
		TrackAndRenameFlow struct {
			Groups    []string `json:"groups"`
			TrackName string   `json:"trackName"`
			TrackGrps []string `json:"trackGroups"`
			User      struct {
				UUID         string `json:"uuid"`
				Username     string `json:"username"`
				PrimaryGroup string `json:"primaryGroup"`
			} `json:"user"`
			Steps []struct {
				Op                string   `json:"op"`
				Track             string   `json:"track"`
				Old               string   `json:"old"`
				New               string   `json:"new"`
				ExpectFrom        string   `json:"expectFrom"`
				ExpectTo          string   `json:"expectTo"`
				ExpectPrimary     string   `json:"expectPrimary"`
				ExpectTrackGroups []string `json:"expectTrackGroups"`
				ExpectError       bool     `json:"expectError"`
			} `json:"steps"`
		} `json:"trackAndRenameFlow"`
		BulkAndSearch struct {
			SearchQuery string `json:"searchQuery"`
			ExpectedHit int    `json:"expectedGroupHitCount"`
			Preview     struct {
				Scope               string           `json:"scope"`
				Action              string           `json:"action"`
				Field               string           `json:"field"`
				Value               string           `json:"value"`
				Constraints         []BulkConstraint `json:"constraints"`
				DryRun              bool             `json:"dryRun"`
				ExpectGroupsChanged int              `json:"expectGroupsChanged"`
				ExpectNodesAffected int              `json:"expectNodesAffected"`
			} `json:"preview"`
			Apply struct {
				Scope               string           `json:"scope"`
				Action              string           `json:"action"`
				Field               string           `json:"field"`
				Value               string           `json:"value"`
				Constraints         []BulkConstraint `json:"constraints"`
				DryRun              bool             `json:"dryRun"`
				ExpectGroupsChanged int              `json:"expectGroupsChanged"`
				ExpectNodesAffected int              `json:"expectNodesAffected"`
			} `json:"apply"`
			Delete struct {
				Scope               string           `json:"scope"`
				Action              string           `json:"action"`
				Constraints         []BulkConstraint `json:"constraints"`
				DryRun              bool             `json:"dryRun"`
				ExpectGroupsChanged int              `json:"expectGroupsChanged"`
				ExpectNodesAffected int              `json:"expectNodesAffected"`
			} `json:"delete"`
		} `json:"bulkAndSearch"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse mutations.json: %v", err)
	}

	now := time.Unix(doc.BaseTime, 0)
	for i, tm := range doc.TempModifiers {
		mod, ok := model.ParseTempModifier(tm.Modifier)
		if !ok {
			t.Fatalf("tempModifier %d: invalid modifier %q", i, tm.Modifier)
		}
		u := model.NewUser(uuid.New(), "ModUser")
		switch tm.Kind {
		case "node":
			_, _ = u.AddNodeWithModifier(model.Node{Key: "test.perm", Value: true, Expiry: tm.InitialExpiry}, model.TempModifierReplace, now)
			updated, err := u.AddNodeWithModifier(model.Node{Key: "test.perm", Value: true, Expiry: tm.NewExpiry}, mod, now)
			if tm.ExpectError {
				if err == nil {
					t.Fatalf("tempModifier %d (node): expected error, got nil", i)
				}
			} else if err != nil || updated.Expiry != tm.ExpectedExpiry {
				t.Fatalf("tempModifier %d (node): got (%d, %v), want (%d, nil)", i, updated.Expiry, err, tm.ExpectedExpiry)
			}
		case "parent":
			_, _ = u.AddParentWithModifier(model.Inheritance{Group: "vip", Expiry: tm.InitialExpiry}, model.TempModifierReplace, now)
			updated, err := u.AddParentWithModifier(model.Inheritance{Group: "vip", Expiry: tm.NewExpiry}, mod, now)
			if tm.ExpectError {
				if err == nil {
					t.Fatalf("tempModifier %d (parent): expected error", i)
				}
			} else if err != nil || updated.Expiry != tm.ExpectedExpiry {
				t.Fatalf("tempModifier %d (parent): got (%d, %v), want (%d, nil)", i, updated.Expiry, err, tm.ExpectedExpiry)
			}
		case "affix":
			_, _ = u.AddPrefixWithModifier(model.PriorityMeta{Priority: 10, Value: "A", Expiry: tm.InitialExpiry}, model.TempModifierReplace, now)
			updated, err := u.AddPrefixWithModifier(model.PriorityMeta{Priority: 10, Value: "A", Expiry: tm.NewExpiry}, mod, now)
			if tm.ExpectError {
				if err == nil {
					t.Fatalf("tempModifier %d (affix): expected error", i)
				}
			} else if err != nil || updated.Expiry != tm.ExpectedExpiry {
				t.Fatalf("tempModifier %d (affix): got (%d, %v), want (%d, nil)", i, updated.Expiry, err, tm.ExpectedExpiry)
			}
		case "meta":
			_, _ = u.SetMetaEntry(model.MetaEntry{Key: "tag", Value: "v1", Expiry: tm.InitialExpiry}, model.TempModifierReplace, now)
			updated, err := u.SetMetaEntry(model.MetaEntry{Key: "tag", Value: "v2", Expiry: tm.NewExpiry}, mod, now)
			if tm.ExpectError {
				if err == nil {
					t.Fatalf("tempModifier %d (meta): expected error", i)
				}
			} else if err != nil || updated.Expiry != tm.ExpectedExpiry {
				t.Fatalf("tempModifier %d (meta): got (%d, %v), want (%d, nil)", i, updated.Expiry, err, tm.ExpectedExpiry)
			}
		default:
			t.Fatalf("unknown tempModifier kind %q", tm.Kind)
		}
	}

	// Track and rename flow
	mgr := newWebTestManager(t)
	for _, gName := range doc.TrackAndRenameFlow.Groups {
		if _, ok, _ := mgr.GroupSnapshot(gName); !ok {
			if _, _, err := mgr.CreateGroupWithActor(gName, 10, gName, "test"); err != nil {
				t.Fatalf("CreateGroup %q: %v", gName, err)
			}
		}
	}
	if _, _, err := mgr.CreateTrackWithActor(doc.TrackAndRenameFlow.TrackName, doc.TrackAndRenameFlow.TrackGrps, "test"); err != nil {
		t.Fatalf("CreateTrack: %v", err)
	}
	uid := uuid.MustParse(doc.TrackAndRenameFlow.User.UUID)
	if _, _, _, err := mgr.ResolveOrCreateUser(uid, doc.TrackAndRenameFlow.User.Username, doc.TrackAndRenameFlow.User.PrimaryGroup, "test"); err != nil {
		t.Fatalf("ResolveOrCreateUser: %v", err)
	}

	activeTrack := doc.TrackAndRenameFlow.TrackName
	for i, step := range doc.TrackAndRenameFlow.Steps {
		switch step.Op {
		case "promote":
			u, from, to, _, err := mgr.PromoteUserByID(uid, step.Track, "test")
			if step.ExpectError {
				if err == nil {
					t.Fatalf("step %d promote: expected error", i)
				}
				continue
			}
			if err != nil || from != step.ExpectFrom || to != step.ExpectTo || u.PrimaryGroup != step.ExpectPrimary {
				t.Fatalf("step %d promote: got (%q->%q, primary=%q, err=%v)", i, from, to, u.PrimaryGroup, err)
			}
		case "demote":
			u, from, to, _, err := mgr.DemoteUserByID(uid, step.Track, "test")
			if step.ExpectError {
				if err == nil {
					t.Fatalf("step %d demote: expected error", i)
				}
				continue
			}
			if err != nil || from != step.ExpectFrom || to != step.ExpectTo || u.PrimaryGroup != step.ExpectPrimary {
				t.Fatalf("step %d demote: got (%q->%q, primary=%q, err=%v)", i, from, to, u.PrimaryGroup, err)
			}
		case "renameGroup":
			if _, _, err := mgr.RenameGroup(step.Old, step.New, "test"); err != nil {
				t.Fatalf("step %d renameGroup: %v", i, err)
			}
			u, _, _ := mgr.UserSnapshot(uid)
			if u.PrimaryGroup != step.ExpectPrimary {
				t.Fatalf("step %d primaryGroup after renameGroup: got %q, want %q", i, u.PrimaryGroup, step.ExpectPrimary)
			}
			tr, _, _ := mgr.TrackSnapshot(activeTrack)
			if !slices.Equal(tr.Groups, step.ExpectTrackGroups) {
				t.Fatalf("step %d track groups: got %v, want %v", i, tr.Groups, step.ExpectTrackGroups)
			}
		case "renameTrack":
			if _, _, err := mgr.RenameTrack(step.Old, step.New, "test"); err != nil {
				t.Fatalf("step %d renameTrack: %v", i, err)
			}
			activeTrack = step.New
		}
	}

	// Bulk and search on fresh default storage
	bulkMgr := newWebTestManager(t)
	searchRes := bulkMgr.Search(doc.BulkAndSearch.SearchQuery)
	if len(searchRes.GroupHits) != doc.BulkAndSearch.ExpectedHit {
		t.Fatalf("search %q hits: got %d, want %d", doc.BulkAndSearch.SearchQuery, len(searchRes.GroupHits), doc.BulkAndSearch.ExpectedHit)
	}
	prev, err := bulkMgr.PreviewBulkUpdate(BulkUpdateOptions{
		Scope:       doc.BulkAndSearch.Preview.Scope,
		Action:      doc.BulkAndSearch.Preview.Action,
		Field:       doc.BulkAndSearch.Preview.Field,
		Value:       doc.BulkAndSearch.Preview.Value,
		Constraints: doc.BulkAndSearch.Preview.Constraints,
	})
	if err != nil || prev.GroupsChanged != doc.BulkAndSearch.Preview.ExpectGroupsChanged || prev.NodesAffected != doc.BulkAndSearch.Preview.ExpectNodesAffected {
		t.Fatalf("bulk preview: got %+v (err=%v)", prev, err)
	}
	applied, err := bulkMgr.BulkUpdate(BulkUpdateOptions{
		Scope:       doc.BulkAndSearch.Apply.Scope,
		Action:      doc.BulkAndSearch.Apply.Action,
		Field:       doc.BulkAndSearch.Apply.Field,
		Value:       doc.BulkAndSearch.Apply.Value,
		Constraints: doc.BulkAndSearch.Apply.Constraints,
	}, "test")
	if err != nil || applied.GroupsChanged != doc.BulkAndSearch.Apply.ExpectGroupsChanged || applied.NodesAffected != doc.BulkAndSearch.Apply.ExpectNodesAffected {
		t.Fatalf("bulk apply: got %+v (err=%v)", applied, err)
	}
	deleted, err := bulkMgr.BulkUpdate(BulkUpdateOptions{
		Scope:       doc.BulkAndSearch.Delete.Scope,
		Action:      doc.BulkAndSearch.Delete.Action,
		Constraints: doc.BulkAndSearch.Delete.Constraints,
	}, "test")
	if err != nil || deleted.GroupsChanged != doc.BulkAndSearch.Delete.ExpectGroupsChanged || deleted.NodesAffected != doc.BulkAndSearch.Delete.ExpectNodesAffected {
		t.Fatalf("bulk delete: got %+v (err=%v)", deleted, err)
	}
}

func TestSpecConformanceInteropStorageAndPortableCode(t *testing.T) {
	srcDir := filepath.Join("..", "spec", "conformance", "interop_storage")
	dstDir := t.TempDir()
	copyTree(t, srcDir, dstDir)

	store, err := storage.NewJSONStorage(dstDir)
	if err != nil {
		t.Fatalf("NewJSONStorage(interop copy): %v", err)
	}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("manager.New(interop copy): %v", err)
	}
	defer mgr.Close()

	rawExp, err := os.ReadFile(filepath.Join("..", "spec", "conformance", "expected.json"))
	if err != nil {
		t.Fatalf("read expected.json: %v", err)
	}
	var exp struct {
		UserUUID  string `json:"userUuid"`
		Scenarios []struct {
			Name         string            `json:"name"`
			Contexts     map[string]string `json:"contexts"`
			PrimaryGroup string            `json:"primaryGroup"`
			Groups       []string          `json:"groups"`
			Prefix       string            `json:"prefix"`
			Suffix       string            `json:"suffix"`
			Meta         map[string]string `json:"meta"`
			Checks       []struct {
				Permission string `json:"permission"`
				Result     string `json:"result"`
				Source     string `json:"source"`
			} `json:"checks"`
		} `json:"scenarios"`
		PortableApply struct {
			ExpectedGroup       string   `json:"expectedGroup"`
			ExpectedGroupWeight int      `json:"expectedGroupWeight"`
			ExpectedTrack       string   `json:"expectedTrack"`
			ExpectedTrackGroups []string `json:"expectedTrackGroups"`
			ExpectedGroupsCount int      `json:"expectedGroupsCount"`
		} `json:"portableApply"`
	}
	if err := json.Unmarshal(rawExp, &exp); err != nil {
		t.Fatalf("parse expected.json: %v", err)
	}

	uid := uuid.MustParse(exp.UserUUID)
	for _, sc := range exp.Scenarios {
		eff, found, _ := mgr.EffectiveUser(uid, sc.Contexts, "default")
		if !found || eff == nil {
			t.Fatalf("scenario %s: user not found", sc.Name)
		}
		if eff.PrimaryGroup != sc.PrimaryGroup || eff.Prefix != sc.Prefix || eff.Suffix != sc.Suffix {
			t.Fatalf("scenario %s: mismatch primary/affixes got (%q,%q,%q)", sc.Name, eff.PrimaryGroup, eff.Prefix, eff.Suffix)
		}
		gotGroups := make([]string, 0, len(eff.Groups))
		for _, g := range eff.Groups {
			gotGroups = append(gotGroups, g.Name)
		}
		if !slices.Equal(gotGroups, sc.Groups) {
			t.Fatalf("scenario %s groups: got %v, want %v", sc.Name, gotGroups, sc.Groups)
		}
		if len(eff.Meta) != len(sc.Meta) {
			t.Fatalf("scenario %s meta: got %v, want %v", sc.Name, eff.Meta, sc.Meta)
		}
		for k, v := range sc.Meta {
			if eff.Meta[k] != v {
				t.Fatalf("scenario %s meta[%q]: got %q, want %q", sc.Name, k, eff.Meta[k], v)
			}
		}
		for _, chk := range sc.Checks {
			res, src, _ := mgr.EvaluateUser(uid, chk.Permission, sc.Contexts, "default")
			if res.String() != chk.Result || src != chk.Source {
				t.Fatalf("scenario %s perm %q: got (%s,%q), want (%s,%q)",
					sc.Name, chk.Permission, res.String(), src, chk.Result, chk.Source)
			}
		}
	}

	rawCode, err := os.ReadFile(filepath.Join("..", "spec", "conformance", "portable_code.txt"))
	if err != nil {
		t.Fatalf("read portable_code.txt: %v", err)
	}
	if _, err := mgr.ApplyEditCode("conformance", strings.TrimSpace(string(rawCode))); err != nil {
		t.Fatalf("ApplyEditCode: %v", err)
	}
	grp, ok, _ := mgr.GroupSnapshot(exp.PortableApply.ExpectedGroup)
	if !ok || grp.Weight != exp.PortableApply.ExpectedGroupWeight {
		t.Fatalf("portable group check failed: ok=%v grp=%+v", ok, grp)
	}
	tr, ok, _ := mgr.TrackSnapshot(exp.PortableApply.ExpectedTrack)
	if !ok || !slices.Equal(tr.Groups, exp.PortableApply.ExpectedTrackGroups) {
		t.Fatalf("portable track check failed: ok=%v tr=%+v", ok, tr)
	}
	allGroups, _ := mgr.GroupsSnapshot()
	if len(allGroups) != exp.PortableApply.ExpectedGroupsCount {
		t.Fatalf("portable total groups: got %d, want %d", len(allGroups), exp.PortableApply.ExpectedGroupsCount)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		t.Fatalf("copyTree(%s -> %s): %v", src, dst, err)
	}
}
