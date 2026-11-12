package ws

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/shell"
)

type instructionRunner struct {
	runs chan agent.RunOptions
	gate chan error
}

func (r instructionRunner) Name() string { return "instruction-test" }
func (r instructionRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	r.runs <- opts
	if opts.Prompt == "block" {
		select {
		case err := <-r.gate:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cb(agent.Event{Type: agent.EventSessionInit, SessionID: "saved-cli-id"})
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func installInstructionRunner() instructionRunner {
	r := instructionRunner{runs: make(chan agent.RunOptions, 16), gate: make(chan error, 1)}
	agent.Register("instruction-test", func() agent.Runner { return r })
	return r
}

func instructionRun(t *testing.T, r instructionRunner, prompt, instruction string) agent.RunOptions {
	t.Helper()
	select {
	case got := <-r.runs:
		if got.Prompt != prompt || got.SystemInstruction != instruction {
			t.Errorf("run prompt=%q instruction=%q; want separate prompt=%q instruction=%q", got.Prompt, got.SystemInstruction, prompt, instruction)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
		return agent.RunOptions{}
	}
}

func instructedSession(t *testing.T, database *db.DB, workdir, instruction string) *db.Session {
	t.Helper()
	s, err := database.CreateSessionWithInstruction("role", "", workdir, "default", "instruction-test", nil, "agent", "", instruction)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionInstructionRunLifecycle(t *testing.T) {
	database, url := startTestServer(t)
	r := installInstructionRunner()
	const instruction = "回答使用繁體中文\n路徑 C:\\work"
	s := instructedSession(t, database, t.TempDir(), instruction)
	c := dialClient(t, url(s.ID))
	c.send(t, map[string]any{"type": "input", "data": "block", "system_instruction": "forged"})
	instructionRun(t, r, "block", instruction)
	c.send(t, map[string]any{"type": "input", "data": "queued"})
	waitFor(t, "queued input", func() bool { return queueLen(database, s.ID) == 1 })
	r.gate <- errors.New("retryable failure")
	waitFor(t, "paused after failure", func() bool {
		paused, _ := database.QueuePaused(s.ID)
		return paused && !taskIsActive(s.ID)
	})
	c.conn.Close()
	c = dialClient(t, url(s.ID))
	c.send(t, map[string]any{"type": "queue_resume"})
	instructionRun(t, r, "queued", instruction)
	waitFor(t, "queue completed", func() bool { return !taskIsActive(s.ID) && lastReply(database, s.ID) == "ok:queued" })
	c.send(t, map[string]any{"type": "input", "data": "next"})
	if got := instructionRun(t, r, "next", instruction); got.SessionID != "saved-cli-id" {
		t.Errorf("resume ID = %q", got.SessionID)
	}
	waitFor(t, "next completed", func() bool { return !taskIsActive(s.ID) })
	c.send(t, map[string]any{"type": "reset_context"})
	c.barrier(t)
	c.send(t, map[string]any{"type": "input", "data": "fresh"})
	if got := instructionRun(t, r, "fresh", instruction); got.SessionID != "" {
		t.Errorf("reset CLI ID = %q", got.SessionID)
	}
	waitFor(t, "fresh completed", func() bool { return !taskIsActive(s.ID) })
	stored, err := database.GetSession(s.ID)
	if err != nil || stored.SystemInstruction != instruction {
		t.Fatalf("stored instruction changed: %+v, %v", stored, err)
	}
	if got := userMessages(database, s.ID); !reflect.DeepEqual(got, []string{"fresh"}) {
		t.Errorf("instruction entered history: %q", got)
	}
}

func TestSessionInstructionIsolation(t *testing.T) {
	database, url := startTestServer(t)
	r := installInstructionRunner()
	dir := t.TempDir()
	for _, instruction := range []string{"role A", "role B"} {
		s := instructedSession(t, database, dir, instruction)
		c := dialClient(t, url(s.ID))
		c.send(t, map[string]any{"type": "input", "data": "hello"})
		got := instructionRun(t, r, "hello", instruction)
		if got.WorkDir != dir {
			t.Errorf("workdir=%q", got.WorkDir)
		}
		waitFor(t, "session complete", func() bool { return !taskIsActive(s.ID) })
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("shared workspace was modified: %v, %v", entries, err)
	}
}

func TestSessionInstructionShellDoesNotConsume(t *testing.T) {
	database, url := startAdmServer(t, ShellOpts{Enabled: true})
	r := installInstructionRunner()
	s := instructedSession(t, database, t.TempDir(), "private role")
	if err := database.UpdatePermissionMode(s.ID, "bypassPermissions"); err != nil {
		t.Fatal(err)
	}
	c := dialClient(t, url(s.ID))
	commands := make(chan shell.RunOptions, 1)
	previous := runShellCommand
	runShellCommand = func(_ context.Context, opts shell.RunOptions, cb func(shell.Event)) error {
		commands <- opts
		cb(shell.Event{Type: shell.EventDone})
		return nil
	}
	t.Cleanup(func() { runShellCommand = previous })
	c.send(t, map[string]any{"type": "set_input_mode", "mode": "shell"})
	c.barrier(t)
	c.send(t, map[string]any{"type": "shell_run", "data": "echo hello"})
	select {
	case got := <-commands:
		if got.Command != "echo hello" || got.WorkDir != s.WorkDir {
			t.Errorf("shell received altered command/options: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not start")
	}
	waitFor(t, "shell completed", func() bool { return !shellTaskActive(s.ID) })
	c.send(t, map[string]any{"type": "set_input_mode", "mode": "agent"})
	c.barrier(t)
	c.send(t, map[string]any{"type": "input", "data": "hello again"})
	instructionRun(t, r, "hello again", "private role")
	waitFor(t, "agent completed", func() bool { return !taskIsActive(s.ID) })
}

func TestHandoffPreservesInstruction(t *testing.T) {
	database, _ := startTestServer(t)
	captureStarter(t, nil)
	old := instructedSession(t, database, t.TempDir(), "private handoff role")
	next, err := startHandoff(database, old, "fix it", errors.New("handoff"))
	if err != nil {
		t.Fatal(err)
	}
	if next.SystemInstruction != old.SystemInstruction {
		t.Errorf("automatic handoff instruction=%q", next.SystemInstruction)
	}
	info := newManualHandoffInfo(old, "fix it", errors.New("handoff"))
	// Use the wire representation to assert the owner prefill contract.
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil || got["system_instruction"] != old.SystemInstruction {
		t.Errorf("manual handoff lost instruction: %s, %v", b, err)
	}
}

func TestSessionInstructionAntigravityPermission(t *testing.T) {
	agent.Register(agent.TypeAntigravity, func() agent.Runner { return fakeKiroACP{} })
	t.Cleanup(func() { agent.Register(agent.TypeAntigravity, func() agent.Runner { return fakeRunner{} }) })
	for _, decision := range []string{"allow_once", "deny_once", "interrupt"} {
		t.Run(decision, func(t *testing.T) {
			database, url := startTestServer(t)
			s, err := database.CreateSessionWithInstruction("agy", "", t.TempDir(), "default", agent.TypeAntigravity, nil, "agent", "", "private role")
			if err != nil {
				t.Fatal(err)
			}
			c := dialClient(t, url(s.ID))
			c.send(t, map[string]any{"type": "input", "data": "go"})
			waitFor(t, "Antigravity permission callback", func() bool { return awaitingConfirm(database, s.ID) })
			// Another connection uses the same existing permission decision path.
			c.conn.Close()
			c = dialClient(t, url(s.ID))
			c.await(t, "restored permission", isType("permission_request"))
			c.send(t, map[string]any{"type": decision})
			waitFor(t, "permission resolved", func() bool {
				_, pending := permPending(s.ID)
				return !pending && !taskIsActive(s.ID)
			})
			if decision != "interrupt" {
				want := "denied"
				if decision == "allow_once" {
					want = "allowed"
				}
				if got := lastReply(database, s.ID); got != want {
					t.Errorf("decision result=%q want %q", got, want)
				}
			}
		})
	}
}

func TestHandoffInstructionOwnerOnly(t *testing.T) {
	database, url := startShareServer(t)
	s := instructedSession(t, database, t.TempDir(), "private role")
	_, ga := newShare(t, database, s.ID, db.ShareRoleViewer, db.ShareModeLive, "guest", time.Hour)
	owner := dialClient(t, url(s.ID, ""))
	guest := dialClient(t, url(s.ID, ga.Guest.Token))
	owner.barrier(t)
	guest.barrier(t)
	info := newManualHandoffInfo(s, "fix", errors.New("handoff"))
	hub.Broadcast(s.ID, serverMsg{Type: "handoff_available", Handoff: &info})
	for _, tc := range []struct {
		client *wsClient
		owner  bool
	}{{owner, true}, {guest, false}} {
		msg := tc.client.await(t, "handoff", isType("handoff_available"))
		h := msg["handoff"].(map[string]any)
		value, present := h["system_instruction"]
		if tc.owner && value != s.SystemInstruction {
			t.Errorf("owner prefill=%v", value)
		}
		if !tc.owner && present {
			t.Errorf("guest instruction leak: %v", value)
		}
	}
	// Guest filtering must never mutate the shared broadcast or owner source.
	b, _ := json.Marshal(info)
	if !strings.Contains(string(b), "private role") {
		t.Fatal("guest filtering mutated shared handoff")
	}
}
