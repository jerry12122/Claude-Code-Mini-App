package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type ClaudeFetcher struct{}

func (f *ClaudeFetcher) Provider() string { return agent.TypeClaude }

func (f *ClaudeFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	text, _ := runClaudeUsageText(ctx)
	info := usage.FromClaudeUsageText(text)
	if len(info.Windows) == 0 {
		if oauthInfo, err := fetchClaudeOAuth(ctx); err == nil && oauthInfo != nil {
			info = oauthInfo
		}
	}
	snap := Snapshot{
		Provider:    agent.TypeClaude,
		DisplayText: FormatDisplay(agent.TypeClaude, info),
		UpdatedAt:   time.Now(),
	}
	if snap.DisplayText == "—" {
		return snap, fmt.Errorf("claude quota: no session/week data")
	}
	return snap, nil
}

func runClaudeUsageText(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", "/usage")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = proc.SysProcAttr()
	_ = cmd.Run()
	text := stdout.String()
	if strings.TrimSpace(text) == "" {
		text = stderr.String()
	}
	return text, nil
}

var (
	claudeUA     string
	claudeUAOnce sync.Once
)

func claudeUserAgent() string {
	claudeUAOnce.Do(func() {
		out, err := exec.Command("claude", "--version").CombinedOutput()
		ver := strings.TrimSpace(string(out))
		ver = strings.TrimSuffix(ver, "(Claude Code)")
		ver = strings.TrimSpace(ver)
		if err != nil || ver == "" {
			ver = "2.1.0"
		}
		claudeUA = "claude-code/" + ver
	})
	return claudeUA
}

func fetchClaudeOAuth(ctx context.Context) (*usage.QuotaInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	raw, err := os.ReadFile(credPath)
	if err != nil {
		return nil, err
	}
	var cred struct {
		ClaudeAiOauth *struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &cred); err != nil {
		return nil, err
	}
	if cred.ClaudeAiOauth == nil || cred.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("no oauth access token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.ClaudeAiOauth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2-0-2025-04-20")
	req.Header.Set("User-Agent", claudeUserAgent())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth usage http %d", resp.StatusCode)
	}
	return usage.FromClaudeOAuthUsageJSON(body)
}
