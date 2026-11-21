package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultSSEKeepalive = 25 * time.Second

func (s *Server) acquireSSESlot() bool {
	maxClients := s.opts.SSEMaxClients
	if maxClients <= 0 {
		maxClients = defaultSSEMaxClients
	}
	for {
		cur := s.sseClients.Load()
		if int(cur) >= maxClients {
			return false
		}
		if s.sseClients.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.opts.SSEEnabled {
		writeError(w, http.StatusServiceUnavailable, "sse_disabled", "SSE event stream is disabled")
		return
	}
	if !s.acquireSSESlot() {
		writeError(w, http.StatusServiceUnavailable, "sse_capacity_exceeded", "too many concurrent SSE clients")
		return
	}
	defer s.sseClients.Add(-1)

	rc := http.NewResponseController(w)
	// Clear http.Server.WriteTimeout on this streaming response.
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := s.mgr.SubscribeEvents(64)
	defer unsub()

	connectedPayload, err := json.Marshal(struct {
		Revision   uint64 `json:"revision"`
		Generation string `json:"generation"`
	}{
		Revision:   s.mgr.Revision(),
		Generation: s.mgr.Generation(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to initialize event stream")
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connectedPayload); err != nil {
		return
	}
	_ = rc.Flush()

	keepalive := s.sseKeepalive
	if keepalive <= 0 {
		keepalive = defaultSSEKeepalive
	}
	ticker := time.NewTicker(keepalive)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload); err != nil {
				return
			}
			_ = rc.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			_ = rc.Flush()
		}
	}
}
