package quota

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type KiroFetcher struct{}

func (f *KiroFetcher) Provider() string { return agent.TypeKiro }

func (f *KiroFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	cmd := exec.CommandContext(ctx, "kiro-cli", "chat", "/usage", "--no-interactive")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = nil
	cmd.SysProcAttr = proc.SysProcAttr()
	_ = cmd.Run()
	info := usage.FromKiroUsageText(stderr.String())
	return Snapshot{
		Provider:    agent.TypeKiro,
		DisplayText: FormatDisplay(agent.TypeKiro, info),
		UpdatedAt:   time.Now(),
	}, nil
}
