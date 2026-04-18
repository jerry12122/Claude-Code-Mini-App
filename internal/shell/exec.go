package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Result 為單次指令執行結果。
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run 在 workDir 執行一行指令（Windows: cmd /C；其他: sh -c）。
func Run(ctx context.Context, workDir, line string) (*Result, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("指令為空")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", line)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", line)
	}
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}
