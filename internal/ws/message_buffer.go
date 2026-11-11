package ws

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ponytail: keep at most 100 ms / 64 KiB of deltas in memory. A hard process
// crash can lose that tail; chunk storage is needed if every delta must be durable.
type messageBuffer struct {
	mu      sync.Mutex
	pending strings.Builder
	timer   *time.Timer
	write   func(string) error
	send    func(serverMsg)
	events  []serverMsg
}

func (b *messageBuffer) Append(text string, events ...serverMsg) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending.WriteString(text)
	for _, event := range events {
		n := len(b.events)
		if n > 0 && (event.Type == "delta" || (event.Type == "shell_delta" && event.Stream != "stderr")) && b.events[n-1].Type == event.Type && b.events[n-1].Stream == event.Stream {
			b.events[n-1].Content += event.Content
		} else {
			b.events = append(b.events, event)
		}
	}
	// Bound shell bursts while preserving every stderr marker and stream order.
	if len(b.events) >= 32 {
		return b.flush()
	}
	if b.pending.Len() >= 64<<10 {
		return b.flush()
	}
	if b.timer == nil && b.pending.Len() > 0 {
		b.timer = time.AfterFunc(100*time.Millisecond, func() {
			if err := b.Flush(); err != nil {
				slog.Error("flush streamed message", "err", err)
			}
		})
	}
	return nil
}

func (b *messageBuffer) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flush()
}

func (b *messageBuffer) flush() error {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if b.pending.Len() == 0 {
		return nil
	}
	if err := b.write(b.pending.String()); err != nil {
		return err
	}
	b.pending.Reset()
	// Persist before publishing, so reconnecting clients cannot miss a buffered tail.
	for _, event := range b.events {
		b.send(event)
	}
	b.events = nil
	return nil
}
