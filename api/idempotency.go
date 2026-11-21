package api

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxIdempotencyEntries   = 4096
	idempotencyTTL          = 15 * time.Minute
	maxIdempotencyKeyLength = 128
)

type idempEntry struct {
	key       string
	bodyHash  [sha256.Size]byte
	status    int
	headers   http.Header
	body      []byte
	expiresAt time.Time
	inFlight  bool
	done      chan struct{}
	elem      *list.Element
}

type idempotencyCache struct {
	mu         sync.Mutex
	entries    map[string]*idempEntry
	order      *list.List
	maxEntries int
	ttl        time.Duration
	nowFn      func() time.Time
}

func newIdempotencyCache(maxEntries int, ttl time.Duration) *idempotencyCache {
	if maxEntries <= 0 {
		maxEntries = maxIdempotencyEntries
	}
	if ttl <= 0 {
		ttl = idempotencyTTL
	}
	return &idempotencyCache{
		entries:    make(map[string]*idempEntry),
		order:      list.New(),
		maxEntries: maxEntries,
		ttl:        ttl,
		nowFn:      time.Now,
	}
}

func validateIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > maxIdempotencyKeyLength || strings.TrimSpace(key) == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func (c *idempotencyCache) acquire(cacheKey string, bodyHash [sha256.Size]byte) (cached *idempEntry, conflict bool, waitCh <-chan struct{}, acquired *idempEntry) {
	now := c.nowFn()
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredLocked(now)

	if entry, ok := c.entries[cacheKey]; ok {
		if entry.inFlight {
			if entry.bodyHash != bodyHash {
				return nil, true, nil, nil
			}
			return nil, false, entry.done, nil
		}
		if entry.bodyHash != bodyHash {
			return nil, true, nil, nil
		}
		return entry, false, nil, nil
	}

	for len(c.entries) >= c.maxEntries {
		if !c.evictOldestCompletedLocked() {
			break
		}
	}

	entry := &idempEntry{
		key:      cacheKey,
		bodyHash: bodyHash,
		inFlight: true,
		done:     make(chan struct{}),
	}
	entry.elem = c.order.PushBack(entry)
	c.entries[cacheKey] = entry
	return nil, false, nil, entry
}

func (c *idempotencyCache) complete(entry *idempEntry, status int, headers http.Header, body []byte) {
	now := c.nowFn()
	c.mu.Lock()
	defer c.mu.Unlock()

	if current, ok := c.entries[entry.key]; ok && current == entry {
		entry.status = status
		entry.headers = cloneHeader(headers)
		entry.body = append([]byte(nil), body...)
		entry.expiresAt = now.Add(c.ttl)
		entry.inFlight = false
		c.order.MoveToBack(entry.elem)
	}
	close(entry.done)
}

func (c *idempotencyCache) abort(entry *idempEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if current, ok := c.entries[entry.key]; ok && current == entry {
		delete(c.entries, entry.key)
		c.order.Remove(entry.elem)
	}
	close(entry.done)
}

func (c *idempotencyCache) evictExpiredLocked(now time.Time) {
	for elem := c.order.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(*idempEntry)
		if !entry.inFlight && now.After(entry.expiresAt) {
			delete(c.entries, entry.key)
			c.order.Remove(elem)
		}
		elem = next
	}
}

func (c *idempotencyCache) evictOldestCompletedLocked() bool {
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(*idempEntry)
		if !entry.inFlight {
			delete(c.entries, entry.key)
			c.order.Remove(elem)
			return true
		}
	}
	return false
}

type responseCapture struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func newResponseCapture() *responseCapture {
	return &responseCapture{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (r *responseCapture) Header() http.Header {
	return r.header
}

func (r *responseCapture) WriteHeader(code int) {
	r.status = code
}

func (r *responseCapture) Write(p []byte) (int, error) {
	return r.buf.Write(p)
}

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, vals := range src {
		dst[k] = append([]string(nil), vals...)
	}
	return dst
}

func writeCapturedResponse(w http.ResponseWriter, status int, headers http.Header, body []byte, replayed bool) {
	for k, vals := range headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	if replayed {
		w.Header().Set("X-Idempotency-Replayed", "true")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) withIdempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Idempotency-Key")) == 0 || s.idemp == nil {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if !validateIdempotencyKey(key) {
			writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key must be 1-128 printable ASCII characters")
			return
		}

		rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "request body is invalid or exceeds size limit")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(rawBody))

		bodyHash := sha256.Sum256(rawBody)
		cacheKey := r.Method + " " + r.URL.Path + "\x00" + key

		for {
			cached, conflict, waitCh, acquired := s.idemp.acquire(cacheKey, bodyHash)
			if conflict {
				writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was reused with a different request body")
				return
			}
			if cached != nil {
				writeCapturedResponse(w, cached.status, cached.headers, cached.body, true)
				return
			}
			if waitCh != nil {
				select {
				case <-waitCh:
					continue
				case <-r.Context().Done():
					return
				}
			}

			rec := newResponseCapture()
			completed := false
			defer func() {
				if !completed {
					s.idemp.abort(acquired)
				}
			}()

			next.ServeHTTP(rec, r)

			bodyBytes := rec.buf.Bytes()
			if rec.status < http.StatusInternalServerError {
				s.idemp.complete(acquired, rec.status, rec.header, bodyBytes)
			} else {
				s.idemp.abort(acquired)
			}
			completed = true

			writeCapturedResponse(w, rec.status, rec.header, bodyBytes, false)
			return
		}
	})
}
