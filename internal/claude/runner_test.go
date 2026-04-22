package claude

import (
	"slices"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestDispatch_streamTextDeltaThenAssistantSkipped(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{
			Type: "content_block_delta",
			Delta: &Delta{Type: "text_delta", Text: "hello"},
		},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type:    "assistant",
		Message: &AssistantMessage{Content: []MessageContent{{Type: "text", Text: "hello"}}},
	}, cb, &st)
	if len(deltas) != 1 || deltas[0] != "hello" {
		t.Fatalf("應僅保留串流 delta，略過重複 assistant，got deltas=%v", deltas)
	}
}

func TestDispatch_assistantOnlyNoStream(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	r.dispatch(&StreamEvent{
		Type:    "assistant",
		Message: &AssistantMessage{Content: []MessageContent{{Type: "text", Text: "only-assistant"}}},
	}, cb, &st)
	if len(deltas) != 1 || deltas[0] != "only-assistant" {
		t.Fatalf("無串流時應採用 assistant 全文，got deltas=%v", deltas)
	}
}

func TestDispatch_resultEmitsDoneWithResultText(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var done *agent.Event
	cb := func(ev agent.Event) {
		if ev.Type == agent.EventDone {
			ev := ev
			done = &ev
		}
	}
	r.dispatch(&StreamEvent{Type: "result", SessionID: "sid-1", Result: "final CLI result line"}, cb, &st)
	if done == nil || done.ResultText != "final CLI result line" || done.SessionID != "sid-1" {
		t.Fatalf("EventDone 應帶入 Result 欄位，got %+v", done)
	}
}

func TestDispatch_thinkingDeltaAccumulatesFullSnapshotEachEmit(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var thoughts []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventThinking {
			thoughts = append(thoughts, e.Text)
		}
	}
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{
			Type:  "content_block_delta",
			Delta: &Delta{Type: "thinking_delta", Text: "步"},
		},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{
			Type:  "content_block_delta",
			Delta: &Delta{Type: "thinking_delta", Text: "驟一"},
		},
	}, cb, &st)
	if len(thoughts) != 2 || thoughts[0] != "步" || thoughts[1] != "步驟一" {
		t.Fatalf("thinking 應累積為完整快照，got thoughts=%v", thoughts)
	}
}

func TestDispatch_consecutiveThinkingBlocksResetBuffer(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var thoughts []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventThinking {
			thoughts = append(thoughts, e.Text)
		}
	}
	// 第一個 thinking block
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{Type: "content_block_start", ContentBlock: &ContentBlock{Type: "thinking"}},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{Type: "content_block_delta", Delta: &Delta{Type: "thinking_delta", Text: "A"}},
	}, cb, &st)
	// 第二個 thinking block 開始，應重置 buffer
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{Type: "content_block_start", ContentBlock: &ContentBlock{Type: "thinking"}},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{Type: "content_block_delta", Delta: &Delta{Type: "thinking_delta", Text: "B"}},
	}, cb, &st)
	// 第二輪應只看到 "B"，不是 "AB"
	if len(thoughts) != 2 || thoughts[0] != "A" || thoughts[1] != "B" {
		t.Fatalf("連續 thinking block 間應重置 thinkingBuf，got thoughts=%v", thoughts)
	}
}

func TestDispatch_contentBlockStartWithoutDeltaThenAssistant(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	r.dispatch(&StreamEvent{
		Type: "stream_event",
		Event: &APIEvent{
			Type:         "content_block_start",
			ContentBlock: &ContentBlock{Type: "text"},
		},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type:    "assistant",
		Message: &AssistantMessage{Content: []MessageContent{{Type: "text", Text: "fallback"}}},
	}, cb, &st)
	if len(deltas) != 1 || deltas[0] != "fallback" {
		t.Fatalf("僅有 block_start、尚無 text_delta 時仍應採用 assistant，got deltas=%v", deltas)
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts agent.RunOptions
		want []string
	}{
		{
			name: "僅內建旗標與預設 permission_mode",
			opts: agent.RunOptions{},
			want: []string{
				"-p", "--output-format", "stream-json", "--verbose",
				"--permission-mode", "default",
			},
		},
		{
			name: "resume 與自訂 permission_mode",
			opts: agent.RunOptions{
				SessionID: "sess-uuid",
				ExtraArgs: map[string]string{
					agent.ArgPermissionMode: "acceptEdits",
				},
			},
			want: []string{
				"-p", "--output-format", "stream-json", "--verbose",
				"--resume", "sess-uuid",
				"--permission-mode", "acceptEdits",
			},
		},
		{
			name: "多個 CliExtraArgs 置於 -p 之前（例如多個 --plugin-dir）",
			opts: agent.RunOptions{
				CliExtraArgs: []string{
					"--plugin-dir", "./.claude/plugins/crm",
					"--plugin-dir", "./.claude/plugins/crm2",
				},
				SessionID: "abc",
				ExtraArgs: map[string]string{
					agent.ArgPermissionMode: "default",
				},
			},
			want: []string{
				"--plugin-dir", "./.claude/plugins/crm",
				"--plugin-dir", "./.claude/plugins/crm2",
				"-p", "--output-format", "stream-json", "--verbose",
				"--resume", "abc",
				"--permission-mode", "default",
			},
		},
		{
			name: "路徑含空格之單一 argv 保持為一個元素",
			opts: agent.RunOptions{
				CliExtraArgs: []string{"--plugin-dir", "./.claude/plugins/my project"},
			},
			want: []string{
				"--plugin-dir", "./.claude/plugins/my project",
				"-p", "--output-format", "stream-json", "--verbose",
				"--permission-mode", "default",
			},
		},
		{
			name: "allowedTools 拆成多個 --allowedTools",
			opts: agent.RunOptions{
				ExtraArgs: map[string]string{
					agent.ArgPermissionMode: "default",
					agent.ArgAllowedTools:   " Write , Edit ",
				},
			},
			want: []string{
				"-p", "--output-format", "stream-json", "--verbose",
				"--permission-mode", "default",
				"--allowedTools", "Write",
				"--allowedTools", "Edit",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildClaudeArgs(tt.opts)
			if !slices.Equal(got, tt.want) {
				t.Errorf("buildClaudeArgs() = %#v\nwant %#v", got, tt.want)
			}
		})
	}
}
