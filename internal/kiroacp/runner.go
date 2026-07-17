package kiroacp

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

func init() {
	agent.Register(agent.TypeKiroACP, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 以 kiro-cli acp（JSON-RPC over stdio）實作 agent.Runner。
//
// 每則訊息 spawn 一次子進程：initialize → session/new|load → session/prompt → kill。
// 不重用 internal/kiro 的 --list-sessions / TTY 行解析。
//
// 已知限制（POC 實測 kiro-cli 2.12.1）：session/load 目前會 timeout，
// 因此跨進程 resume 可能失敗；首回合 session/new 與單回合 prompt 已驗證通過。
type Runner struct{}

func (r *Runner) Name() string { return agent.TypeKiroACP }

func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	args := buildArgs(opts)
	log.Printf("[kiroacp] 執行: kiro-cli %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt))
	if opts.WorkDir != "" {
		log.Printf("[kiroacp] 工作目錄: %s", opts.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "kiro-cli", args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.GracefulStop(cmd.Process.Pid, 3*time.Second)
		}
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[kiroacp] 啟動失敗: %v", err)
		return err
	}
	log.Printf("[kiroacp] 子進程 PID=%d", cmd.Process.Pid)

	cl := newClient(cmd, stdout, stdin)
	defer func() {
		cl.close()
		_ = cmd.Wait()
	}()

	streamStarted := false
	emitStreamStart := func() {
		if streamStarted {
			return
		}
		streamStarted = true
		cb(agent.Event{Type: agent.EventStreamStart})
	}

	cl.onUpdate = func(body sessionUpdateBody) {
		if ctx.Err() != nil {
			return
		}
		switch body.SessionUpdate {
		case "agent_message_chunk":
			text := extractAgentText(body)
			if text == "" {
				return
			}
			emitStreamStart()
			cb(agent.Event{Type: agent.EventDelta, Text: text})
		case "tool_call", "tool_call_update":
			label := strings.TrimSpace(body.Title)
			if label == "" {
				label = body.Kind
			}
			if body.Status != "" {
				label = label + " (" + body.Status + ")"
			}
			if label == "" {
				label = body.SessionUpdate
			}
			cb(agent.Event{Type: agent.EventActivity, Text: label})
		}
	}

	cwd := opts.WorkDir
	if cwd == "" {
		cwd = "."
	}

	if _, err := cl.call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "claude-miniapp", "version": "0.1.0"},
	}); err != nil {
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	_ = cl.notify("initialized", map[string]any{})

	sessionID := strings.TrimSpace(opts.SessionID)
	var sess sessionNewResult

	if sessionID == "" {
		raw, err := cl.call(ctx, "session/new", map[string]any{
			"cwd":        cwd,
			"mcpServers": []any{},
		})
		if err != nil {
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sess, err = parseSessionResult(raw)
		if err != nil {
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sessionID = sess.SessionID
		if sessionID == "" {
			err := fmt.Errorf("kiroacp: session/new 未回傳 sessionId")
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		log.Printf("[kiroacp] session/new id=%s model=%s", sessionID, modelIDFrom(sess))
		cb(agent.Event{
			Type:      agent.EventSessionInit,
			SessionID: sessionID,
			Model:     modelSnapshot(sess),
		})
	} else {
		// POC：session/load 在 2.12.1 常 timeout；設較短逾時並回明確錯誤。
		loadCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		raw, err := cl.call(loadCtx, "session/load", map[string]any{
			"sessionId": sessionID,
			"cwd":       cwd,
		})
		cancel()
		if err != nil {
			err = fmt.Errorf("kiroacp: session/load 失敗（POC 已知限制，跨進程 resume 可能不可用）: %w", err)
			log.Printf("[kiroacp] %v", err)
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sess, _ = parseSessionResult(raw)
		log.Printf("[kiroacp] session/load id=%s model=%s", sessionID, modelIDFrom(sess))
		if m := modelSnapshot(sess); m != nil {
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: sessionID, Model: m})
		}
	}

	if _, err := cl.call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt": []map[string]string{
			{"type": "text", "text": opts.Prompt},
		},
	}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}

	cb(agent.Event{Type: agent.EventDone, SessionID: sessionID})
	return nil
}

func buildArgs(opts agent.RunOptions) []string {
	args := []string{"acp", "--trust-all-tools"}
	if opts.ExtraArgs != nil {
		if effort := strings.TrimSpace(opts.ExtraArgs["effort"]); effort != "" {
			args = append(args, "--effort", effort)
		}
		if agentProfile := strings.TrimSpace(opts.ExtraArgs["agent"]); agentProfile != "" {
			args = append(args, "--agent", agentProfile)
		}
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "--model", m)
		}
	}
	return args
}

func modelIDFrom(s sessionNewResult) string {
	if s.Models == nil {
		return ""
	}
	return strings.TrimSpace(s.Models.CurrentModelID)
}

func modelSnapshot(s sessionNewResult) *agent.ModelSnapshot {
	id := modelIDFrom(s)
	if id == "" {
		return nil
	}
	return model.AgentSnapshot(model.Info{
		Provider:    agent.TypeKiroACP,
		Model:       id,
		DisplayText: model.FormatDisplay(id),
		Source:      model.SourceInitEvent,
		Ok:          true,
	})
}