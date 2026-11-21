package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func newSSETestServer(t *testing.T, enabled bool, maxClients int) (*Server, *manager.Manager) {
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
		Tokens:               []string{testToken},
		WriteEnabled:         true,
		WriteTokens:          []string{testWriteToken},
		RateLimitPerSec:      10000,
		RateLimitBurst:       10000,
		WriteRateLimitPerSec: 10000,
		WriteRateLimitBurst:  10000,
		SSEEnabled:           enabled,
		SSEMaxClients:        maxClients,
	})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return srv, mgr
}

type sseFrame struct {
	event string
	data  string
	raw   string
}

func readSSEFrame(t *testing.T, r *bufio.Reader) sseFrame {
	t.Helper()
	var frame sseFrame
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse line: %v", err)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if frame.event != "" || frame.data != "" || frame.raw != "" {
				return frame
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			frame.raw = trimmed
			continue
		}
		if strings.HasPrefix(trimmed, "event: ") {
			frame.event = strings.TrimPrefix(trimmed, "event: ")
		} else if strings.HasPrefix(trimmed, "data: ") {
			frame.data = strings.TrimPrefix(trimmed, "data: ")
		}
	}
}

func TestSSEDisabledAndCapacityEnforcement(t *testing.T) {
	// 1. Disabled returns 503 sse_disabled.
	disabledSrv, _ := newSSETestServer(t, false, 4)
	if rec := doRequest(t, disabledSrv.Handler(), http.MethodGet, "/v1/events", "", nil); rec.Code != http.StatusServiceUnavailable {
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 when unauthenticated, got %d", rec.Code)
		}
	}
	rec := doRequest(t, disabledSrv.Handler(), http.MethodGet, "/v1/events", testToken, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled sse status = %d, want 503", rec.Code)
	}
	var errBody errorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error != "sse_disabled" {
		t.Fatalf("error code = %q, want sse_disabled", errBody.Error)
	}

	// 2. MaxClients capacity limit enforced and released on disconnect.
	srv, _ := newSSETestServer(t, true, 1)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect sse client 1: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client 1 status = %d, want 200", resp.StatusCode)
	}

	// Second concurrent client must be rejected with 503 sse_capacity_exceeded.
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/events", nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("connect sse client 2: %v", err)
	}
	var capErr errorResponse
	_ = json.NewDecoder(resp2.Body).Decode(&capErr)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable || capErr.Error != "sse_capacity_exceeded" {
		t.Fatalf("client 2 got %d / %+v, want 503 sse_capacity_exceeded", resp2.StatusCode, capErr)
	}

	cancel()
	_ = resp.Body.Close()

	// Wait briefly for server handler defer to release slot, then reconnect.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.sseClients.Load() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	req3, _ := http.NewRequestWithContext(ctx3, http.MethodGet, ts.URL+"/v1/events", nil)
	req3.Header.Set("Authorization", "Bearer "+testToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("reconnect client 3: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("client 3 reconnect status = %d, want 200", resp3.StatusCode)
	}
}

func TestSSEStreamConnectedEventsAndKeepalive(t *testing.T) {
	srv, mgr := newSSETestServer(t, true, 8)
	srv.sseKeepalive = 40 * time.Millisecond
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect sse: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}

	reader := bufio.NewReader(resp.Body)

	// 1. Initial connected event
	connected := readSSEFrame(t, reader)
	if connected.event != "connected" {
		t.Fatalf("first event = %q, want connected", connected.event)
	}
	var connData struct {
		Revision   uint64 `json:"revision"`
		Generation string `json:"generation"`
	}
	if err := json.Unmarshal([]byte(connected.data), &connData); err != nil {
		t.Fatalf("decode connected payload: %v", err)
	}
	if connData.Revision == 0 || connData.Generation != mgr.Generation() {
		t.Fatalf("unexpected connected payload: %+v", connData)
	}

	// 2. Keepalive frame arrives within keepalive window
	ka := readSSEFrame(t, reader)
	if ka.raw != ": keepalive" {
		t.Fatalf("expected keepalive frame, got %+v", ka)
	}

	// 3. Mutations trigger user_updated, group_updated, track_updated events
	id := uuid.New()
	rec := doWriteRequest(t, srv.Handler(), http.MethodPost, "/v1/users/"+id.String()+"/permissions", testWriteToken, "store", "", []byte(`{"key":"sse.test","value":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("user write failed: %d %s", rec.Code, rec.Body.String())
	}

	readEvent := func(wantType string) manager.ChangeEvent {
		for {
			f := readSSEFrame(t, reader)
			if f.event == "" {
				continue // skip keepalives
			}
			var ev manager.ChangeEvent
			if err := json.Unmarshal([]byte(f.data), &ev); err != nil {
				t.Fatalf("decode event data: %v", err)
			}
			if f.event != wantType || ev.Type != wantType {
				t.Fatalf("got event %q/%q, want %q", f.event, ev.Type, wantType)
			}
			return ev
		}
	}

	userEv := readEvent(manager.EventUserUpdated)
	if userEv.UUID != id.String() || userEv.Actor != "store" {
		t.Fatalf("unexpected user_updated event: %+v", userEv)
	}

	if _, err := mgr.CreateGroup("ssegroup", 10, ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	groupEv := readEvent(manager.EventGroupUpdated)
	if groupEv.Group != "ssegroup" {
		t.Fatalf("unexpected group_updated event: %+v", groupEv)
	}

	if _, err := mgr.CreateTrack("ssetrack"); err != nil {
		t.Fatalf("create track: %v", err)
	}
	trackEv := readEvent(manager.EventTrackUpdated)
	if trackEv.Track != "ssetrack" {
		t.Fatalf("unexpected track_updated event: %+v", trackEv)
	}

	if err := mgr.SaveUser(&model.User{UUID: id, Username: "SSEUser", PrimaryGroup: "default"}); err != nil {
		t.Fatalf("save user: %v", err)
	}
	readEvent(manager.EventUserUpdated)
}
