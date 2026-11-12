package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

type acpTestRequest struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func fakeACPSession(mode string) {
	if len(os.Args) < 2 || os.Args[1] != "acp" {
		os.Exit(2)
	}
	log, err := os.Create(os.Getenv("FAKE_AGY_ACP_LOG"))
	if err != nil {
		os.Exit(4)
	}
	defer log.Close()
	sc := bufio.NewScanner(os.Stdin)
	var pendingPrompt any
	for sc.Scan() {
		fmt.Fprintln(log, sc.Text())
		var req acpTestRequest
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			os.Exit(3)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "":
			if pendingPrompt != nil && mode == "session-permission" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": pendingPrompt, "result": map[string]any{"stopReason": "end_turn"}})
			}
			continue
		case "initialize":
			appendCap := any(true)
			if mode == "session-noappend" {
				appendCap = false
			}
			if mode == "session-badtype" {
				appendCap = "true"
			}
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"loadSession": mode != "session-noload", "_meta": map[string]any{"antigravity": map[string]any{"systemPrompt": map[string]any{"append": appendCap}}},
			}}
		case "session/new", "session/load":
			if req.Method == "session/load" && mode == "session-events" {
				fakeACPUpdate("agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "HISTORY"}})
			}
			result = fakeACPSessionOptions()
			if req.Method == "session/new" {
				result.(map[string]any)["sessionId"] = "new-id"
			}
		case "session/set_config_option", "session/set_mode", "session/set_model":
			result = fakeACPSessionOptions()
		case "session/prompt":
			pendingPrompt = req.ID
			switch mode {
			case "session-events":
				fakeACPUpdate("agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "new answer"}})
				fakeACPUpdate("agent_thought_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "think "}})
				fakeACPUpdate("agent_thought_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "more"}})
				fakeACPUpdate("tool_call", map[string]any{"toolCallId": "tool-1", "title": "Read file", "status": "in_progress", "rawInput": map[string]any{"path": "file.txt"}})
				fakeACPUpdate("tool_call_update", map[string]any{"toolCallId": "tool-1", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "file contents"}}}})
			case "session-permission":
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": "permission-1", "method": "session/request_permission", "params": map[string]any{"sessionId": "new-id", "toolCall": map[string]any{"toolCallId": "tool-1", "title": "Write file"}, "options": []any{map[string]any{"optionId": "allow-one", "kind": "allow_once", "name": "Allow once"}, map[string]any{"optionId": "deny-one", "kind": "reject_once", "name": "Reject"}}}})
				continue
			case "session-unknown":
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": "question-1", "method": "provider/question", "params": map[string]any{}})
				continue
			case "session-cancel":
				fakeACPUpdate("agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "running"}})
				continue
			case "session-eof":
				return
			case "session-rpcerror":
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32603, "message": "PRIVATE_INSTRUCTION"}})
				continue
			}
			result = map[string]any{"stopReason": "end_turn"}
		}
		if req.ID == nil {
			continue
		}
		if mode == "session-auth" && req.Method == "session/new" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "PRIVATE_INSTRUCTION login needed"}})
			continue
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if mode == "session-nonzero" && req.Method == "session/prompt" {
			os.Exit(9)
		}
	}
}

func fakeACPUpdate(kind string, fields map[string]any) {
	fields["sessionUpdate"] = kind
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "new-id", "update": fields}})
}

func fakeACPSessionOptions() map[string]any {
	return map[string]any{
		"modes":  map[string]any{"currentModeId": "default", "availableModes": []any{map[string]any{"id": "default"}, map[string]any{"id": "accept-edits"}, map[string]any{"id": "plan"}}},
		"models": map[string]any{"currentModelId": "gemini-high", "availableModels": []any{map[string]any{"modelId": "gemini-high"}, map[string]any{"modelId": "claude-sonnet"}}},
		"configOptions": []any{
			map[string]any{"id": "model", "category": "model", "type": "select", "currentValue": "gemini", "options": []any{map[string]any{"value": "gemini"}, map[string]any{"value": "claude-sonnet"}}},
			map[string]any{"id": "thought_level", "category": "thought_level", "type": "select", "currentValue": "high", "options": []any{map[string]any{"value": "low"}, map[string]any{"value": "medium"}, map[string]any{"value": "high"}}},
		},
	}
}

func newACPSessionTest(t *testing.T, mode string, opts agent.RunOptions) (*acpClient, string) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "rpc.jsonl")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_AGY_BIN", bin)
	t.Setenv("FAKE_AGY_ACP", mode)
	t.Setenv("FAKE_AGY_ACP_LOG", log)
	c, err := startACP(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.close() })
	return c, log
}

func readACPSessionLog(t *testing.T, path string) []acpTestRequest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var requests []acpTestRequest
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var req acpTestRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, req)
	}
	return requests
}

func TestACPInstructionMetadata(t *testing.T) {
	for _, id := range []string{"", "saved-id"} {
		for _, user := range []string{"user only", ""} {
			t.Run(id+"/"+user, func(t *testing.T) {
				instruction := "PRIVATE_INSTRUCTION 繁體中文\n引號\" 路徑 C:\\work"
				opts := agent.RunOptions{SessionID: id, WorkDir: t.TempDir(), Prompt: user, SystemInstruction: instruction}
				c, log := newACPSessionTest(t, "session", opts)
				sess, err := c.prepareSession(context.Background(), opts)
				if err != nil {
					t.Fatal(err)
				}
				wantID := id
				if wantID == "" {
					wantID = "new-id"
				}
				if sess.SessionID != wantID {
					t.Fatalf("sessionId=%q want %q", sess.SessionID, wantID)
				}
				if _, err := c.prompt(context.Background(), sess.SessionID, opts.Prompt); err != nil {
					t.Fatal(err)
				}
				requests := readACPSessionLog(t, log)
				var opened, prompted bool
				for _, req := range requests {
					switch req.Method {
					case "session/new", "session/load":
						opened = true
						want := map[string]any{"cwd": opts.WorkDir, "mcpServers": []any{}, "_meta": map[string]any{"antigravity": map[string]any{"systemPrompt": map[string]any{"append": instruction}}}}
						if id != "" {
							want["sessionId"] = id
							if req.Method != "session/load" {
								t.Fatal("resume did not load")
							}
						}
						if !reflect.DeepEqual(req.Params, want) {
							t.Fatalf("unexpected metadata: %#v", req.Params)
						}
					case "session/prompt":
						prompted = true
						want := map[string]any{"sessionId": wantID, "prompt": []any{map[string]any{"type": "text", "text": user}}}
						if !reflect.DeepEqual(req.Params, want) {
							t.Fatalf("unexpected user prompt: %#v", req.Params)
						}
					}
				}
				if !opened || !prompted {
					t.Fatal("missing open or prompt request")
				}
			})
		}
	}
}

func TestACPSessionWorkingDirectory(t *testing.T) {
	for _, cwd := range []string{"", "."} {
		t.Run("cwd="+cwd, func(t *testing.T) {
			opts := agent.RunOptions{WorkDir: cwd, SystemInstruction: "private"}
			c, log := newACPSessionTest(t, "session", opts)
			if _, err := c.prepareSession(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			want, err := filepath.Abs(".")
			if err != nil {
				t.Fatal(err)
			}
			for _, req := range readACPSessionLog(t, log) {
				if req.Method == "session/new" && req.Params["cwd"] != want {
					t.Fatalf("cwd=%v want %q", req.Params["cwd"], want)
				}
			}
		})
	}
}

func TestACPCapabilityFailure(t *testing.T) {
	for _, mode := range []string{"session-noappend", "session-badtype", "session-noload", "session-auth"} {
		t.Run(mode, func(t *testing.T) {
			opts := agent.RunOptions{SystemInstruction: "PRIVATE_INSTRUCTION", SessionID: "saved-id"}
			if mode == "session-auth" {
				opts.SessionID = ""
			}
			c, log := newACPSessionTest(t, mode, opts)
			_, err := c.prepareSession(context.Background(), opts)
			if err == nil {
				t.Fatal("expected explicit capability/auth failure")
			}
			if strings.Contains(err.Error(), "PRIVATE_INSTRUCTION") {
				t.Fatal("private payload in error")
			}
			if mode == "session-auth" && !strings.Contains(err.Error(), "login") {
				t.Fatalf("missing auth recovery hint: %v", err)
			}
			for _, req := range readACPSessionLog(t, log) {
				if req.Method == "session/prompt" {
					t.Fatal("prompt sent after failed setup")
				}
			}
		})
	}
}

func TestACPRequestedOptions(t *testing.T) {
	for _, tc := range []struct {
		key, value, method, field, want string
		reject                          bool
	}{
		{agent.ArgModel, "gemini", "session/set_config_option", "value", "gemini", false},
		{agent.ArgModel, "gemini-high", "session/set_model", "modelId", "gemini-high", false},
		{agent.ArgEffort, "low", "session/set_config_option", "value", "low", false},
		{agent.ArgPermissionMode, "acceptEdits", "session/set_mode", "modeId", "accept-edits", false},
		{agent.ArgPermissionMode, "plan", "session/set_mode", "modeId", "plan", false},
		{agent.ArgPermissionMode, "bypassPermissions", "", "", "", false},
		{agent.ArgModel, "unsupported", "", "", "", true},
		{agent.ArgEffort, "max", "", "", "", true},
		{agent.ArgPermissionMode, "unknown", "", "", "", true},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			opts := agent.RunOptions{SystemInstruction: "private", ExtraArgs: map[string]string{tc.key: tc.value}}
			c, log := newACPSessionTest(t, "session", opts)
			if tc.key == agent.ArgPermissionMode {
				joined := strings.Join(c.cmd.Args, " ")
				if strings.Contains(joined, "--dangerously-skip-permissions") != (tc.value == "bypassPermissions" || tc.value == "yolo") {
					t.Fatal("startup permission flag did not match explicit request")
				}
			}
			sess, err := c.prepareSession(context.Background(), opts)
			if (err != nil) != tc.reject {
				t.Fatalf("setup err=%v reject=%v", err, tc.reject)
			}
			if tc.reject {
				return
			}
			if sess.SessionID != "new-id" {
				t.Fatal("session identity lost")
			}
			if tc.method != "" {
				found := false
				for _, req := range readACPSessionLog(t, log) {
					if req.Method == tc.method && req.Params[tc.field] == tc.want {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s %s=%s", tc.method, tc.field, tc.want)
				}
			}
		})
	}
}
