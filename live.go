package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type event struct {
	name string
	data string
}

type broker struct {
	mu      sync.Mutex
	clients map[chan event]struct{}
}

func newBroker() *broker {
	return &broker{clients: make(map[chan event]struct{})}
}

func (b *broker) subscribe() (<-chan event, func()) {
	ch := make(chan event, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}
}

func (b *broker) send(ev event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- ev:
		default:
			// A slow tab misses this frame; the next change sends another.
		}
	}
}

func (s *previewServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "retry: 1000\n\n")
	flusher.Flush()

	events, unsubscribe := s.broker.subscribe()
	defer unsubscribe()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-events:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *previewServer) watch(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var last os.FileInfo
	if info, err := os.Stat(s.filePath); err == nil {
		last = info
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(s.filePath)
			if err != nil {
				last = nil
				continue
			}
			// Editors save either in place (mtime changes) or via rename
			// (inode changes); checking both catches either.
			if last != nil && os.SameFile(last, info) && last.ModTime().Equal(info.ModTime()) && last.Size() == info.Size() {
				continue
			}
			last = info
			s.broker.send(event{name: "reload", data: "{}"})
		}
	}
}
