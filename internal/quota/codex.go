package quota

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type CodexFetcher struct{}

func (f *CodexFetcher) Provider() string { return agent.TypeCodex }

func (f *CodexFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	codexBin, err := codex.ResolveBin()
	if err != nil {
		return codexSnapshot(nil), nil
	}
	args := []string{
		"exec", "--json", "--skip-git-repo-check",
		"--yolo",
		"Reply with ONLY your current Codex usage limits as plain text. Include 5-hour and weekly percent used if available.",
	}
	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Env = codexEnv()
	cmd.SysProcAttr = proc.SysProcAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexSnapshot(nil), nil
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexSnapshot(nil), nil
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return codexSnapshot(nil), nil
	}
	_ = stdin.Close()

	info := parseCodexQuotaJSONL(stdout)
	_ = cmd.Wait()

	return codexSnapshot(info), nil
}

func codexSnapshot(info *usage.QuotaInfo) Snapshot {
	if info == nil {
		info = &usage.QuotaInfo{Provider: agent.TypeCodex, Source: "codex exec"}
	}
	return Snapshot{
		Provider:    agent.TypeCodex,
		DisplayText: FormatDisplay(agent.TypeCodex, info),
		UpdatedAt:   time.Now(),
	}
}

func parseCodexQuotaJSONL(r io.Reader) *usage.QuotaInfo {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var agentText strings.Builder
	var turnUsage *codex.TurnUsage

	for sc.Scan() {
		line := sc.Bytes()
		ev, err := codex.ParseEvent(line)
		if err != nil {
			continue
		}
		if ev.Type == "item.completed" {
			if text := codex.AgentMessageText(ev.Item); text != "" {
				agentText.WriteString(text)
			}
		}
		if ev.Type == "turn.completed" && ev.Usage != nil {
			turnUsage = ev.Usage
		}
	}

	if agentText.Len() > 0 {
		if info := usage.FromCodexStatusText(agentText.String()); info != nil && len(info.Windows) > 0 {
			return info
		}
	}
	if turnUsage != nil {
		return usage.FromCodexTurnUsage(turnUsage.InputTokens, turnUsage.OutputTokens)
	}
	return nil
}

func codexEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TERM=") {
			continue
		}
		out = append(out, e)
	}
	return out
}
