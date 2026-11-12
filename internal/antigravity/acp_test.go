package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func fakeACPProcess(mode string) {
	if strings.HasPrefix(mode, "session") {
		fakeACPSession(mode)
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "acp" {
		os.Exit(2)
	}
	// A full stderr pipe must never obstruct JSON-RPC responses.
	fmt.Fprint(os.Stderr, strings.Repeat("private diagnostic ", 10000))
	sc := bufio.NewScanner(os.Stdin)
	var firstID int64
	for sc.Scan() {
		var msg struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			os.Exit(3)
		}
		switch mode {
		case "paired":
			if firstID == 0 {
				firstID = msg.ID
				continue
			}
			fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%d}\n", msg.ID, msg.ID)
			fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%d}\n", firstID, firstID)
		case "response":
			fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"ok\":true}}\n", msg.ID)
		case "error":
			fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32603,\"message\":\"PRIVATE_INSTRUCTION\",\"data\":\"PRIVATE_INSTRUCTION\"}}\n", msg.ID)
		case "eof":
			return
		case "malformed":
			fmt.Fprintln(os.Stdout, "{PRIVATE_INSTRUCTION")
		case "timeout", "cancel":
			time.Sleep(time.Minute)
		}
	}
}

func TestACPRequestLifecycle(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"response", "error", "eof", "timeout", "cancel", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CC_AGY_BIN", bin)
			t.Setenv("FAKE_AGY_ACP", mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pid := 0
			c, err := startACP(ctx, agent.RunOptions{OnStart: func(n int) { pid = n }})
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			if pid <= 0 {
				t.Fatal("OnStart did not receive child PID")
			}
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 150 * time.Millisecond
			}
			if mode == "cancel" {
				time.AfterFunc(150*time.Millisecond, cancel)
			}
			began := time.Now()
			result, err := c.request(ctx, "initialize", map[string]string{"private": "PRIVATE_INSTRUCTION"}, timeout)
			if time.Since(began) > 3*time.Second {
				t.Fatal("request was not released promptly")
			}
			if mode == "response" {
				if err != nil || string(result) != `{"ok":true}` {
					t.Fatalf("result=%s err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("expected observable transport error")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_INSTRUCTION") {
				t.Fatalf("error exposed RPC payload: %v", err)
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline error, got %v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation error, got %v", err)
			}
			if mode == "eof" && !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF, got %v", err)
			}
			c.mu.Lock()
			pending := len(c.pending)
			c.mu.Unlock()
			if pending != 0 {
				t.Fatalf("request left %d pending entries", pending)
			}
			if err := c.close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-c.waitDone:
			default:
				t.Fatal("child process was not reaped")
			}
		})
	}
}

func TestACPRequestPairing(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_AGY_BIN", bin)
	t.Setenv("FAKE_AGY_ACP", "paired")
	c, err := startACP(context.Background(), agent.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	// The server reverses response order. Each waiter must receive its own ID.
	first := make(chan acpReply, 1)
	go func() {
		result, err := c.request(context.Background(), "first", nil, 5*time.Second)
		first <- acpReply{result: result, err: err}
	}()
	deadline := time.Now().Add(time.Second)
	for c.nextID.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second, err := c.request(context.Background(), "second", nil, 5*time.Second)
	if err != nil || string(second) != "2" {
		t.Fatalf("second response=%s err=%v", second, err)
	}
	got := <-first
	if got.err != nil || string(got.result) != "1" {
		t.Fatalf("first response=%s err=%v", got.result, got.err)
	}
}
