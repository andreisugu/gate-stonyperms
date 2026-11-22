package api

import (
	"net/http"
	"testing"
)

func TestBackendCatalogEndpoint(t *testing.T) {
	srv, mgr := newTestServer(t)
	body := []byte(`{"server":"survival","permissions":["essentials.home.set","jobs.use"],"contexts":{"world":["world_nether"],"gamemode":["creative"]}}`)
	rec := doRequest(t, srv.Handler(), http.MethodPost, "/v1/backends/catalog", testToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status = %d, body = %s", rec.Code, rec.Body.String())
	}
	perms, contexts := mgr.BackendCatalogSnapshot()
	if len(perms) != 2 || perms[0] != "essentials.home.set" || perms[1] != "jobs.use" {
		t.Fatalf("unexpected catalog permissions: %v", perms)
	}
	if len(contexts["world"]) != 1 || contexts["world"][0] != "world_nether" {
		t.Fatalf("unexpected catalog contexts: %v", contexts)
	}

	rec = doRequest(t, srv.Handler(), http.MethodPost, "/v1/backends/catalog", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated catalog status = %d", rec.Code)
	}
}
