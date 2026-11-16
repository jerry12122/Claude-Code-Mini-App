package quota

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type KiroFetcher struct{}

func (f *KiroFetcher) Provider() string { return agent.TypeKiro }

func (f *KiroFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// ponytail: V2 handles /usage locally; V3 sends it to the model in headless mode.
	info, err := readKiroQuota(exec.CommandContext(ctx, "kiro-cli", "chat", "--v2", "/usage", "--no-interactive"))
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return Snapshot{Provider: agent.TypeKiro, DisplayText: "—"}, fmt.Errorf("kiro quota: %w", err)
	}
	return snapshotFromInfo(agent.TypeKiro, info), nil
}

func readKiroQuota(cmd *exec.Cmd) (*usage.QuotaInfo, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return proc.KillTree(cmd.Process.Pid) }
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	info := usage.FromKiroUsageText(stdout.String())
	if len(info.Windows) == 0 {
		return nil, fmt.Errorf("no credits found in kiro-cli /usage output")
	}
	return info, nil
}
