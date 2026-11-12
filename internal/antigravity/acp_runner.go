package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

type acpRunState struct {
	mu        sync.Mutex
	active    bool
	sessionID string
	text      string
	thinking  string
	started   bool
	tools     map[string]*agent.ToolCall
	completed map[string]bool
	cb        agent.EventCallback
}

func (r *Runner) runACP(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) (retErr error) {
	state := &acpRunState{cb: cb, tools: make(map[string]*agent.ToolCall), completed: make(map[string]bool)}
	defer func() {
		if retErr != nil {
			cb(agent.Event{Type: agent.EventError, SessionID: state.sessionID, Err: retErr})
		}
	}()
	c, err := startACP(ctx, opts, acpHandlers{notification: state.update, request: func(pctx context.Context, msg acpMessage) (any, error) {
		return state.permission(pctx, opts.RequestPermission, msg)
	}})
	if err != nil {
		return err
	}
	defer func() {
		state.mu.Lock()
		state.active = false
		state.mu.Unlock()
		if err := c.close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	sess, err := c.prepareSession(ctx, opts)
	if err != nil {
		return err
	}
	c.setSessionID(sess.SessionID)
	state.mu.Lock()
	state.sessionID = sess.SessionID
	cb(agent.Event{Type: agent.EventSessionInit, SessionID: sess.SessionID})
	// All load replay has been consumed before the load response. Only this
	// turn's prompt is allowed to contribute text, thought or tool events.
	state.active = true
	state.mu.Unlock()
	raw, err := c.prompt(ctx, sess.SessionID, opts.Prompt)
	state.mu.Lock()
	state.active = false
	text := state.text
	state.mu.Unlock()
	if err != nil {
		return fmt.Errorf("antigravity ACP prompt failed; verify agy login if needed: %w", err)
	}
	var result struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return errors.New("antigravity ACP returned an invalid prompt result")
	}
	switch result.StopReason {
	case "end_turn", "max_tokens", "max_turn_requests", "refusal":
	case "cancelled":
		return context.Canceled
	default:
		return errors.New("antigravity ACP prompt returned an unsupported stop reason")
	}
	if err := ctx.Err(); err != nil {
		c.abort(err)
		return err
	}
	if err := c.finish(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cb(agent.Event{Type: agent.EventDone, SessionID: sess.SessionID, ResultText: text})
	return nil
}

type acpUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
}

func acpText(raw json.RawMessage) string {
	var block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &block) == nil && block.Type == "text" {
		return block.Text
	}
	return ""
}

func acpToolText(raw json.RawMessage) string {
	var blocks []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "content" {
			text.WriteString(acpText(block.Content))
		}
	}
	return text.String()
}

func acpRawToolText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func (s *acpRunState) update(msg acpMessage) error {
	if msg.Method != "session/update" {
		return nil
	}
	var params struct {
		SessionID string    `json:"sessionId"`
		Update    acpUpdate `json:"update"`
	}
	if json.Unmarshal(msg.Params, &params) != nil {
		return errors.New("antigravity ACP returned an invalid session update")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return nil
	}
	if params.SessionID != s.sessionID {
		return errors.New("antigravity ACP sent an update for a different session")
	}
	update := params.Update
	switch update.SessionUpdate {
	case "agent_message_chunk":
		text := acpText(update.Content)
		if text == "" {
			return nil
		}
		if !s.started {
			s.started = true
			s.cb(agent.Event{Type: agent.EventStreamStart})
		}
		s.text += text
		s.cb(agent.Event{Type: agent.EventDelta, Text: text})
	case "agent_thought_chunk":
		s.thinking += acpText(update.Content)
		s.cb(agent.Event{Type: agent.EventThinking, Text: s.thinking})
		s.cb(agent.Event{Type: agent.EventActivity, Text: "思考中"})
	case "tool_call", "tool_call_update":
		if update.ToolCallID == "" {
			return errors.New("antigravity ACP tool update omitted its identifier")
		}
		tool := s.tools[update.ToolCallID]
		if tool == nil {
			tool = &agent.ToolCall{CallID: update.ToolCallID, Name: update.Title}
			if tool.Name == "" {
				tool.Name = update.Kind
			}
			if tool.Name == "" {
				tool.Name = "Tool"
			}
			s.tools[update.ToolCallID] = tool
			tool.Arguments = update.RawInput
			copy := *tool
			s.cb(agent.Event{Type: agent.EventToolStarted, Tool: &copy})
			s.cb(agent.Event{Type: agent.EventActivity, Text: "執行工具：" + tool.Name})
		}
		if update.Title != "" {
			tool.Name = update.Title
		}
		if len(update.RawInput) > 0 {
			tool.Arguments = update.RawInput
		}
		if output := acpToolText(update.Content); output != "" {
			tool.Output = output
		} else if output := acpRawToolText(update.RawOutput); output != "" {
			tool.Output = output
		}
		if update.Status == "completed" || update.Status == "failed" {
			if s.completed[tool.CallID] {
				return nil
			}
			s.completed[tool.CallID] = true
			tool.OK = update.Status == "completed"
			if !tool.OK {
				tool.ErrMessage = "antigravity tool failed"
			}
			copy := *tool
			s.cb(agent.Event{Type: agent.EventToolCompleted, Tool: &copy})
		}
	}
	return nil
}

func (s *acpRunState) permission(ctx context.Context, callback func(context.Context, agent.PermissionRequest) string, msg acpMessage) (any, error) {
	if msg.Method != "session/request_permission" {
		return nil, errors.New("antigravity ACP requested an unsupported client interaction; cancel it in agy and retry")
	}
	var params struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if json.Unmarshal(msg.Params, &params) != nil {
		return nil, errors.New("antigravity ACP returned an invalid permission request")
	}
	outcome := map[string]any{"outcome": "cancelled"}
	s.mu.Lock()
	active := s.active && params.SessionID == s.sessionID
	s.mu.Unlock()
	if callback != nil && active && ctx.Err() == nil {
		req := agent.PermissionRequest{ToolCallID: params.ToolCall.ToolCallID, Title: params.ToolCall.Title}
		for _, option := range params.Options {
			req.Options = append(req.Options, agent.PermissionOption{OptionID: option.OptionID, Name: option.Name, Kind: option.Kind})
		}
		selected := callback(ctx, req)
		if selected != "" && ctx.Err() == nil {
			for _, option := range params.Options {
				if selected == option.OptionID {
					outcome = map[string]any{"outcome": "selected", "optionId": selected}
					break
				}
			}
		}
	}
	return map[string]any{"outcome": outcome}, nil
}
