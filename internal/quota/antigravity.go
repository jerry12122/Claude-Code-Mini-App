package quota

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

type AntigravityFetcher struct{}

func (f *AntigravityFetcher) Provider() string { return agent.TypeAntigravity }

func (f *AntigravityFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := runAgyUsageJSON(ctx)
	if err != nil {
		return Snapshot{Provider: agent.TypeAntigravity, DisplayText: "—"}, err
	}
	info, err := usage.FromAgyUsageJSON(raw)
	if err != nil {
		return Snapshot{Provider: agent.TypeAntigravity, DisplayText: "—"}, err
	}
	return snapshotFromInfo(agent.TypeAntigravity, info), nil
}

func runAgyUsageJSON(ctx context.Context) ([]byte, error) {
	bin := "agy"
	if b := strings.TrimSpace(os.Getenv("CC_AGY_BIN")); b != "" {
		bin = b
	}
	cmd := exec.CommandContext(ctx, bin, "-p", "/usage", "--output-format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return proc.KillTree(cmd.Process.Pid) }
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run agy usage: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
