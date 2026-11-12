package claude

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestBuildClaudeArgsSystemInstruction(t *testing.T) {
	t.Parallel()
	const instruction = "Use \"quotes\".\nKeep C:\\work\\檔案 and 日本語."
	for _, sessionID := range []string{"", "saved-session"} {
		t.Run("session="+sessionID, func(t *testing.T) {
			t.Parallel()
			opts := agent.RunOptions{
				SessionID:         sessionID,
				Prompt:            "user prompt stays separate",
				SystemInstruction: instruction,
				CliExtraArgs:      []string{"--system-prompt-snapshot", "on"},
				ExtraArgs: map[string]string{
					agent.ArgPermissionMode:   "acceptEdits",
					agent.ArgAllowedTools:     "Read,Edit",
					agent.ArgModel:            "sonnet",
					agent.ArgEffort:           "high",
					agent.ArgPermPromptConfig: "C:\\bridge config.json",
				},
			}
			want := []string{
				"--system-prompt-snapshot", "on",
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
			}
			if sessionID != "" {
				want = append(want, "--resume", "saved-session")
			}
			want = append(want,
				"--permission-mode", "acceptEdits",
				"--allowedTools", "Read", "--allowedTools", "Edit",
				"--model", "sonnet", "--effort", "high",
				"--mcp-config", "C:\\bridge config.json", "--permission-prompt-tool", agent.PermPromptFlag,
				"--append-system-prompt", instruction,
			)
			if got := buildClaudeArgs(opts); !slices.Equal(got, want) {
				t.Fatalf("buildClaudeArgs() = %#v\nwant %#v", got, want)
			}
			if opts.Prompt != "user prompt stays separate" {
				t.Fatalf("user prompt changed: %q", opts.Prompt)
			}
		})
	}
}

func TestRunSystemInstructionRedacted(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Inspect startup logging without launching a provider process.
	const instruction = "private-instruction-marker"
	err := (&Runner{}).Run(ctx, agent.RunOptions{SystemInstruction: instruction}, func(agent.Event) {})
	if err == nil {
		t.Fatal("canceled run unexpectedly succeeded")
	}
	if strings.Contains(logs.String(), instruction) {
		t.Fatalf("generated argv logs contain instruction: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "--append-system-prompt [redacted]") {
		t.Fatalf("generated argv logs missing redaction marker: %s", logs.String())
	}
}

func TestClaudeArgsForLogPreservesArgv(t *testing.T) {
	t.Parallel()
	args := []string{"-p", "--append-system-prompt", "private instruction", "--model", "sonnet"}
	if got := claudeArgsForLog(args); got != "-p --append-system-prompt [redacted] --model sonnet" {
		t.Fatalf("unexpected logged argv: %q", got)
	}
	if !slices.Equal(args, []string{"-p", "--append-system-prompt", "private instruction", "--model", "sonnet"}) {
		t.Fatalf("logging changed process argv: %#v", args)
	}
}

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
			Type:  "content_block_delta",
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

func TestDispatch_userToolResultImageEmitsMarkdownDelta(t *testing.T) {
	t.Chdir(t.TempDir()) // 不可與 t.Parallel 併用 // media.SaveBase64Image 用相對路徑落地檔案，隔離避免污染原始碼樹

	r := &Runner{}
	var st streamState
	var deltas []string
	cb := func(e agent.Event) {
		if e.Type == agent.EventDelta {
			deltas = append(deltas, e.Text)
		}
	}
	r.dispatch(&StreamEvent{
		Type: "user",
		Message: &AssistantMessage{Content: []MessageContent{
			{
				Type: "tool_result",
				Content: []MessageContent{
					{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}},
				},
			},
		}},
	}, cb, &st)

	if len(deltas) != 1 || !strings.Contains(deltas[0], "![screenshot](/uploads/") {
		t.Fatalf("預期收到一則 markdown image delta，got deltas=%v", deltas)
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
		Type:  "stream_event",
		Event: &APIEvent{Type: "content_block_start", ContentBlock: &ContentBlock{Type: "thinking"}},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type:  "stream_event",
		Event: &APIEvent{Type: "content_block_delta", Delta: &Delta{Type: "thinking_delta", Text: "A"}},
	}, cb, &st)
	// 第二個 thinking block 開始，應重置 buffer
	r.dispatch(&StreamEvent{
		Type:  "stream_event",
		Event: &APIEvent{Type: "content_block_start", ContentBlock: &ContentBlock{Type: "thinking"}},
	}, cb, &st)
	r.dispatch(&StreamEvent{
		Type:  "stream_event",
		Event: &APIEvent{Type: "content_block_delta", Delta: &Delta{Type: "thinking_delta", Text: "B"}},
	}, cb, &st)
	// 第二輪應只看到 "B"，不是 "AB"
	if !slices.Equal(thoughts, []string{"思考中…", "A", "思考中…", "B"}) {
		t.Fatalf("連續 thinking block 間應重置 thinkingBuf，got thoughts=%v", thoughts)
	}
}

func TestDispatchToolActivityLifecycle(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		var st streamState
		var events []agent.Event
		cb := func(e agent.Event) { events = append(events, e) }
		r := &Runner{}
		if streamed {
			e, _ := ParseEvent([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"檢查中"}}}`))
			r.dispatch(e, cb, &st)
		}
		for _, line := range []string{
			`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash"}}}`,
			`{"type":"assistant","message":{"content":[{"type":"text","text":"檢查中"},{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"git status"}}]}}`,
		} {
			e, err := ParseEvent([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			r.dispatch(e, cb, &st)
		}
		last := events[len(events)-1]
		if last.Type != agent.EventActivity || last.Text != "執行工具：Bash：git status" {
			t.Fatalf("工具活動必須在文字之後，streamed=%v: %+v", streamed, events)
		}
		var text string
		for _, e := range events {
			if e.Type == agent.EventDelta {
				text += e.Text
			}
		}
		if text != "檢查中" {
			t.Fatalf("工具不得重複文字: %q", text)
		}
		e, _ := ParseEvent([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-1","is_error":true,"content":"failed"}]}}`))
		r.dispatch(e, cb, &st)
		last = events[len(events)-1]
		if last.Type != agent.EventActivity || last.Text != "工具失敗：Bash" || len(st.toolNames) != 0 {
			t.Fatalf("工具結果: %+v names=%v", last, st.toolNames)
		}
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

func TestDispatch_userResetsStreamFlagSoNextAssistantAccepted(t *testing.T) {
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
			Type:  "content_block_delta",
			Delta: &Delta{Type: "text_delta", Text: "turn1"},
		},
	}, cb, &st)
	r.dispatch(&StreamEvent{Type: "user"}, cb, &st)
	r.dispatch(&StreamEvent{
		Type:    "assistant",
		Message: &AssistantMessage{Content: []MessageContent{{Type: "text", Text: "turn2-final"}}},
	}, cb, &st)
	if len(deltas) != 2 || deltas[0] != "turn1" || deltas[1] != "turn2-final" {
		t.Fatalf("user 事件後應接受下一輪 assistant，got deltas=%v", deltas)
	}
}

func TestDispatch_resultFillsDeltaWhenNoTextEmitted(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	var st streamState
	var deltas []string
	var done *agent.Event
	cb := func(ev agent.Event) {
		switch ev.Type {
		case agent.EventDelta:
			deltas = append(deltas, ev.Text)
		case agent.EventDone:
			ev := ev
			done = &ev
		}
	}
	r.dispatch(&StreamEvent{Type: "result", SessionID: "sid", Result: "only-in-result"}, cb, &st)
	if len(deltas) != 1 || deltas[0] != "only-in-result" {
		t.Fatalf("無先前文字時 result 應後備成 delta，got deltas=%v", deltas)
	}
	if done == nil || done.ResultText != "only-in-result" {
		t.Fatalf("EventDone 應保留 ResultText，got %+v", done)
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
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
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
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
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
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
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
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
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
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
				"--permission-mode", "default",
				"--allowedTools", "Write",
				"--allowedTools", "Edit",
			},
		},
		{
			name: "model 與 effort",
			opts: agent.RunOptions{
				ExtraArgs: map[string]string{
					agent.ArgPermissionMode: "default",
					agent.ArgModel:          "sonnet",
					agent.ArgEffort:         "high",
				},
			},
			want: []string{
				"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
				"--permission-mode", "default",
				"--model", "sonnet",
				"--effort", "high",
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
