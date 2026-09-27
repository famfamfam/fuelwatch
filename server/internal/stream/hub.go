// Package stream — SSE-hub: события для панели в реальном времени (docs/07-api.md §3).
package stream

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	pingInterval = 25 * time.Second
	subBuffer    = 64
)

type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func New() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Publish рассылает событие всем подписчикам. Медленный подписчик отключается,
// браузер переподключится сам.
func (h *Hub) Publish(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Error("stream marshal", "event", event, "err", err)
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(b) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- msg:
		default:
			delete(h.subs, c)
			close(c)
		}
	}
}

func (h *Hub) remove(c chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[c]; ok {
		delete(h.subs, c)
		close(c)
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	c := make(chan []byte, subBuffer)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	defer h.remove(c)

	if _, err := w.Write([]byte("retry: 3000\n\n")); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		var msg []byte
		select {
		case <-r.Context().Done():
			return
		case m, ok := <-c:
			if !ok {
				return
			}
			msg = m
		case <-ping.C:
			msg = []byte(": ping\n\n")
		}
		if _, err := w.Write(msg); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
