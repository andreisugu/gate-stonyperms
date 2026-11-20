package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

const testToken = "test-token-0123456789abcdef"

func newTestServer(t *testing.T) (*Server, *manager.Manager) {
	t.Helper()

	dir := t.TempDir()
	store, err := storage.NewJSONStorage(filepath.Join(dir, "stonyperms"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(mgr.Close)

	srv, err := New(mgr, Options{
		Listen:       "127.0.0.1:0",
		Token:        testToken,
		SnapshotTTL:  30 * time.Second,
		DefaultGroup: "default",
	})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return srv, mgr
}

func doRequest(t *testing.T, handler http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthEndpointsAndAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()

	if rec := doRequest(t, h, http.MethodGet, "/healthz", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}

	if rec := doRequest(t, h, http.MethodGet, "/v1/health", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/v1/health", "wrong-token-0123456789", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", rec.Code)
	}

	rec := doRequest(t, h, http.MethodGet, "/v1/health", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), testToken) {
		t.Fatal("response leaked the bearer token")
	}
	var health healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.SchemaVersion != SchemaVersion || health.Revision == 0 {
		t.Fatalf("unexpected health: %+v", health)
	}
}

func TestPlayerSnapshotInheritanceAndContexts(t *testing.T) {
	srv, mgr := newTestServer(t)
	h := srv.Handler()

	vip, err := mgr.CreateGroup("vip", 50, "&aVIP")
	if err != nil {
		t.Fatalf("create vip: %v", err)
	}
	vip.AddNode(model.NewNode("bridge.vip", true))
	vip.AddNode(model.Node{Key: "bridge.lobby", Value: true, Contexts: map[string]string{"server": "lobby"}})
	if err := mgr.SaveGroup(vip); err != nil {
		t.Fatalf("save vip: %v", err)
	}

	id := uuid.New()
	user := mgr.GetOrCreateUser(id, "Steve")
	user.AddParent(model.NewInheritance("vip"))
	user.AddNode(model.NewNode("bridge.direct", true))
	if err := mgr.SaveUser(user); err != nil {
		t.Fatalf("save user: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/v1/players/"+id.String()+"?server=lobby", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d body=%s", rec.Code, rec.Body.String())
	}

	var snap snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.User.UUID != id.String() || snap.User.Username != "Steve" || snap.User.Synthetic {
		t.Fatalf("unexpected user: %+v", snap.User)
	}
	if snap.Revision == 0 || snap.TTLSeconds != 30 {
		t.Fatalf("unexpected revision/ttl: %d/%d", snap.Revision, snap.TTLSeconds)
	}

	if !hasNode(snap.Nodes, "bridge.vip", true, "group:vip") {
		t.Fatalf("missing inherited vip node: %+v", snap.Nodes)
	}
	if !hasNode(snap.Nodes, "bridge.direct", true, "user") {
		t.Fatalf("missing direct node: %+v", snap.Nodes)
	}
	if !hasNode(snap.Nodes, "bridge.lobby", true, "group:vip") {
		t.Fatalf("missing context-matching node: %+v", snap.Nodes)
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/players/"+id.String()+"?server=hub", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("hub snapshot status = %d", rec.Code)
	}
	var hub snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &hub); err != nil {
		t.Fatalf("decode hub snapshot: %v", err)
	}
	if hasNode(hub.Nodes, "bridge.lobby", true, "group:vip") {
		t.Fatal("context-bound node leaked into a non-matching context")
	}
}

func TestUnknownUserGetsSyntheticDefaultSnapshot(t *testing.T) {
	srv, _ := newTestServer(t)
	id := uuid.New()

	rec := doRequest(t, srv.Handler(), http.MethodGet, "/v1/players/"+id.String(), testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snap snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !snap.User.Synthetic {
		t.Fatal("expected synthetic user")
	}
	if snap.User.PrimaryGroup != "default" {
		t.Fatalf("primary group = %q, want default", snap.User.PrimaryGroup)
	}
	if !hasNode(snap.Nodes, "gate.command.server", true, "group:default") {
		t.Fatalf("default group nodes missing: %+v", snap.Nodes)
	}
}

func TestSnapshotETagAndRevision(t *testing.T) {
	srv, mgr := newTestServer(t)
	h := srv.Handler()
	id := uuid.New()
	mgr.GetOrCreateUser(id, "Alex")

	rec := doRequest(t, h, http.MethodGet, "/v1/players/"+id.String(), testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/players/"+id.String(), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}

	if _, err := mgr.CreateGroup("newgroup", 10, ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	rec3 := doRequest(t, h, http.MethodGet, "/v1/players/"+id.String(), testToken, nil)
	if rec3.Code != http.StatusOK {
		t.Fatalf("after change status = %d", rec3.Code)
	}
	if rec3.Header().Get("ETag") == etag {
		t.Fatal("ETag did not change after a data mutation")
	}
}

func TestCheckEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()
	id := uuid.New()

	body := []byte(`{"uuid":"` + id.String() + `","permission":"gate.command.server"}`)
	rec := doRequest(t, h, http.MethodPost, "/v1/check", testToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("check status = %d body=%s", rec.Code, rec.Body.String())
	}
	var check checkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &check); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	if check.Value != "true" || check.Source != "group:default" {
		t.Fatalf("unexpected check: %+v", check)
	}

	body = []byte(`{"uuid":"` + id.String() + `","permission":"unknown.node"}`)
	rec = doRequest(t, h, http.MethodPost, "/v1/check", testToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown check status = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &check); err != nil {
		t.Fatalf("decode unknown check: %v", err)
	}
	if check.Value != "undefined" {
		t.Fatalf("value = %q, want undefined", check.Value)
	}

	body = []byte(`{"uuid":"` + id.String() + `","permission":"x","extra":true}`)
	if rec := doRequest(t, h, http.MethodPost, "/v1/check", testToken, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", rec.Code)
	}
}

func TestGroupsEndpoints(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()

	rec := doRequest(t, h, http.MethodGet, "/v1/groups", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("groups status = %d", rec.Code)
	}
	var groups groupsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(groups.Groups) < 2 {
		t.Fatalf("expected default groups, got %d", len(groups.Groups))
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/groups/admin", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("group status = %d body=%s", rec.Code, rec.Body.String())
	}
	var one groupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if one.Group.Name != "admin" {
		t.Fatalf("group = %q, want admin", one.Group.Name)
	}

	if rec := doRequest(t, h, http.MethodGet, "/v1/groups/missing", testToken, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing group status = %d, want 404", rec.Code)
	}

	// The manager allows names such as "vip+"; the API must not be stricter.
	if _, err := srv.mgr.CreateGroup("vip+", 50, ""); err != nil {
		t.Fatalf("create vip+: %v", err)
	}
	if rec := doRequest(t, h, http.MethodGet, "/v1/groups/vip+", testToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("vip+ group status = %d body=%s", rec.Code, rec.Body.String())
	}

	// A non-matching special name is a lookup miss, not a validation error.
	if rec := doRequest(t, h, http.MethodGet, "/v1/groups/bad!name", testToken, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown special group status = %d, want 404", rec.Code)
	}
}

func TestCheckRejectsTrailingJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	h := srv.Handler()
	id := uuid.New()
	valid := `{"uuid":"` + id.String() + `","permission":"x"}`

	for _, body := range []string{
		valid + ` trailing`,
		valid + `{"partial":`,
		valid + ` {}`,
	} {
		if rec := doRequest(t, h, http.MethodPost, "/v1/check", testToken, []byte(body)); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, rec.Code)
		}
	}
}

func TestSnapshotETagFingerprintAndGeneration(t *testing.T) {
	base := snapshotResponse{
		User:   userDTO{UUID: "00000000-0000-0000-0000-000000000001", Username: "Steve"},
		Groups: []groupDTO{{Name: "default", Weight: 0}},
		Nodes:  []nodeDTO{{Key: "temp.node", Value: true, Expiry: 100, Source: "user", User: true}},
	}
	first := snapshotETag("generation-a", 7, base)

	base.Nodes[0].Expiry = 200
	second := snapshotETag("generation-a", 7, base)
	if first == second {
		t.Fatal("ETag did not change when a node expiry changed")
	}

	base.Nodes[0].Expiry = 100
	if first != snapshotETag("generation-a", 7, base) {
		t.Fatal("ETag changed for identical effective state")
	}
	if first == snapshotETag("generation-b", 7, base) {
		t.Fatal("ETag reused across process generations")
	}
}

func TestStartRejectsInvalidTLSWithoutLeakingPort(t *testing.T) {
	_, mgr := newTestServer(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	srv, err := New(mgr, Options{
		Listen:      addr,
		Token:       testToken,
		TLSCertFile: certPath,
		TLSKeyFile:  keyPath,
	})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	if err := srv.Start(context.Background()); err == nil {
		t.Fatal("expected invalid TLS certificate to fail startup")
	}

	// A failed certificate load must not leave the address bound.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("address still bound after TLS setup failure: %v", err)
	}
	_ = ln.Close()
}

func TestRejectsRemoteBindWithoutOptIn(t *testing.T) {
	dir := t.TempDir()
	store, _ := storage.NewJSONStorage(filepath.Join(dir, "stonyperms"))
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	defer mgr.Close()

	if _, err := New(mgr, Options{Listen: "0.0.0.0:8737", Token: testToken}); err == nil {
		t.Fatal("expected remote bind to be rejected")
	}
	if _, err := New(mgr, Options{Listen: "0.0.0.0:8737", Token: testToken, AllowRemote: true}); err != nil {
		t.Fatalf("remote bind with opt-in failed: %v", err)
	}
	if _, err := New(mgr, Options{Listen: "127.0.0.1:8737", Token: "short"}); err == nil {
		t.Fatal("expected short token to be rejected")
	}
}

func TestLoadToken(t *testing.T) {
	t.Setenv("STONYPERMS_API_TOKEN", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("  file-token-0123456789  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := LoadToken("", path)
	if err != nil || token != "file-token-0123456789" {
		t.Fatalf("file token = %q, %v", token, err)
	}

	t.Setenv("STONYPERMS_API_TOKEN", "env-token-0123456789")
	token, err = LoadToken("inline-token-0123456789", path)
	if err != nil || token != "env-token-0123456789" {
		t.Fatalf("env token = %q, %v", token, err)
	}
}

func hasNode(nodes []nodeDTO, key string, value bool, source string) bool {
	for _, n := range nodes {
		if n.Key == key && n.Value == value && n.Source == source {
			return true
		}
	}
	return false
}
