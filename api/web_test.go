package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

const (
	testReadTok  = "read-token-0123456789abcdef"
	testWriteTok = "write-token-0123456789abcdef"
)

func newWebAPITestServer(t *testing.T) (*Server, *manager.Manager) {
	t.Helper()
	store, err := storage.NewJSONStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONStorage: %v", err)
	}
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	t.Cleanup(mgr.Close)

	mgr.SetWebConfig(manager.WebConfig{
		BaseURL:  "http://127.0.0.1:8737",
		LiveSave: true,
	})

	srv, err := New(mgr, Options{
		Listen:               "127.0.0.1:0",
		Tokens:               []string{testReadTok},
		WriteEnabled:         true,
		WriteTokens:          []string{testWriteTok},
		RateLimitPerSec:      -1,
		WriteRateLimitPerSec: -1,
		Logger:               logr.Discard(),
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return srv, mgr
}

func TestEmbeddedWebRoutesAndMIME(t *testing.T) {
	srv, _ := newWebAPITestServer(t)
	h := srv.Handler()

	cases := []struct {
		path     string
		wantType string
	}{
		{"/", "text/html"},
		{"/editor", "text/html"},
		{"/verbose", "text/html"},
		{"/tree", "text/html"},
		{"/wiki", "text/html"},
		{"/styles.css", "text/css"},
		{"/app.js", "application/javascript"},
		{"/demo-data.js", "application/javascript"},
		{"/wiki-data.js", "application/javascript"},
		{"/favicon.svg", "image/svg+xml"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d, want 200", tc.path, rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !bytes.Contains([]byte(ct), []byte(tc.wantType)) {
			t.Fatalf("GET %s Content-Type=%q, want substring %q", tc.path, ct, tc.wantType)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("GET %s should not emit CORS wildcard, got %q", tc.path, got)
		}
	}
}

func TestWebSessionAPIAuthCORSAndSaveFlow(t *testing.T) {
	srv, mgr := newWebAPITestServer(t)
	h := srv.Handler()

	reqNoAuth := httptest.NewRequest(http.MethodPost, "/v1/editor/sessions", bytes.NewBufferString(`{"actor":"Bridge"}`))
	recNoAuth := httptest.NewRecorder()
	h.ServeHTTP(recNoAuth, reqNoAuth)
	if recNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("POST /v1/editor/sessions unauth=%d, want 401", recNoAuth.Code)
	}

	reqReadOnly := httptest.NewRequest(http.MethodPost, "/v1/editor/sessions", bytes.NewBufferString(`{"actor":"Bridge"}`))
	reqReadOnly.Header.Set("Authorization", "Bearer "+testReadTok)
	recReadOnly := httptest.NewRecorder()
	h.ServeHTTP(recReadOnly, reqReadOnly)
	if recReadOnly.Code != http.StatusForbidden {
		t.Fatalf("POST /v1/editor/sessions with read token=%d, want 403", recReadOnly.Code)
	}

	reqGroups := httptest.NewRequest(http.MethodGet, "/v1/groups", nil)
	reqGroups.Header.Set("Authorization", "Bearer "+testReadTok)
	recGroups := httptest.NewRecorder()
	h.ServeHTTP(recGroups, reqGroups)
	if recGroups.Code != http.StatusOK {
		t.Fatalf("GET /v1/groups=%d, want 200", recGroups.Code)
	}
	if got := recGroups.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("GET /v1/groups leaked CORS header %q", got)
	}

	reqCreate := httptest.NewRequest(http.MethodPost, "/v1/editor/sessions", bytes.NewBufferString(`{"actor":"PaperBridge","commandAlias":"spb","extraPermissions":["paper.custom.fly"]}`))
	reqCreate.Header.Set("Authorization", "Bearer "+testWriteTok)
	recCreate := httptest.NewRecorder()
	h.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("POST /v1/editor/sessions=%d body=%s", recCreate.Code, recCreate.Body.String())
	}

	var created manager.EditorSessionCreated
	if err := json.Unmarshal(recCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}

	reqOpt := httptest.NewRequest(http.MethodOptions, "/v1/editor/sessions/"+created.Token+"/save", nil)
	recOpt := httptest.NewRecorder()
	h.ServeHTTP(recOpt, reqOpt)
	if recOpt.Code != http.StatusNoContent || recOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("OPTIONS save CORS failed: status=%d cors=%q", recOpt.Code, recOpt.Header().Get("Access-Control-Allow-Origin"))
	}

	reqFetch := httptest.NewRequest(http.MethodGet, "/v1/editor/sessions/"+created.Token, nil)
	recFetch := httptest.NewRecorder()
	h.ServeHTTP(recFetch, reqFetch)
	if recFetch.Code != http.StatusOK || recFetch.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET session failed: status=%d cors=%q", recFetch.Code, recFetch.Header().Get("Access-Control-Allow-Origin"))
	}

	saveBody, err := json.Marshal(manager.EditorSavePayload{
		Groups: []model.Group{*model.NewGroup("donator", 25)},
	})
	if err != nil {
		t.Fatalf("marshal save: %v", err)
	}
	reqSave := httptest.NewRequest(http.MethodPost, "/v1/editor/sessions/"+created.Token+"/save", bytes.NewReader(saveBody))
	recSave := httptest.NewRecorder()
	h.ServeHTTP(recSave, reqSave)
	if recSave.Code != http.StatusOK || mgr.GetGroup("donator") == nil {
		t.Fatalf("POST save failed: status=%d body=%s", recSave.Code, recSave.Body.String())
	}

	reqTree := httptest.NewRequest(http.MethodPost, "/v1/tree/sessions", bytes.NewBufferString(`{"actor":"Bridge","root":"paper","extraPermissions":["paper.custom.fly"]}`))
	reqTree.Header.Set("Authorization", "Bearer "+testWriteTok)
	recTree := httptest.NewRecorder()
	h.ServeHTTP(recTree, reqTree)
	if recTree.Code != http.StatusCreated {
		t.Fatalf("POST /v1/tree/sessions=%d", recTree.Code)
	}
	var treeCreated manager.TreeSessionCreated
	if err := json.Unmarshal(recTree.Body.Bytes(), &treeCreated); err != nil {
		t.Fatalf("unmarshal tree: %v", err)
	}
	reqTreeGet := httptest.NewRequest(http.MethodGet, "/v1/tree/sessions/"+treeCreated.Token, nil)
	recTreeGet := httptest.NewRecorder()
	h.ServeHTTP(recTreeGet, reqTreeGet)
	if recTreeGet.Code != http.StatusOK || !bytes.Contains(recTreeGet.Body.Bytes(), []byte("paper.custom.fly")) {
		t.Fatalf("GET tree failed: status=%d body=%s", recTreeGet.Code, recTreeGet.Body.String())
	}
}
