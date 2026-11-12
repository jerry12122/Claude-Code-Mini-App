package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

const acpPromptTimeout = 30 * time.Minute

type acpConfigOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
	} `json:"options"`
}

type acpSession struct {
	SessionID string `json:"sessionId"`
	Modes     struct {
		CurrentModeID  string `json:"currentModeId"`
		AvailableModes []struct {
			ID string `json:"id"`
		} `json:"availableModes"`
	} `json:"modes"`
	Models struct {
		AvailableModels []struct {
			ModelID string `json:"modelId"`
		} `json:"availableModels"`
	} `json:"models"`
	ConfigOptions []acpConfigOption `json:"configOptions"`
}

func (c *acpClient) setupRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := c.request(ctx, method, params, acpRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("antigravity ACP %s failed; verify agy login and update agy if needed: %w", method, err)
	}
	return raw, nil
}

func (c *acpClient) prepareSession(ctx context.Context, opts agent.RunOptions) (*acpSession, error) {
	raw, err := c.setupRequest(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"clientInfo":         map[string]any{"name": "claude-miniapp", "version": "0.1.0"},
	})
	if err != nil {
		return nil, err
	}
	var initialized struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
			Meta        struct {
				Antigravity struct {
					SystemPrompt struct {
						Append bool `json:"append"`
					} `json:"systemPrompt"`
				} `json:"antigravity"`
			} `json:"_meta"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(raw, &initialized) != nil || initialized.ProtocolVersion != 1 {
		return nil, errors.New("antigravity ACP initialize response is unsupported; update agy")
	}
	if !initialized.AgentCapabilities.Meta.Antigravity.SystemPrompt.Append {
		return nil, errors.New("antigravity ACP lacks system instruction append support; update agy")
	}
	if opts.SessionID != "" && !initialized.AgentCapabilities.LoadSession {
		return nil, errors.New("antigravity ACP lacks session/load support; update agy")
	}
	cwd := opts.WorkDir
	if cwd == "" {
		cwd = "."
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return nil, errors.New("antigravity ACP cannot resolve working directory")
	}
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
		"_meta":      map[string]any{"antigravity": map[string]any{"systemPrompt": map[string]any{"append": opts.SystemInstruction}}},
	}
	method := "session/new"
	if opts.SessionID != "" {
		method = "session/load"
		params["sessionId"] = opts.SessionID
	}
	raw, err = c.setupRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}
	var sess acpSession
	if json.Unmarshal(raw, &sess) != nil || string(raw) == "null" {
		return nil, errors.New("antigravity ACP returned invalid session settings")
	}
	if opts.SessionID != "" {
		// ACP load responses need not contain sessionId. A conflicting ID is an
		// error rather than silently routing the turn to another conversation.
		if sess.SessionID != "" && sess.SessionID != opts.SessionID {
			return nil, errors.New("antigravity ACP loaded a different session")
		}
		sess.SessionID = opts.SessionID
	}
	if sess.SessionID == "" {
		return nil, errors.New("antigravity ACP session/new omitted sessionId")
	}
	if err := c.applySessionOptions(ctx, &sess, opts.ExtraArgs); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (c *acpClient) applySessionOptions(ctx context.Context, sess *acpSession, extra map[string]string) error {
	mode := strings.TrimSpace(extra[agent.ArgPermissionMode])
	switch mode {
	case "", "default":
		mode = "default"
	case "acceptEdits":
		mode = "accept-edits"
	case "plan":
	case "bypassPermissions", "yolo":
		mode = "" // Explicit startup flag, not an inferred permission grant.
	default:
		return errors.New("antigravity ACP does not support the requested permission mode")
	}
	if mode != "" {
		found := false
		for _, available := range sess.Modes.AvailableModes {
			if available.ID == mode {
				found = true
			}
		}
		if !found {
			return errors.New("antigravity ACP does not advertise the requested permission mode")
		}
		if sess.Modes.CurrentModeID != mode {
			if _, err := c.setupRequest(ctx, "session/set_mode", map[string]any{"sessionId": sess.SessionID, "modeId": mode}); err != nil {
				return err
			}
		}
	}
	if model := strings.TrimSpace(extra[agent.ArgModel]); model != "" {
		if optionID, ok := sess.configValue("model", model); ok {
			if err := c.setConfigOption(ctx, sess, optionID, model); err != nil {
				return err
			}
		} else {
			found := false
			for _, available := range sess.Models.AvailableModels {
				if available.ModelID == model {
					found = true
				}
			}
			if !found {
				return errors.New("antigravity ACP does not advertise the requested model")
			}
			if _, err := c.setupRequest(ctx, "session/set_model", map[string]any{"sessionId": sess.SessionID, "modelId": model}); err != nil {
				return err
			}
		}
	}
	if effort := strings.TrimSpace(extra[agent.ArgEffort]); effort != "" {
		optionID, ok := sess.configValue("thought_level", effort)
		if !ok {
			return errors.New("antigravity ACP does not advertise the requested effort")
		}
		if err := c.setConfigOption(ctx, sess, optionID, effort); err != nil {
			return err
		}
	}
	return nil
}

func (sess *acpSession) configValue(category, value string) (string, bool) {
	for _, option := range sess.ConfigOptions {
		if option.Type != "select" || (option.Category != category && option.ID != category) {
			continue
		}
		for _, available := range option.Options {
			if available.Value == value {
				return option.ID, true
			}
		}
	}
	return "", false
}

func (c *acpClient) setConfigOption(ctx context.Context, sess *acpSession, id, value string) error {
	raw, err := c.setupRequest(ctx, "session/set_config_option", map[string]any{"sessionId": sess.SessionID, "configId": id, "value": value})
	if err != nil {
		return err
	}
	var result struct {
		ConfigOptions []acpConfigOption `json:"configOptions"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return errors.New("antigravity ACP returned invalid updated settings")
	}
	if result.ConfigOptions != nil {
		sess.ConfigOptions = result.ConfigOptions
	}
	return nil
}

func (c *acpClient) prompt(ctx context.Context, sessionID, text string) (json.RawMessage, error) {
	return c.request(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}, acpPromptTimeout)
}
