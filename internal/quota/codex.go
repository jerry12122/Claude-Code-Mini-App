package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

// CodexFetcher queries account limits without starting a thread or model turn.
type CodexFetcher struct{}

func (f *CodexFetcher) Provider() string { return agent.TypeCodex }

func (f *CodexFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := readCodexQuota(exec.CommandContext(ctx, "codex", "app-server"))
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return Snapshot{Provider: agent.TypeCodex, DisplayText: "—"}, fmt.Errorf("codex quota: %w", err)
	}
	return snapshotFromInfo(agent.TypeCodex, info), nil
}

func readCodexQuota(cmd *exec.Cmd) (*usage.QuotaInfo, error) {
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return proc.KillTree(cmd.Process.Pid) }
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		// ponytail: two RPCs per refresh; stop the process tree instead of keeping a server alive.
		_ = cmd.Cancel()
		_ = cmd.Wait()
	}()
	enc, dec := json.NewEncoder(stdin), json.NewDecoder(stdout)
	call := func(id int, method string, params any) (json.RawMessage, error) {
		if err := enc.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for {
			var response struct {
				ID     *int            `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := dec.Decode(&response); err != nil {
				return nil, fmt.Errorf("%s: %w", method, err)
			}
			if response.Method != "" {
				if response.ID != nil {
					return nil, fmt.Errorf("unexpected server request: %s", response.Method)
				}
				continue
			}
			if response.ID == nil || *response.ID != id {
				continue
			}
			if response.Error != nil {
				return nil, fmt.Errorf("%s: RPC %d: %s", method, response.Error.Code, response.Error.Message)
			}
			if len(response.Result) == 0 || string(response.Result) == "null" {
				return nil, fmt.Errorf("%s: missing result", method)
			}
			return response.Result, nil
		}
	}
	if _, err := call(1, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "claude-miniapp-quota", "version": "1"},
	}); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	raw, err := call(2, "account/rateLimits/read", map[string]any{})
	if err != nil {
		return nil, err
	}
	return usage.FromCodexRateLimitsJSON(raw)
}
