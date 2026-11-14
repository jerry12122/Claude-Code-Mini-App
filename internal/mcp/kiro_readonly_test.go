package mcp

import (
	"context"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestLegacyKiroToolsReadOnly(t *testing.T) {
	database := setupTestDB(t)
	s, err := database.CreateSession("legacy", "", t.TempDir(), "default", agent.TypeKiro, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	d := &deps{db: database} // 不提供 Registry：所有寫入都必須在連線前拒絕。
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"create", func() error {
			_, _, err := d.createSession(ctx, nil, createSessionIn{AgentType: agent.TypeKiro})
			return err
		}},
		{"send", func() error {
			_, _, err := d.sendMessage(ctx, nil, sendMessageIn{SessionID: s.ID, Text: "blocked"})
			return err
		}},
		{"ask", func() error {
			_, _, err := d.askSession(ctx, nil, askSessionIn{SessionID: s.ID, Text: "blocked"})
			return err
		}},
		{"post", func() error {
			_, _, err := d.postMessage(ctx, nil, postMessageIn{SessionID: s.ID, Text: "blocked"})
			return err
		}},
		{"resume", func() error { _, _, err := d.resumeQueue(ctx, nil, sessionIDIn{SessionID: s.ID}); return err }},
		{"delete_queue", func() error {
			_, _, err := d.deleteQueueMessage(ctx, nil, deleteQueueMessageIn{SessionID: s.ID, MessageID: 1})
			return err
		}},
		{"shell", func() error {
			_, _, err := d.shellExec(ctx, nil, shellExecIn{SessionID: s.ID, Command: "echo blocked"})
			return err
		}},
		{"permission", func() error {
			_, _, err := d.respondPermission(ctx, nil, respondPermissionIn{SessionID: s.ID, Decision: "allow_once"})
			return err
		}},
		{"mode", func() error {
			_, _, err := d.setPermissionMode(ctx, nil, setPermissionModeIn{SessionID: s.ID, Mode: "default"})
			return err
		}},
		{"model", func() error {
			_, _, err := d.setModel(ctx, nil, setModelIn{SessionID: s.ID, Model: "changed"})
			return err
		}},
		{"effort", func() error {
			_, _, err := d.setEffort(ctx, nil, setEffortIn{SessionID: s.ID, Effort: "high"})
			return err
		}},
		{"interrupt", func() error { _, _, err := d.interrupt(ctx, nil, sessionIDIn{SessionID: s.ID}); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); err == nil || err.Error() != agent.DisabledReason(agent.TypeKiro) {
				t.Fatalf("expected read-only error, got %v", err)
			}
		})
	}
	if _, _, err := d.getMessages(ctx, nil, getMessagesIn{SessionID: s.ID}); err != nil {
		t.Fatalf("history should remain readable: %v", err)
	}
	if sessions, err := database.ListSessions(); err != nil || len(sessions) != 1 {
		t.Fatalf("legacy session must be preserved: %+v, %v", sessions, err)
	}
}
