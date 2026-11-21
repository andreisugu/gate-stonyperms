package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

const (
	testReadToken2  = "read-token-rotated-0123456789"
	testWriteToken  = "write-token-primary-0123456789"
	testWriteToken2 = "write-token-rotated-0123456789"
)

func newWriteTestServer(t *testing.T) (*Server, *manager.Manager) {
	t.Helper()
	store, err := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(mgr.Close)

	srv, err := New(mgr, Options{
		Listen:               "127.0.0.1:0",
		Tokens:               []string{testToken, testReadToken2},
		WriteEnabled:         true,
		WriteTokens:          []string{testWriteToken, testWriteToken2},
		RateLimitPerSec:      10000,
		RateLimitBurst:       10000,
		WriteRateLimitPerSec: 10000,
		WriteRateLimitBurst:  10000,
		SSEEnabled:           true,
		SSEMaxClients:        32,
		DefaultGroup:         "default",
	})
	if err != nil {
		t.Fatalf("new write api: %v", err)
	}
	return srv, mgr
}

func doWriteRequest(t *testing.T, handler http.Handler, method, path, token, actor, idempKey string, body []byte) *httptest.ResponseRecorder {
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
	if actor != "" {
		req.Header.Set("X-StonyPerms-Actor", actor)
	}
	if idempKey != "" {
		req.Header.Set("Idempotency-Key", idempKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var errBody errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	if errBody.Error != wantCode {
		t.Fatalf("error code = %q, want %q; body=%s", errBody.Error, wantCode, rec.Body.String())
	}
}

func TestLoadTokensRotationAndValidation(t *testing.T) {
	t.Setenv("STONYPERMS_TEST_TOKENS", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	content := "\n# primary token\n  token-one-0123456789  \n\n# duplicate ignored\ntoken-one-0123456789\ntoken-two-0123456789\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, err := LoadTokens("inline-ignored-012345", path, "STONYPERMS_TEST_TOKENS")
	if err != nil {
		t.Fatalf("LoadTokens file: %v", err)
	}
	if len(tokens) != 2 || tokens[0] != "token-one-0123456789" || tokens[1] != "token-two-0123456789" {
		t.Fatalf("unexpected file tokens: %+v", tokens)
	}

	// Env var takes precedence and supports multi-line tokens.
	t.Setenv("STONYPERMS_TEST_TOKENS", "env-one-0123456789\n# skip\nenv-two-0123456789")
	tokens, err = LoadTokens("", path, "STONYPERMS_TEST_TOKENS")
	if err != nil || len(tokens) != 2 || tokens[0] != "env-one-0123456789" || tokens[1] != "env-two-0123456789" {
		t.Fatalf("unexpected env tokens: %+v, %v", tokens, err)
	}

	// Reject >4 tokens per tier.
	t.Setenv("STONYPERMS_TEST_TOKENS", "tok1\ntok2\ntok3\ntok4\ntok5")
	if _, err := LoadTokens("", "", "STONYPERMS_TEST_TOKENS"); err == nil {
		t.Fatal("expected error for >4 tokens")
	}

	// Reject empty/comment-only token set.
	t.Setenv("STONYPERMS_TEST_TOKENS", "# comment only\n   \n")
	if _, err := LoadTokens("", "", "STONYPERMS_TEST_TOKENS"); err == nil {
		t.Fatal("expected error for empty token set")
	}
}

func TestScopedAuthMatrixAndHealthFlags(t *testing.T) {
	store, _ := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	// Reject overlapping read & write tokens and missing/short write tokens.
	if _, err := New(mgr, Options{Tokens: []string{testToken}, WriteEnabled: true}); err == nil {
		t.Fatal("expected missing write tokens to fail")
	}
	if _, err := New(mgr, Options{Tokens: []string{testToken}, WriteEnabled: true, WriteTokens: []string{"short"}}); err == nil {
		t.Fatal("expected short write token to fail")
	}
	if _, err := New(mgr, Options{Tokens: []string{testToken}, WriteEnabled: true, WriteTokens: []string{testWriteToken, testToken}}); err == nil {
		t.Fatal("expected overlapping read/write token to refuse startup")
	}

	// Disabled writes: read token gets 403 writes_disabled on write routes, bad token gets 401.
	roSrv, err := New(mgr, Options{Tokens: []string{testToken}, WriteEnabled: false, SSEEnabled: false})
	if err != nil {
		t.Fatalf("new ro server: %v", err)
	}
	assertErrorCode(t, doWriteRequest(t, roSrv.Handler(), http.MethodPost, "/v1/groups", "", "", "", []byte(`{"name":"vip"}`)), http.StatusUnauthorized, "unauthorized")
	assertErrorCode(t, doWriteRequest(t, roSrv.Handler(), http.MethodPost, "/v1/groups", testToken, "", "", []byte(`{"name":"vip"}`)), http.StatusForbidden, "writes_disabled")

	rec := doRequest(t, roSrv.Handler(), http.MethodGet, "/v1/health", testToken, nil)
	var roHealth healthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &roHealth)
	if roHealth.WriteEnabled || roHealth.SSEEnabled {
		t.Fatalf("expected writeEnabled=false sseEnabled=false, got %+v", roHealth)
	}

	// Enabled writes: rotated read tokens can read, get 403 insufficient_scope on write; rotated write tokens can read and write.
	rwSrv, _ := newWriteTestServer(t)
	h := rwSrv.Handler()

	rec = doRequest(t, h, http.MethodGet, "/v1/health", testReadToken2, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotated read token health status = %d", rec.Code)
	}
	var rwHealth healthResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rwHealth)
	if !rwHealth.WriteEnabled || !rwHealth.SSEEnabled {
		t.Fatalf("expected writeEnabled=true sseEnabled=true, got %+v", rwHealth)
	}

	// Write token is accepted on read endpoints too.
	if rec := doRequest(t, h, http.MethodGet, "/v1/health", testWriteToken2, nil); rec.Code != http.StatusOK {
		t.Fatalf("write token on read endpoint status = %d", rec.Code)
	}

	// Read token on write & admin endpoints -> 403 insufficient_scope.
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testReadToken2, "", "", []byte(`{"name":"vip"}`)), http.StatusForbidden, "insufficient_scope")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodGet, "/v1/logs", testToken, "", "", nil), http.StatusForbidden, "insufficient_scope")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/reload", testToken, "", "", nil), http.StatusForbidden, "insufficient_scope")
}

func TestReadEndpointsUsersBatchMembersTracks(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()

	_, _ = mgr.CreateGroup("vip", 50, "&aVIP")
	u1 := uuid.New()
	u2 := uuid.New()
	_, _, _ = mgr.MutateUser(u1, "Alpha", true, "default", "seed", func(u *model.User) (bool, string, error) {
		u.AddParent(model.NewInheritance("vip"))
		u.AddNode(model.Node{Key: "lobby.perk", Value: true, Contexts: map[string]string{"server": "lobby"}})
		return true, "seed alpha", nil
	})
	_, _, _ = mgr.MutateUser(u2, "Beta", true, "default", "seed", func(u *model.User) (bool, string, error) {
		u.AddParent(model.Inheritance{Group: "vip", Expiry: time.Now().Add(-time.Minute).Unix()})
		return true, "seed beta", nil
	})

	// POST /v1/players/batch
	unknownID := uuid.New()
	batchBody, _ := json.Marshal(playersBatchRequest{
		UUIDs:    []string{u1.String(), unknownID.String()},
		Contexts: map[string]string{"server": "lobby"},
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/players/batch", testToken, batchBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("players batch status = %d body=%s", rec.Code, rec.Body.String())
	}
	var batchResp playersBatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &batchResp); err != nil {
		t.Fatalf("decode players batch: %v", err)
	}
	if len(batchResp.Players) != 2 || batchResp.Players[u1.String()].User.Synthetic || !batchResp.Players[unknownID.String()].User.Synthetic {
		t.Fatalf("unexpected players batch: %+v", batchResp)
	}
	if !hasNode(batchResp.Players[u1.String()].Nodes, "lobby.perk", true, "user") {
		t.Fatalf("missing lobby.perk in batch snapshot: %+v", batchResp.Players[u1.String()].Nodes)
	}

	// Reject >100 UUIDs in batch.
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	tooManyBody, _ := json.Marshal(playersBatchRequest{UUIDs: tooMany})
	assertErrorCode(t, doRequest(t, h, http.MethodPost, "/v1/players/batch", testToken, tooManyBody), http.StatusBadRequest, "invalid_batch")

	// GET /v1/users with group filter and pagination
	rec = doRequest(t, h, http.MethodGet, "/v1/users?group=vip&limit=10&offset=0", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("users list status = %d", rec.Code)
	}
	var usersResp usersResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &usersResp)
	if usersResp.Total != 1 || len(usersResp.Users) != 1 || usersResp.Users[0].Username != "Alpha" {
		t.Fatalf("unexpected filtered users: %+v", usersResp)
	}

	// GET /v1/users/by-name/{username} raw vs ?effective=true
	rec = doRequest(t, h, http.MethodGet, "/v1/users/by-name/alpha", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("by-name raw status = %d body=%s", rec.Code, rec.Body.String())
	}
	var userResp userRecordResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &userResp)
	if userResp.User.UUID != u1 {
		t.Fatalf("by-name returned wrong user: %+v", userResp.User)
	}

	rec = doRequest(t, h, http.MethodGet, "/v1/users/by-name/alpha?effective=true&server=lobby", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("by-name effective status = %d", rec.Code)
	}
	var effSnap snapshotResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &effSnap)
	if !hasNode(effSnap.Nodes, "lobby.perk", true, "user") {
		t.Fatalf("expected lobby.perk in effective by-name snapshot: %+v", effSnap.Nodes)
	}

	assertErrorCode(t, doRequest(t, h, http.MethodGet, "/v1/users/by-name/nobody", testToken, nil), http.StatusNotFound, "user_not_found")
	assertErrorCode(t, doRequest(t, h, http.MethodGet, "/v1/users/nobody", testToken, nil), http.StatusNotFound, "user_not_found")

	// GET /v1/users/{id} by UUID and username
	if rec := doRequest(t, h, http.MethodGet, "/v1/users/"+u2.String(), testToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("get user by uuid status = %d", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/v1/users/Beta", testToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("get user by name status = %d", rec.Code)
	}

	// GET /v1/groups/{name}/members
	rec = doRequest(t, h, http.MethodGet, "/v1/groups/vip/members", testToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("group members status = %d", rec.Code)
	}
	var gmResp groupMembersResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &gmResp)
	if len(gmResp.Members) != 1 || gmResp.Members[0].Username != "Alpha" {
		t.Fatalf("unexpected group members: %+v", gmResp)
	}
	assertErrorCode(t, doRequest(t, h, http.MethodGet, "/v1/groups/missing/members", testToken, nil), http.StatusNotFound, "group_not_found")

	// GET /v1/tracks and /v1/tracks/{name}
	if rec := doRequest(t, h, http.MethodGet, "/v1/tracks", testToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("tracks list status = %d", rec.Code)
	}
	if rec := doRequest(t, h, http.MethodGet, "/v1/tracks/ranks", testToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("track get status = %d", rec.Code)
	}
	assertErrorCode(t, doRequest(t, h, http.MethodGet, "/v1/tracks/missing", testToken, nil), http.StatusNotFound, "track_not_found")
}

func TestUserWriteEndpointsParentsPermissionsMetaPromoteClear(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()

	// Seed groups & ranks track: default -> vip -> mvp -> admin
	for _, g := range []struct {
		name   string
		weight int
	}{{"vip", 20}, {"mvp", 50}} {
		rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "setup", "", []byte(`{"name":"`+g.name+`","weight":`+string(rune(48+g.weight/10))+`0}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create group %s failed: %d %s", g.name, rec.Code, rec.Body.String())
		}
	}
	rec := doWriteRequest(t, h, http.MethodPut, "/v1/tracks/ranks", testWriteToken, "setup", "", []byte(`{"groups":["default","vip","mvp","admin"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("update ranks track failed: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown non-UUID username on write subresource must return 404 (never synthesize offline UUID).
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/users/NotAnExistingUser/permissions", testWriteToken, "shop", "", []byte(`{"key":"test.perm"}`)), http.StatusNotFound, "user_not_found")
	if _, found, _ := mgr.ResolveUserByName("NotAnExistingUser"); found {
		t.Fatal("write endpoint fabricated offline user on username typo")
	}

	// POST /v1/users: offline user creation and subsequent username-based ensure without duplicate offline UUID.
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users", testWriteToken, "paynow", "", []byte(`{"username":"OfflineShopper","createOffline":true,"primaryGroup":"vip"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create offline user status = %d body=%s", rec.Code, rec.Body.String())
	}
	var ensured ensureUserResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ensured)
	if !ensured.Created || ensured.User.PrimaryGroup != "vip" {
		t.Fatalf("unexpected ensure response: %+v", ensured)
	}

	// Online UUID user ensured by username matches existing online UUID instead of creating offline duplicate.
	onlineID := uuid.New()
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users", testWriteToken, "lobby", "", []byte(`{"uuid":"`+onlineID.String()+`","username":"OnlineHero"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create online user status = %d", rec.Code)
	}
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users", testWriteToken, "paynow", "", []byte(`{"username":"onlinehero","createOffline":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("ensure existing by name status = %d body=%s", rec.Code, rec.Body.String())
	}
	var ensured2 ensureUserResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ensured2)
	if ensured2.Created || ensured2.User.UUID != onlineID {
		t.Fatalf("expected existing online UUID %s, got %+v", onlineID, ensured2)
	}

	// Parent accumulate: new temp -> extends active -> permanent stays permanent.
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/parents", testWriteToken, "paynow", "", []byte(`{"group":"vip","mode":"accumulate","durationSeconds":3600,"contexts":{"server":"lobby"}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accumulate 1 failed: %d %s", rec.Code, rec.Body.String())
	}
	var ur userRecordResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ur)
	firstExp := ur.User.Parents[0].Expiry

	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/parents", testWriteToken, "paynow", "", []byte(`{"group":"vip","mode":"accumulate","durationSeconds":1800,"contexts":{"server":"lobby"}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accumulate 2 failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ur)
	if ur.User.Parents[0].Expiry != firstExp+1800 {
		t.Fatalf("expected accumulated expiry %d, got %d", firstExp+1800, ur.User.Parents[0].Expiry)
	}

	// Promote to permanent vip, then accumulate again -> stays permanent (expiry == 0).
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/parents", testWriteToken, "paynow", "", []byte(`{"group":"vip","mode":"add","setPrimary":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("permanent add failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/parents", testWriteToken, "paynow", "", []byte(`{"group":"vip","mode":"accumulate","durationSeconds":600}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("accumulate permanent failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ur)
	for _, p := range ur.User.Parents {
		if p.Group == "vip" && len(p.Contexts) == 0 && p.Expiry != 0 {
			t.Fatalf("permanent parent downgraded to temporary: %+v", p)
		}
	}

	// Remove parent resets primaryGroup to default when primary was removed.
	rec = doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/parents/vip", testWriteToken, "paynow", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete parent failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ur)
	if ur.User.PrimaryGroup != "default" {
		t.Fatalf("expected primaryGroup reset to default, got %q", ur.User.PrimaryGroup)
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/parents/vip", testWriteToken, "paynow", "", nil), http.StatusNotFound, "parent_not_found")

	// Primary group PUT
	rec = doWriteRequest(t, h, http.MethodPut, "/v1/users/"+onlineID.String()+"/primary-group", testWriteToken, "admin", "", []byte(`{"group":"vip"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("put primary group failed: %d %s", rec.Code, rec.Body.String())
	}

	// Permissions POST (default value=true and explicit deny value=false) + DELETE via query & body
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/permissions", testWriteToken, "admin", "", []byte(`{"key":"shop.kit","contexts":{"server":"smp"}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set perm default true failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/permissions", testWriteToken, "admin", "", []byte(`{"key":"shop.ban","value":false}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set perm false failed: %d %s", rec.Code, rec.Body.String())
	}
	// Delete via query param
	rec = doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/permissions?key=shop.kit&server=smp", testWriteToken, "admin", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete perm query failed: %d %s", rec.Code, rec.Body.String())
	}
	// Delete via JSON body
	rec = doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/permissions", testWriteToken, "admin", "", []byte(`{"key":"shop.ban"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete perm body failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/permissions?key=missing.node", testWriteToken, "admin", "", nil), http.StatusNotFound, "node_not_found")

	// Meta + Prefix + Suffix
	if rec := doWriteRequest(t, h, http.MethodPut, "/v1/users/"+onlineID.String()+"/meta/rank_tier", testWriteToken, "admin", "", []byte(`{"value":"gold"}`)); rec.Code != http.StatusOK {
		t.Fatalf("put meta failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/meta/rank_tier", testWriteToken, "admin", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete meta failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/meta/rank_tier", testWriteToken, "admin", "", nil), http.StatusNotFound, "meta_not_found")

	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/prefixes", testWriteToken, "admin", "", []byte(`{"priority":120,"value":"&e[Hero] "}`)); rec.Code != http.StatusOK {
		t.Fatalf("post prefix failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/prefixes/120", testWriteToken, "admin", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete prefix failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/prefixes/120", testWriteToken, "admin", "", nil), http.StatusNotFound, "prefix_not_found")

	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/suffixes", testWriteToken, "admin", "", []byte(`{"priority":50,"value":" &7[S]"}`)); rec.Code != http.StatusOK {
		t.Fatalf("post suffix failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String()+"/suffixes/50", testWriteToken, "admin", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete suffix failed: %d %s", rec.Code, rec.Body.String())
	}

	// Promote / Demote
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/promote", testWriteToken, "staff", "", []byte(`{"track":"ranks"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("promote failed: %d %s", rec.Code, rec.Body.String())
	}
	var trResp trackActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &trResp)
	if trResp.FromGroup != "vip" || trResp.ToGroup != "mvp" {
		t.Fatalf("unexpected promote transition: %+v", trResp)
	}
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/demote", testWriteToken, "staff", "", []byte(`{"track":"ranks"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("demote failed: %d %s", rec.Code, rec.Body.String())
	}

	// Clear scopes + Delete user
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+onlineID.String()+"/clear", testWriteToken, "staff", "", []byte(`{"scope":"all"}`)); rec.Code != http.StatusOK {
		t.Fatalf("clear failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String(), testWriteToken, "staff", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete user failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+onlineID.String(), testWriteToken, "staff", "", nil), http.StatusNotFound, "user_not_found")
}

func TestUserBatchAtomicityAndValidation(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()
	_, _ = mgr.CreateGroup("vip", 50, "VIP")

	uid := uuid.New()
	_, _, _, _ = mgr.ResolveOrCreateUser(uid, "BatchUser", "default", "seed")
	revBefore := mgr.Revision()

	// Failing batch (op 1 valid, op 2 references missing group) must leave user and revision untouched!
	badBatch := []byte(`{
		"operations": [
			{"op": "parent_add", "group": "vip"},
			{"op": "permission_set", "key": "batch.node", "value": true},
			{"op": "parent_add", "group": "does_not_exist"}
		]
	}`)
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/batch", testWriteToken, "paynow", "", badBatch), http.StatusNotFound, "group_not_found")
	if mgr.Revision() != revBefore {
		t.Fatalf("failed batch bumped revision: %d -> %d", revBefore, mgr.Revision())
	}
	snap, _, _ := mgr.UserSnapshot(uid)
	if len(snap.Parents) != 0 || len(snap.Nodes) != 0 {
		t.Fatalf("failed batch partially mutated user: %+v", snap)
	}

	// Valid multi-op batch applies all ops in one revision bump.
	goodBatch := []byte(`{
		"operations": [
			{"op": "parent_add", "group": "vip", "setPrimary": true},
			{"op": "permission_set", "key": "batch.fly", "value": true, "contexts": {"server": "lobby"}},
			{"op": "meta_set", "key": "badge", "value": "star"},
			{"op": "prefix_set", "priority": 200, "value": "&b[Star] "},
			{"op": "suffix_set", "priority": 10, "value": " &a*"}
		]
	}`)
	rec := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/batch", testWriteToken, "paynow", "", goodBatch)
	if rec.Code != http.StatusOK {
		t.Fatalf("good batch failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp batchUserResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Applied != 5 || resp.Revision != revBefore+1 || resp.User.PrimaryGroup != "vip" || resp.User.Meta["badge"] != "star" || len(resp.User.Prefixes) != 1 || len(resp.User.Suffixes) != 1 {
		t.Fatalf("unexpected good batch response: %+v", resp)
	}
}

func TestGroupTrackCRUDAndAdminLogsReload(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()

	// Create group vip+ (matches ^[a-z0-9_+-]{1,64}$), patch, permissions, parents, meta, prefixes, suffixes, delete.
	rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "admin-ui", "", []byte(`{"name":"vip+","weight":75,"displayName":"&bVIP+"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create vip+ failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "admin-ui", "", []byte(`{"name":"bad!group"}`)), http.StatusBadRequest, "invalid_group")

	if rec := doWriteRequest(t, h, http.MethodPatch, "/v1/groups/vip+", testWriteToken, "admin-ui", "", []byte(`{"weight":80}`)); rec.Code != http.StatusOK {
		t.Fatalf("patch vip+ failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups/vip+/permissions", testWriteToken, "admin-ui", "", []byte(`{"key":"vip.plus","value":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("group perm set failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/vip+/permissions", testWriteToken, "admin-ui", "", []byte(`{"key":"vip.plus"}`)); rec.Code != http.StatusOK {
		t.Fatalf("group perm delete failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups/vip+/parents", testWriteToken, "admin-ui", "", []byte(`{"group":"default"}`)); rec.Code != http.StatusOK {
		t.Fatalf("group add parent failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups/vip+/parents", testWriteToken, "admin-ui", "", []byte(`{"group":"vip+"}`)), http.StatusBadRequest, "invalid_parent")
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/vip+/parents/default", testWriteToken, "admin-ui", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("group delete parent failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodPut, "/v1/groups/vip+/meta/color", testWriteToken, "admin-ui", "", []byte(`{"value":"aqua"}`)); rec.Code != http.StatusOK {
		t.Fatalf("group set meta failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups/vip+/prefixes", testWriteToken, "admin-ui", "", []byte(`{"priority":75,"value":"&b[VIP+] "}`)); rec.Code != http.StatusOK {
		t.Fatalf("group set prefix failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/vip+/prefixes/75", testWriteToken, "admin-ui", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("group del prefix failed: %d %s", rec.Code, rec.Body.String())
	}

	// Default group cannot be deleted.
	assertErrorCode(t, doWriteRequest(t, h, http.MethodDelete, "/v1/groups/default", testWriteToken, "admin-ui", "", nil), http.StatusBadRequest, "cannot_delete_default")

	// Track CRUD
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/tracks", testWriteToken, "admin-ui", "", []byte(`{"name":"donator","groups":["default","vip+"]}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create track failed: %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPut, "/v1/tracks/donator", testWriteToken, "admin-ui", "", []byte(`{"groups":["default","missing"]}`)), http.StatusNotFound, "group_not_found")
	if rec := doWriteRequest(t, h, http.MethodPut, "/v1/tracks/donator", testWriteToken, "admin-ui", "", []byte(`{"groups":["default","vip+","admin"]}`)); rec.Code != http.StatusOK {
		t.Fatalf("put track failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/tracks/donator", testWriteToken, "admin-ui", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete track failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/vip+", testWriteToken, "admin-ui", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete vip+ failed: %d %s", rec.Code, rec.Body.String())
	}

	// Admin /v1/logs & /v1/reload
	rec = doWriteRequest(t, h, http.MethodGet, "/v1/logs?limit=20&actor=admin-ui", testWriteToken, "", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get logs failed: %d %s", rec.Code, rec.Body.String())
	}
	var logsResp logsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &logsResp)
	if len(logsResp.Logs) < 5 {
		t.Fatalf("expected audit logs for admin-ui, got %d", len(logsResp.Logs))
	}

	revBefore := mgr.Revision()
	rec = doWriteRequest(t, h, http.MethodPost, "/v1/reload", testWriteToken, "ops", "", nil)
	if rec.Code != http.StatusOK || mgr.Revision() <= revBefore {
		t.Fatalf("reload failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestValidationBoundsRateLimitAndIdempotency(t *testing.T) {
	// 1. Rate limiting (read & write independent, ignores X-Forwarded-For).
	store, _ := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	mgr, _ := manager.New(store)
	defer mgr.Close()

	rlSrv, err := New(mgr, Options{
		Tokens:               []string{testToken},
		WriteEnabled:         true,
		WriteTokens:          []string{testWriteToken},
		RateLimitPerSec:      1,
		RateLimitBurst:       3,
		WriteRateLimitPerSec: 1,
		WriteRateLimitBurst:  2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Freeze limiter clock for deterministic burst exhaustion.
	fixedNow := time.Now()
	rlSrv.readLimiter.nowFn = func() time.Time { return fixedNow }
	rlSrv.writeLimiter.nowFn = func() time.Time { return fixedNow }

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
		req.RemoteAddr = "10.0.0.5:1000"
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("X-Forwarded-For", "198.51.100."+string(rune(49+i))) // must be ignored
		rec := httptest.NewRecorder()
		rlSrv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("read burst %d failed: %d", i, rec.Code)
		}
	}
	req4 := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req4.RemoteAddr = "10.0.0.5:2000"
	req4.Header.Set("Authorization", "Bearer "+testToken)
	req4.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec4 := httptest.NewRecorder()
	rlSrv.Handler().ServeHTTP(rec4, req4)
	assertErrorCode(t, rec4, http.StatusTooManyRequests, "rate_limited")
	if rec4.Header().Get("Retry-After") != "1" {
		t.Fatalf("missing Retry-After header: %q", rec4.Header().Get("Retry-After"))
	}

	// Write limiter has its own burst of 2 on the same IP.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewReader([]byte(`{"name":"rl`+string(rune(97+i))+`"}`)))
		req.RemoteAddr = "10.0.0.5:3000"
		req.Header.Set("Authorization", "Bearer "+testWriteToken)
		rec := httptest.NewRecorder()
		rlSrv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("write burst %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	wReq3 := httptest.NewRequest(http.MethodPost, "/v1/groups", bytes.NewReader([]byte(`{"name":"rlc"}`)))
	wReq3.RemoteAddr = "10.0.0.5:3001"
	wReq3.Header.Set("Authorization", "Bearer "+testWriteToken)
	wRec3 := httptest.NewRecorder()
	rlSrv.Handler().ServeHTTP(wRec3, wReq3)
	assertErrorCode(t, wRec3, http.StatusTooManyRequests, "rate_limited")

	// 2. Idempotency replay, conflict, validation, and 5xx non-caching.
	srv, mgr2 := newWriteTestServer(t)
	h := srv.Handler()

	// Invalid actor and invalid idempotency key
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "bad\nactor", "", []byte(`{"name":"x"}`)), http.StatusBadRequest, "invalid_actor")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "ok", "bad\x01key", []byte(`{"name":"x"}`)), http.StatusBadRequest, "invalid_idempotency_key")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "ok", "", []byte(`{"name":"g1","displayName":"ok","extra":1}`)), http.StatusBadRequest, "invalid_body")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uuid.NewString()+"/permissions", testWriteToken, "ok", "", []byte(`{"key":"bad space"}`)), http.StatusBadRequest, "invalid_permission")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uuid.NewString()+"/permissions", testWriteToken, "ok", "", []byte(`{"key":"valid.key","durationSeconds":10,"expiry":9999999999}`)), http.StatusBadRequest, "invalid_expiry")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPut, "/v1/users/"+uuid.NewString()+"/meta/bad%20key", testWriteToken, "ok", "", []byte(`{"value":"x"}`)), http.StatusBadRequest, "invalid_meta_key")
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPut, "/v1/users/"+uuid.NewString()+"/meta/good.key", testWriteToken, "ok", "", []byte(`{"value":"bad\u0007val"}`)), http.StatusBadRequest, "invalid_meta_value")

	// Replay exact POST create group
	createBody := []byte(`{"name":"idemp","weight":10}`)
	r1 := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "paynow", "key-create-1", createBody)
	if r1.Code != http.StatusCreated || r1.Header().Get("X-Idempotency-Replayed") != "" {
		t.Fatalf("first idemp request failed: code=%d hdr=%q body=%s", r1.Code, r1.Header().Get("X-Idempotency-Replayed"), r1.Body.String())
	}
	revAfterCreate := mgr2.Revision()

	r2 := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "paynow", "key-create-1", createBody)
	if r2.Code != http.StatusCreated || r2.Header().Get("X-Idempotency-Replayed") != "true" || r2.Body.String() != r1.Body.String() {
		t.Fatalf("replayed response mismatch: code=%d hdr=%q body=%s", r2.Code, r2.Header().Get("X-Idempotency-Replayed"), r2.Body.String())
	}
	if mgr2.Revision() != revAfterCreate {
		t.Fatal("replayed idempotent request mutated manager revision")
	}

	// Conflict when reusing same key with different body.
	assertErrorCode(t, doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "paynow", "key-create-1", []byte(`{"name":"other","weight":20}`)), http.StatusConflict, "idempotency_conflict")

	// Concurrent identical idempotent writes execute only once.
	uid := uuid.New()
	var wg sync.WaitGroup
	codes := make([]int, 5)
	replayed := make([]string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/parents", testWriteToken, "paynow", "concurrent-key", []byte(`{"group":"default"}`))
			codes[idx] = rec.Code
			replayed[idx] = rec.Header().Get("X-Idempotency-Replayed")
		}(i)
	}
	wg.Wait()
	freshCount := 0
	for i := 0; i < 5; i++ {
		if codes[i] != http.StatusOK {
			t.Fatalf("concurrent idemp[%d] code = %d", i, codes[i])
		}
		if replayed[i] == "" {
			freshCount++
		}
	}
	if freshCount != 1 {
		t.Fatalf("expected exactly 1 fresh execution across 5 concurrent retries, got %d (%v)", freshCount, replayed)
	}
}

func TestPersistenceFailureDoesNotCacheIdempotencyAndRollsBack(t *testing.T) {
	var failSave atomic.Bool
	base, _ := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	store := &failingUserWriteStorage{StorageProvider: base, fail: &failSave}
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	srv, err := New(mgr, Options{
		Tokens:               []string{testToken},
		WriteEnabled:         true,
		WriteTokens:          []string{testWriteToken},
		RateLimitPerSec:      10000,
		RateLimitBurst:       10000,
		WriteRateLimitPerSec: 10000,
		WriteRateLimitBurst:  10000,
	})
	if err != nil {
		t.Fatal(err)
	}

	uid := uuid.New()
	body := []byte(`{"key":"disk.check","value":true}`)
	failSave.Store(true)
	revBefore := mgr.Revision()
	rec := doWriteRequest(t, srv.Handler(), http.MethodPost, "/v1/users/"+uid.String()+"/permissions", testWriteToken, "ops", "retry-5xx", body)
	assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	if mgr.Revision() != revBefore {
		t.Fatal("failed persistence bumped revision")
	}

	// Retry with the same Idempotency-Key after disk recovers must succeed (5xx wasn't cached).
	failSave.Store(false)
	rec2 := doWriteRequest(t, srv.Handler(), http.MethodPost, "/v1/users/"+uid.String()+"/permissions", testWriteToken, "ops", "retry-5xx", body)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry after 5xx failed: %d %s", rec2.Code, rec2.Body.String())
	}
	if rec2.Header().Get("X-Idempotency-Replayed") != "" {
		t.Fatal("5xx failure was wrongly cached")
	}
}

type failingUserWriteStorage struct {
	storage.StorageProvider
	fail *atomic.Bool
}

func (f *failingUserWriteStorage) SaveUser(u *model.User) error {
	if f.fail.Load() {
		return errors.New("simulated disk write failure")
	}
	return f.StorageProvider.SaveUser(u)
}

func TestIdempotencyCacheTTLEvictionAndGroupDeleteForms(t *testing.T) {
	srv, _ := newWriteTestServer(t)
	h := srv.Handler()

	// Test TTL expiry on idempotency cache with controlled clock.
	now := time.Now()
	srv.idemp.nowFn = func() time.Time { return now }

	r1 := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "ops", "ttl-key", []byte(`{"name":"ttlgrp","weight":5}`))
	if r1.Code != http.StatusCreated {
		t.Fatalf("initial create failed: %d %s", r1.Code, r1.Body.String())
	}
	// Advance past 15m TTL -> entry expires, so repeating with same key executes fresh (hits group_exists 409 instead of replaying 201).
	now = now.Add(16 * time.Minute)
	r2 := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "ops", "ttl-key", []byte(`{"name":"ttlgrp","weight":5}`))
	assertErrorCode(t, r2, http.StatusConflict, "group_exists")
	if r2.Header().Get("X-Idempotency-Replayed") != "" {
		t.Fatal("expired idempotency entry was replayed")
	}

	// Test DELETE /v1/groups/{name}/parents without path param (JSON body and query) and group suffixes.
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups/ttlgrp/parents", testWriteToken, "ops", "", []byte(`{"group":"default","contexts":{"server":"lobby"}}`)); rec.Code != http.StatusOK {
		t.Fatalf("add parent failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/ttlgrp/parents", testWriteToken, "ops", "", []byte(`{"group":"default","contexts":{"server":"lobby"}}`)); rec.Code != http.StatusOK {
		t.Fatalf("delete parent via body failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodPost, "/v1/groups/ttlgrp/suffixes", testWriteToken, "ops", "", []byte(`{"priority":15,"value":" &7[T]"}`)); rec.Code != http.StatusOK {
		t.Fatalf("add suffix failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doWriteRequest(t, h, http.MethodDelete, "/v1/groups/ttlgrp/suffixes/15", testWriteToken, "ops", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete suffix failed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIdempotencyQueryParameterCanonicalizationAndConflict(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()

	uid := uuid.New()
	_, _, err := mgr.MutateUser(uid, "QueryIdemp", true, "default", "seed", func(u *model.User) (bool, string, error) {
		u.AddNode(model.Node{Key: "perm.a", Value: true, Contexts: map[string]string{"server": "lobby"}})
		u.AddNode(model.Node{Key: "perm.b", Value: true, Contexts: map[string]string{"server": "lobby"}})
		return true, "seed perms", nil
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// 1. Delete perm.a with query parameters and Idempotency-Key.
	r1 := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+uid.String()+"/permissions?key=perm.a&server=lobby", testWriteToken, "ops", "del-q-1", nil)
	if r1.Code != http.StatusOK || r1.Header().Get("X-Idempotency-Replayed") != "" {
		t.Fatalf("initial query delete failed: code=%d hdr=%q body=%s", r1.Code, r1.Header().Get("X-Idempotency-Replayed"), r1.Body.String())
	}
	revAfterFirst := mgr.Revision()

	// 2. Reordered identical query parameters + same Idempotency-Key replays cleanly.
	r2 := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+uid.String()+"/permissions?server=lobby&key=perm.a", testWriteToken, "ops", "del-q-1", nil)
	if r2.Code != http.StatusOK || r2.Header().Get("X-Idempotency-Replayed") != "true" || r2.Body.String() != r1.Body.String() {
		t.Fatalf("reordered query replay mismatch: code=%d hdr=%q body=%s", r2.Code, r2.Header().Get("X-Idempotency-Replayed"), r2.Body.String())
	}
	if mgr.Revision() != revAfterFirst {
		t.Fatal("replayed query delete mutated manager revision")
	}

	// 3. Different query parameter (?key=perm.b) with same Idempotency-Key returns 409 and preserves perm.b.
	r3 := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+uid.String()+"/permissions?key=perm.b&server=lobby", testWriteToken, "ops", "del-q-1", nil)
	assertErrorCode(t, r3, http.StatusConflict, "idempotency_conflict")

	snap, found, _ := mgr.UserSnapshot(uid)
	if !found || len(snap.Nodes) != 1 || snap.Nodes[0].Key != "perm.b" {
		t.Fatalf("expected perm.b to remain untouched after conflict, got %+v", snap)
	}
}

func TestAPISearchBulkUpdateModifiersMetaTracksAndAffixes(t *testing.T) {
	srv, mgr := newWriteTestServer(t)
	h := srv.Handler()

	mustJSON := func(v any) []byte {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json marshal: %v", err)
		}
		return b
	}

	// Create groups + track
	createGrp := doWriteRequest(t, h, http.MethodPost, "/v1/groups", testWriteToken, "ops", "", mustJSON(map[string]any{
		"name": "vip", "weight": 20, "displayName": "VIP",
	}))
	if createGrp.Code != http.StatusCreated {
		t.Fatalf("create vip: %d %s", createGrp.Code, createGrp.Body.String())
	}
	if _, err := mgr.CreateGroup("mvp", 40, "MVP"); err != nil {
		t.Fatalf("create mvp: %v", err)
	}
	tr, _ := mgr.CreateTrack("api_parity_ranks")
	_ = tr.Append("default")
	_ = tr.Append("vip")
	_ = tr.Append("mvp")
	_ = mgr.SaveTrack(tr)

	uid := uuid.New()
	if res := doWriteRequest(t, h, http.MethodPost, "/v1/users", testWriteToken, "ops", "", mustJSON(map[string]any{
		"uuid": uid.String(), "username": "ParityAPIUser", "createOffline": true,
	})); res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("ensure user: %d %s", res.Code, res.Body.String())
	}

	// 1. Primary group PUT auto-inherits vip
	pgRes := doWriteRequest(t, h, http.MethodPut, "/v1/users/"+uid.String()+"/primary-group", testWriteToken, "ops", "", mustJSON(map[string]any{
		"group": "vip",
	}))
	if pgRes.Code != http.StatusOK {
		t.Fatalf("put primary group: %d %s", pgRes.Code, pgRes.Body.String())
	}
	if u := mgr.GetUser(uid); len(u.Parents) == 0 || u.Parents[0].Group != "vip" {
		t.Fatalf("expected PUT primary-group to auto-inherit vip, got %+v", u.Parents)
	}

	// 2. Temporary permission modifiers: accumulate & deny
	rPerm1 := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/permissions", testWriteToken, "ops", "", mustJSON(map[string]any{
		"key": "essentials.fly", "value": true, "durationSeconds": 3600, "modifier": "accumulate",
	}))
	if rPerm1.Code != http.StatusOK {
		t.Fatalf("perm accumulate 1: %d %s", rPerm1.Code, rPerm1.Body.String())
	}
	rPerm2 := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/permissions", testWriteToken, "ops", "", mustJSON(map[string]any{
		"key": "essentials.fly", "value": true, "durationSeconds": 1800, "modifier": "accumulate",
	}))
	if rPerm2.Code != http.StatusOK {
		t.Fatalf("perm accumulate 2: %d %s", rPerm2.Code, rPerm2.Body.String())
	}
	rPermDeny := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/permissions", testWriteToken, "ops", "", mustJSON(map[string]any{
		"key": "essentials.fly", "value": true, "durationSeconds": 600, "modifier": "deny",
	}))
	assertErrorCode(t, rPermDeny, http.StatusConflict, "temp_entry_exists")

	// 3. Contextual/temporary meta & tempOnly delete
	rMeta := doWriteRequest(t, h, http.MethodPut, "/v1/users/"+uid.String()+"/meta/boost", testWriteToken, "ops", "", mustJSON(map[string]any{
		"value": "2x", "durationSeconds": 3600, "contexts": map[string]string{"server": "survival"},
	}))
	if rMeta.Code != http.StatusOK {
		t.Fatalf("contextual meta: %d %s", rMeta.Code, rMeta.Body.String())
	}
	rMetaDel := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+uid.String()+"/meta/boost?tempOnly=true&server=survival", testWriteToken, "ops", "", nil)
	if rMetaDel.Code != http.StatusOK {
		t.Fatalf("delete temp meta: %d %s", rMetaDel.Code, rMetaDel.Body.String())
	}

	// 4. Affix mode=set replaces previous context prefixes
	_ = doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/prefixes", testWriteToken, "ops", "", mustJSON(map[string]any{
		"priority": 10, "value": "[One] ",
	}))
	rAffixSet := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/prefixes", testWriteToken, "ops", "", mustJSON(map[string]any{
		"priority": 50, "value": "[Only] ", "mode": "set",
	}))
	if rAffixSet.Code != http.StatusOK {
		t.Fatalf("affix set mode: %d %s", rAffixSet.Code, rAffixSet.Body.String())
	}
	if u := mgr.GetUser(uid); len(u.Prefixes) != 1 || u.Prefixes[0].Value != "[Only] " {
		t.Fatalf("expected mode=set to replace global prefixes, got %+v", u.Prefixes)
	}

	// 5. GET /v1/search & POST /v1/bulkupdate
	sr := doRequest(t, h, http.MethodGet, "/v1/search?q=essentials.fly", testToken, nil)
	if sr.Code != http.StatusOK || !strings.Contains(sr.Body.String(), "ParityAPIUser") {
		t.Fatalf("GET /v1/search: %d %s", sr.Code, sr.Body.String())
	}
	bu := doWriteRequest(t, h, http.MethodPost, "/v1/bulkupdate", testWriteToken, "ops", "", mustJSON(map[string]any{
		"scope": "users", "action": "update", "field": "permission", "value": "essentials.fly.v2",
		"constraints": []map[string]string{{"field": "permission", "operator": "==", "pattern": "essentials.fly"}},
	}))
	if bu.Code != http.StatusOK || !mgr.HasPermission(mgr.GetUser(uid), "essentials.fly.v2", nil) {
		t.Fatalf("POST /v1/bulkupdate: %d %s", bu.Code, bu.Body.String())
	}

	// 6. Track set & clear routes on user & group
	ts := doWriteRequest(t, h, http.MethodPost, "/v1/users/"+uid.String()+"/tracks/api_parity_ranks", testWriteToken, "ops", "", mustJSON(map[string]any{
		"group": "mvp",
	}))
	if ts.Code != http.StatusOK {
		t.Fatalf("POST user track: %d %s", ts.Code, ts.Body.String())
	}
	tc := doWriteRequest(t, h, http.MethodDelete, "/v1/users/"+uid.String()+"/tracks/api_parity_ranks", testWriteToken, "ops", "", nil)
	if tc.Code != http.StatusOK {
		t.Fatalf("DELETE user track: %d %s", tc.Code, tc.Body.String())
	}
}
