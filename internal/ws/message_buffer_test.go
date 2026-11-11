package ws

import (
	"errors"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"strings"
	"testing"
	"time"
)

func TestMessageBuffer(t *testing.T) {
	var saved strings.Builder
	writes := 0
	b := &messageBuffer{write: func(s string) error { writes++; saved.WriteString(s); return nil }}
	defer b.Flush()
	chunk := strings.Repeat("x", 256)
	for i := 0; i < 4000; i++ {
		if err := b.Append(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if saved.String() != strings.Repeat(chunk, 4000) || writes > 20 {
		t.Fatalf("length=%d writes=%d", saved.Len(), writes)
	}
	t.Logf("4000 deltas persisted in %d writes", writes)

	flushed := make(chan string, 1)
	idle := &messageBuffer{write: func(s string) error { flushed <- s; return nil }}
	defer idle.Flush()
	idle.Append("tail")
	select {
	case got := <-flushed:
		if got != "tail" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("idle tail not persisted")
	}

	retry := &messageBuffer{write: func(string) error { return errors.New("disk unavailable") }}
	retry.Append("keep me")
	if err := retry.Flush(); err == nil {
		t.Fatal("write error hidden")
	}
	retry.mu.Lock()
	retry.write = func(s string) error {
		if s != "keep me" {
			t.Fatalf("retry lost content: %q", s)
		}
		return nil
	}
	retry.mu.Unlock()
	if err := retry.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestMessageBufferPublishesAfterPersistence(t *testing.T) {
	persisted := ""
	var sent []string
	b := &messageBuffer{write: func(s string) error { persisted += s; return nil }, send: func(msg serverMsg) {
		if persisted != "firstsecond" {
			t.Errorf("published before persistence: %q", persisted)
		}
		sent = append(sent, msg.Content)
	}}
	defer b.Flush()
	b.Append("first", serverMsg{Type: "delta", Content: "first"})
	b.Append("second", serverMsg{Type: "delta", Content: "second"})
	if len(sent) != 0 {
		t.Fatal("published before flush")
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sent, "") != "firstsecond" {
		t.Fatalf("sent=%v", sent)
	}
}

func BenchmarkMessagePersistence(b *testing.B) {
	for _, buffered := range []bool{false, true} {
		name := "direct"
		if buffered {
			name = "buffered"
		}
		b.Run(name, func(b *testing.B) {
			database, err := db.Open(":memory:")
			if err != nil {
				b.Fatal(err)
			}
			defer database.Close()
			chunk := strings.Repeat("x", 256)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				id, err := database.CreatePendingMessage("s")
				if err != nil {
					b.Fatal(err)
				}
				write := func(s string) error { return database.AppendMessageContent(id, s) }
				buffer := &messageBuffer{write: write}
				for j := 0; j < 4000; j++ {
					var err error
					if buffered {
						err = buffer.Append(chunk)
					} else {
						err = write(chunk)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				if err := buffer.Flush(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestMessageBufferCoalescesSocketPackets(t *testing.T) {
	var persisted, received strings.Builder
	packets := 0
	b := &messageBuffer{write: func(s string) error { persisted.WriteString(s); return nil }, send: func(m serverMsg) { packets++; received.WriteString(m.Content) }}
	chunk := strings.Repeat("x", 256)
	for i := 0; i < 4000; i++ {
		if err := b.Append(chunk, serverMsg{Type: "delta", Content: chunk}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if packets >= 128 || received.String() != persisted.String() {
		t.Fatalf("packets=%d received=%d saved=%d", packets, received.Len(), persisted.Len())
	}
	t.Logf("4000 deltas => %d packets (queue capacity 128)", packets)
	var shell []serverMsg
	b = &messageBuffer{write: func(string) error { return nil }, send: func(m serverMsg) { shell = append(shell, m) }}
	b.Append("[stderr] a\n", serverMsg{Type: "shell_delta", Stream: "stderr", Content: "a\n"})
	b.Append("[stderr] b\n", serverMsg{Type: "shell_delta", Stream: "stderr", Content: "b\n"})
	b.Flush()
	if len(shell) != 2 || shell[1].Stream != "stderr" {
		t.Fatal("stderr formatting changed")
	}
}
