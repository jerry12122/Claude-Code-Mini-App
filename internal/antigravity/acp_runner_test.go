package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestInstructionTransportLogsRedacted(t *testing.T) {
	setupACPRunnerTest(t, "session-rpcerror")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	err := (&Runner{}).Run(context.Background(), agent.RunOptions{SystemInstruction: "PRIVATE_INSTRUCTION", Prompt: "ordinary user"}, func(e agent.Event) {
		if e.Err != nil && strings.Contains(e.Err.Error(), "PRIVATE_INSTRUCTION") {
			t.Errorf("event error exposed instruction: %v", e.Err)
		}
	})
	if err == nil {
		t.Fatal("expected RPC error")
	}
	if strings.Contains(logs.String(), "PRIVATE_INSTRUCTION") || strings.Contains(err.Error(), "PRIVATE_INSTRUCTION") {
		t.Fatalf("transport exposed instruction: logs=%s err=%v", logs.String(), err)
	}
}

func setupACPRunnerTest(t *testing.T, mode string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "rpc.jsonl")
	t.Setenv("CC_AGY_BIN", bin)
	t.Setenv("FAKE_AGY_ACP", mode)
	t.Setenv("FAKE_AGY_ACP_LOG", log)
	return log
}

func TestInstructionTransportSelection(t *testing.T) {
	t.Run("instruction before print fallback", func(t *testing.T) {
		log := setupACPRunnerTest(t, "session")
		t.Setenv("CC_AGY_FORCE_PRINT_TEXT", "1")
		if err := (&Runner{}).Run(context.Background(), agent.RunOptions{SystemInstruction: "private", Prompt: "user"}, func(agent.Event) {}); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, req := range readACPSessionLog(t, log) {
			if req.Method == "session/prompt" {
				found = true
			}
		}
		if !found {
			t.Fatal("instruction did not use ACP")
		}
	})
	for _, mode := range []string{"text", "stream"} {
		t.Run("empty instruction "+mode, func(t *testing.T) {
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CC_AGY_BIN", bin)
			t.Setenv("FAKE_AGY_PRINT", mode)
			force := "0"
			if mode == "text" {
				force = "1"
			}
			t.Setenv("CC_AGY_FORCE_PRINT_TEXT", force)
			done := 0
			err = (&Runner{}).Run(context.Background(), agent.RunOptions{Prompt: "user"}, func(e agent.Event) {
				if e.Type == agent.EventDone {
					done++
				}
			})
			if err != nil || done != 1 {
				t.Fatalf("legacy path err=%v done=%d", err, done)
			}
		})
	}
}

func TestACPReplayAndTerminalEvents(t *testing.T) {
	for _, mode := range []string{"session-events", "session-rpcerror", "session-eof", "session-unknown", "session-nonzero"} {
		t.Run(mode, func(t *testing.T) {
			log := setupACPRunnerTest(t, mode)
			var events []agent.Event
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := (&Runner{}).Run(ctx, agent.RunOptions{SystemInstruction: "private", Prompt: "user", SessionID: "new-id"}, func(e agent.Event) { events = append(events, e) })
			var done, failed, started, completed int
			var text, thought string
			var activity []string
			for _, e := range events {
				switch e.Type {
				case agent.EventDone:
					done++
					if e.SessionID != "new-id" || e.ResultText != "new answer" {
						t.Errorf("invalid done: %+v", e)
					}
				case agent.EventError:
					failed++
				case agent.EventDelta:
					text += e.Text
				case agent.EventThinking:
					thought = e.Text
				case agent.EventActivity:
					activity = append(activity, e.Text)
				case agent.EventToolStarted:
					started++
					if e.Tool.CallID != "tool-1" || e.Tool.Name != "Read file" {
						t.Errorf("invalid started tool: %+v", e.Tool)
					}
				case agent.EventToolCompleted:
					completed++
					if e.Tool.CallID != "tool-1" || e.Tool.Name != "Read file" || !e.Tool.OK || e.Tool.Output != "file contents" {
						t.Errorf("invalid completed tool: %+v", e.Tool)
					}
				}
			}
			if mode == "session-events" {
				if !slices.Equal(activity, []string{"思考中", "思考中", "執行工具：Read file"}) {
					t.Errorf("activity events = %q", activity)
				}
				if err != nil || done != 1 || failed != 0 || text != "new answer" || thought != "think more" || started != 1 || completed != 1 {
					t.Fatalf("err=%v done=%d errors=%d text=%q thought=%q tools=%d/%d", err, done, failed, text, thought, started, completed)
				}
			} else if err == nil || done != 0 || failed != 1 {
				t.Fatalf("failed run err=%v done=%d errors=%d", err, done, failed)
			}
			if mode == "session-unknown" {
				found := false
				for _, req := range readACPSessionLog(t, log) {
					if req.ID == "question-1" && req.Method == "" {
						found = true
					}
				}
				if !found {
					t.Fatal("unsupported blocking request was not answered")
				}
			}
		})
	}
}

func TestACPPermissionRequest(t *testing.T) {
	for _, selection := range []string{"allow-one", "invalid", "nil"} {
		t.Run(selection, func(t *testing.T) {
			log := setupACPRunnerTest(t, "session-permission")
			opts := agent.RunOptions{SystemInstruction: "private", Prompt: "user"}
			called := false
			if selection != "nil" {
				opts.RequestPermission = func(ctx context.Context, req agent.PermissionRequest) string {
					called = true
					if req.ToolCallID != "tool-1" || len(req.Options) != 2 {
						t.Errorf("invalid permission request %+v", req)
					}
					return selection
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := (&Runner{}).Run(ctx, opts, func(agent.Event) {}); err != nil {
				t.Fatal(err)
			}
			if called != (selection != "nil") {
				t.Fatal("permission callback not used")
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var outcome map[string]any
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var msg struct {
					ID     any `json:"id"`
					Result struct {
						Outcome map[string]any `json:"outcome"`
					} `json:"result"`
				}
				if json.Unmarshal([]byte(line), &msg) == nil && msg.ID == "permission-1" {
					outcome = msg.Result.Outcome
				}
			}
			if selection == "allow-one" {
				if outcome["outcome"] != "selected" || outcome["optionId"] != selection {
					t.Fatalf("wrong selection %#v", outcome)
				}
			} else if outcome["outcome"] != "cancelled" {
				t.Fatalf("unsafe permission outcome %#v", outcome)
			}
		})
	}
}

func TestACPCancellation(t *testing.T) {
	log := setupACPRunnerTest(t, "session-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := 0
	began := time.Now()
	err := (&Runner{}).Run(ctx, agent.RunOptions{SystemInstruction: "private", Prompt: "user"}, func(e agent.Event) {
		if e.Type == agent.EventDelta {
			cancel()
		}
		if e.Type == agent.EventDone {
			done++
		}
	})
	if !errors.Is(err, context.Canceled) || done != 0 || time.Since(began) > 3*time.Second {
		t.Fatalf("cancel err=%v done=%d elapsed=%s", err, done, time.Since(began))
	}
	found := false
	for _, req := range readACPSessionLog(t, log) {
		if req.Method == "session/cancel" && req.Params["sessionId"] == "new-id" {
			found = true
		}
	}
	if !found {
		t.Fatal("session/cancel notification not sent before cleanup")
	}
}
