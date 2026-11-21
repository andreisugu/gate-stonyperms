package manager

import "time"

const (
	EventUserUpdated   = "user_updated"
	EventUserDeleted   = "user_deleted"
	EventGroupUpdated  = "group_updated"
	EventGroupDeleted  = "group_deleted"
	EventTrackUpdated  = "track_updated"
	EventTrackDeleted  = "track_deleted"
	EventReload        = "reload"
	EventExpiryCleanup = "expiry_cleanup"
)

// ChangeEvent describes a state transition emitted to SSE and internal subscribers.
type ChangeEvent struct {
	Revision  uint64 `json:"revision"`
	Type      string `json:"type"`
	UUID      string `json:"uuid,omitempty"`
	Username  string `json:"username,omitempty"`
	Group     string `json:"group,omitempty"`
	Track     string `json:"track,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

type eventSubscriber struct {
	ch     chan ChangeEvent
	closed bool
}

// SubscribeEvents registers a buffered subscriber for live manager mutations.
// Slow subscribers drop overflow events rather than blocking mutations.
// Call the returned cancel function to unsubscribe and close the channel.
func (m *Manager) SubscribeEvents(buffer int) (<-chan ChangeEvent, func()) {
	if buffer <= 0 {
		buffer = 16
	}

	m.subMu.Lock()
	if m.subClosed {
		m.subMu.Unlock()
		ch := make(chan ChangeEvent)
		close(ch)
		return ch, func() {}
	}
	if m.subscribers == nil {
		m.subscribers = make(map[uint64]*eventSubscriber)
	}
	m.nextSubID++
	id := m.nextSubID
	sub := &eventSubscriber{ch: make(chan ChangeEvent, buffer)}
	m.subscribers[id] = sub
	m.subMu.Unlock()

	return sub.ch, func() {
		m.subMu.Lock()
		if !sub.closed {
			sub.closed = true
			delete(m.subscribers, id)
			close(sub.ch)
		}
		m.subMu.Unlock()
	}
}

func (m *Manager) notifyEvent(ev ChangeEvent) {
	if ev.Timestamp == 0 {
		ev.Timestamp = time.Now().UTC().Unix()
	}
	m.notifyUpdate()

	m.subMu.Lock()
	for _, sub := range m.subscribers {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
		}
	}
	m.subMu.Unlock()
}

func (m *Manager) closeSubscribers() {
	m.subMu.Lock()
	m.subClosed = true
	for id, sub := range m.subscribers {
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		delete(m.subscribers, id)
	}
	m.subMu.Unlock()
}
