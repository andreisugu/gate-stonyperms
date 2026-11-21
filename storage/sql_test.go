package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

func TestSQLiteStorageFullRoundTripAndPrefixValidation(t *testing.T) {
	if err := ValidateTablePrefix("bad-prefix!"); err == nil {
		t.Fatal("expected invalid table prefix error")
	}
	if _, err := NormalizeStorageType("invalid_engine"); err == nil {
		t.Fatal("expected invalid storage type error")
	}

	dbFile := filepath.Join(t.TempDir(), "nested", "sp.db")
	rawStore, err := Open("sqlite", "", SQLConfig{
		File:        dbFile,
		TablePrefix: "sp_test_",
	})
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	store := rawStore.(*SQLStorage)
	defer func() { _ = store.Close() }()

	if !strings.HasPrefix(store.DescribeTarget(), "sqlite:") {
		t.Fatalf("unexpected DescribeTarget: %s", store.DescribeTarget())
	}

	bundle, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if bundle.Groups["default"] == nil || bundle.Groups["admin"] == nil {
		t.Fatalf("expected default and admin groups, got %+v", bundle.Groups)
	}
	if bundle.Tracks["ranks"] == nil || len(bundle.Tracks["ranks"].Groups) != 2 {
		t.Fatalf("expected default ranks track, got %+v", bundle.Tracks)
	}
	if len(bundle.Logs) != 1 {
		t.Fatalf("expected initial log entry, got %d", len(bundle.Logs))
	}

	vip := model.NewGroup("vip", 50)
	vip.DisplayName = "&aVIP"
	vip.Parents = []model.Inheritance{{Group: "default", Expiry: 2000000000, Contexts: map[string]string{"server": "smp"}}}
	vip.Nodes = []model.Node{
		{Key: "essentials.*", Value: true},
		{Key: "essentials.god", Value: false, Expiry: 2000000100, Contexts: map[string]string{"server": "smp", "world": "spawn"}},
	}
	vip.Prefixes = []model.PriorityMeta{{Priority: 50, Value: "&a[VIP] ", Contexts: map[string]string{"server": "smp"}}}
	vip.Suffixes = []model.PriorityMeta{{Priority: 50, Value: " &a+"}}
	vip.Meta = map[string]string{"homes": "5", "Shared": "CaseKept"}
	vip.MetaEntries = []model.MetaEntry{{Key: "color", Value: "gold", Priority: 10, Expiry: 2000000000, Contexts: map[string]string{"server": "creative"}}}
	if err := store.SaveGroup(vip); err != nil {
		t.Fatalf("SaveGroup(vip): %v", err)
	}

	uid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	u := model.NewUser(uid, "Alex")
	u.PrimaryGroup = "vip"
	u.Parents = []model.Inheritance{{Group: "vip"}}
	u.Nodes = []model.Node{
		{Key: "first.node", Value: true},
		{Key: "second.node", Value: false, Contexts: map[string]string{"world": "nether"}},
	}
	u.Meta = map[string]string{"tier": "gold"}
	u.MetaEntries = []model.MetaEntry{{Key: "banner", Value: "special", Priority: 5, Expiry: 2000000500, Contexts: map[string]string{"server": "lobby"}}}
	u.Prefixes = []model.PriorityMeta{{Priority: 200, Value: "&b[Star] "}}
	if err := store.SaveUser(u); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}

	for i := 0; i < 1005; i++ {
		if err := store.AppendLog(model.LogEntry{
			Timestamp:  1700000000 + int64(i),
			Actor:      "tester",
			Target:     "Alex",
			TargetType: "user",
			Action:     fmt.Sprintf("act-%d", i),
		}); err != nil {
			t.Fatalf("AppendLog %d: %v", i, err)
		}
	}
	_ = store.Close()

	reopened, err := NewSQLStorage(SQLConfig{Type: "sqlite", File: dbFile, TablePrefix: "sp_test_"})
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	reloaded, err := reopened.LoadAll()
	if err != nil {
		t.Fatalf("reloaded LoadAll: %v", err)
	}
	gv := reloaded.Groups["vip"]
	if gv == nil || len(gv.Nodes) != 2 || gv.Nodes[0].Key != "essentials.*" || gv.Nodes[1].Key != "essentials.god" {
		t.Fatalf("group nodes mismatch: %+v", gv)
	}
	if gv.Meta["Shared"] != "CaseKept" || len(gv.MetaEntries) != 1 || gv.MetaEntries[0].Contexts["server"] != "creative" {
		t.Fatalf("group meta mismatch: %+v %+v", gv.Meta, gv.MetaEntries)
	}
	ru := reloaded.Users[uid]
	if ru == nil || ru.PrimaryGroup != "vip" || len(ru.Nodes) != 2 || ru.Nodes[1].Key != "second.node" {
		t.Fatalf("reloaded user mismatch: %+v", ru)
	}
	if len(reloaded.Logs) != maxLogEntries {
		t.Fatalf("expected %d logs from LoadAll, got %d", maxLogEntries, len(reloaded.Logs))
	}
	if reloaded.Logs[0].Action != "act-5" || reloaded.Logs[len(reloaded.Logs)-1].Action != "act-1004" {
		t.Fatalf("unexpected log window ordering: first=%q last=%q", reloaded.Logs[0].Action, reloaded.Logs[len(reloaded.Logs)-1].Action)
	}

	if err := reopened.DeleteUser(uid); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := reopened.DeleteGroup("vip"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if err := reopened.DeleteTrack("ranks"); err != nil {
		t.Fatalf("DeleteTrack: %v", err)
	}
	afterDel, err := reopened.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll after delete: %v", err)
	}
	if afterDel.Users[uid] != nil || afterDel.Groups["vip"] != nil || afterDel.Tracks["ranks"] != nil {
		t.Fatalf("expected deleted entities absent")
	}
}

func TestDialectHelpersPasswordAndRebind(t *testing.T) {
	t.Setenv("STONYPERMS_DB_PASSWORD", "env-secret-pw")
	pw, err := ResolvePassword("inline", "", "STONYPERMS_DB_PASSWORD")
	if err != nil || pw != "env-secret-pw" {
		t.Fatalf("expected env password precedence, got (%q, %v)", pw, err)
	}

	t.Setenv("STONYPERMS_DB_PASSWORD", "")
	pwFile := filepath.Join(t.TempDir(), "db.pw")
	if err := os.WriteFile(pwFile, []byte("file-secret-pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pw, err = ResolvePassword("", pwFile, "STONYPERMS_DB_PASSWORD")
	if err != nil || pw != "file-secret-pw" {
		t.Fatalf("expected file password trimmed, got (%q, %v)", pw, err)
	}

	_, dsnPg, safePg, err := buildDriverAndDSN(DialectPostgreSQL, SQLConfig{
		Host:        "db.internal",
		Port:        5432,
		Database:    "sp",
		Username:    "spuser",
		TablePrefix: "sp_",
		SSLMode:     "require",
	}, "super-secret-pw")
	if err != nil || !strings.Contains(dsnPg, "super-secret-pw") || strings.Contains(safePg, "super-secret-pw") {
		t.Fatalf("unexpected postgres target redaction: dsn=%q safe=%q err=%v", dsnPg, safePg, err)
	}

	_, dsnMy, safeMy, err := buildDriverAndDSN(DialectMariaDB, SQLConfig{
		Host:        "127.0.0.1",
		Port:        3306,
		Database:    "sp",
		Username:    "spuser",
		TablePrefix: "sp_",
		SSLMode:     "disable",
	}, "super-secret-pw")
	if err != nil || !strings.Contains(dsnMy, "super-secret-pw") || strings.Contains(safeMy, "super-secret-pw") {
		t.Fatalf("unexpected mariadb target redaction: dsn=%q safe=%q err=%v", dsnMy, safeMy, err)
	}

	s := &SQLStorage{dialect: DialectPostgreSQL, prefix: "sp_"}
	got := s.rebind(`SELECT '?' FROM sp_users WHERE uuid = ? AND username = ?`)
	want := `SELECT '?' FROM sp_users WHERE uuid = $1 AND username = $2`
	if got != want {
		t.Fatalf("rebind got %q, want %q", got, want)
	}
	ddls := s.ddlStatements()
	if len(ddls) < 16 || !strings.Contains(strings.Join(ddls, "\n"), "BIGSERIAL PRIMARY KEY") {
		t.Fatalf("expected postgres BIGSERIAL DDL")
	}
}

func TestLiveExternalDatabaseIfConfigured(t *testing.T) {
	if host := os.Getenv("STONYPERMS_TEST_POSTGRES_HOST"); host != "" {
		port := 5432
		if p := os.Getenv("STONYPERMS_TEST_POSTGRES_PORT"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &port)
		}
		runLiveRoundTrip(t, SQLConfig{
			Type:        "postgresql",
			Host:        host,
			Port:        port,
			Database:    os.Getenv("STONYPERMS_TEST_POSTGRES_DB"),
			Username:    os.Getenv("STONYPERMS_TEST_POSTGRES_USER"),
			Password:    os.Getenv("STONYPERMS_TEST_POSTGRES_PASS"),
			TablePrefix: "sp_pgtest_",
			SSLMode:     "disable",
		})
	}
	if host := os.Getenv("STONYPERMS_TEST_MYSQL_HOST"); host != "" {
		port := 3306
		if p := os.Getenv("STONYPERMS_TEST_MYSQL_PORT"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &port)
		}
		runLiveRoundTrip(t, SQLConfig{
			Type:        "mariadb",
			Host:        host,
			Port:        port,
			Database:    os.Getenv("STONYPERMS_TEST_MYSQL_DB"),
			Username:    os.Getenv("STONYPERMS_TEST_MYSQL_USER"),
			Password:    os.Getenv("STONYPERMS_TEST_MYSQL_PASS"),
			TablePrefix: "sp_mytest_",
			SSLMode:     "disable",
		})
	}
}

func runLiveRoundTrip(t *testing.T, cfg SQLConfig) {
	t.Helper()
	store, err := NewSQLStorage(cfg)
	if err != nil {
		t.Fatalf("NewSQLStorage(%s): %v", cfg.Type, err)
	}
	defer func() { _ = store.Close() }()

	g := model.NewGroup("donor", 40)
	g.DisplayName = "&6Donor"
	g.Nodes = []model.Node{{Key: "live.node", Value: true, Contexts: map[string]string{"server": "lobby"}}}
	g.Meta = map[string]string{"Shared": "ExactCase", "homes": "4"}
	g.MetaEntries = []model.MetaEntry{{Key: "badge", Value: "star", Priority: 10}}
	if err := store.SaveGroup(g); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	uid := uuid.New()
	u := model.NewUser(uid, "LiveTester")
	u.PrimaryGroup = "donor"
	u.Parents = []model.Inheritance{{Group: "donor"}}
	if err := store.SaveUser(u); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}

	b, err := store.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if b.Groups["donor"] == nil || b.Users[uid] == nil || b.Groups["donor"].Meta["Shared"] != "ExactCase" {
		t.Fatalf("live reload mismatch: %+v", b)
	}
	_ = store.DeleteUser(uid)
	_ = store.DeleteGroup("donor")
}
