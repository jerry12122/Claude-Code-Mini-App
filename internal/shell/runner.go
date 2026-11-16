package shell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

// MaxLineBytes is the max bytes per output line (plan: 4096).
const MaxLineBytes = 4096

// Type identifies the local shell for UI labels.
type Type string

const (
	TypePowerShell Type = "powershell"
	TypeBash       Type = "bash"
	TypeSh         Type = "sh"
)

// DetectType returns the shell kind for this OS.
func DetectType() Type {
	if runtime.GOOS == "windows" {
		return TypePowerShell
	}
	if st, err := os.Stat("/bin/bash"); err == nil && !st.IsDir() {
		return TypeBash
	}
	return TypeSh
}

// RunOptions configures a one-shot shell command.
type RunOptions struct {
	Command string
	WorkDir string
	Timeout int // seconds; default 60
}

// EventType is a stream event kind.
type EventType string

const (
	EventDeltaStdout EventType = "delta_stdout"
	EventDeltaStderr EventType = "delta_stderr"
	EventDone        EventType = "done"
	EventError       EventType = "error"
)

// Event is passed to the Run callback.
type Event struct {
	Type     EventType
	Text     string
	ExitCode int
}

func validateWorkDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("work directory is required")
	}
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return "", fmt.Errorf("invalid work directory: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("work directory not accessible: %w", err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("work directory must be a directory")
	}
	return abs, nil
}

func buildCmd(ctx context.Context, command, workDir string) (*exec.Cmd, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("empty command")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Force UTF-8 output so Go can read bytes correctly (Windows defaults to system codepage, e.g. CP950).
		wrapped := `$OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; ` + command
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wrapped)
	} else if _, err := os.Stat("/bin/bash"); err == nil {
		cmd = exec.CommandContext(ctx, "/bin/bash", "-c", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	cmd.Dir = workDir
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.KillTree(cmd.Process.Pid)
		}
		return nil
	}
	return cmd, nil
}

func readLineLimited(br *bufio.Reader, max int) (string, error) {
	var b []byte
	for {
		c, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				if len(b) == 0 {
					return "", io.EOF
				}
				return string(b), io.EOF
			}
			return string(b), err
		}
		if c == '\n' {
			return string(append(b, '\n')), nil
		}
		if len(b) < max-4 {
			b = append(b, c)
			continue
		}
		b = append(b, '.', '.', '.', '\n')
		for {
			c2, err2 := br.ReadByte()
			if err2 != nil {
				if err2 == io.EOF {
					return string(b), io.EOF
				}
				return string(b), err2
			}
			if c2 == '\n' {
				return string(b), nil
			}
		}
	}
}

func streamPipe(r io.ReadCloser, typ EventType, cb func(Event)) {
	defer r.Close()
	br := bufio.NewReader(r)
	for {
		line, err := readLineLimited(br, MaxLineBytes)
		if line != "" {
			cb(Event{Type: typ, Text: line})
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			cb(Event{Type: EventError, Text: err.Error()})
			return
		}
	}
}

// Run executes the command in opts.WorkDir (Cmd.Dir), streams stdout/stderr, and honors ctx cancel.
func Run(ctx context.Context, opts RunOptions, cb func(Event)) error {
	workDir, err := validateWorkDir(opts.WorkDir)
	if err != nil {
		cb(Event{Type: EventError, Text: err.Error()})
		return err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd, err := buildCmd(ctx, opts.Command, workDir)
	if err != nil {
		cb(Event{Type: EventError, Text: err.Error()})
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cb(Event{Type: EventError, Text: err.Error()})
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cb(Event{Type: EventError, Text: err.Error()})
		return err
	}

	if err := cmd.Start(); err != nil {
		cb(Event{Type: EventError, Text: err.Error()})
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		streamPipe(stdout, EventDeltaStdout, cb)
	}()
	go func() {
		defer wg.Done()
		streamPipe(stderr, EventDeltaStderr, cb)
	}()
	wg.Wait()

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if ctx.Err() != nil {
			exitCode = -1
			if ctx.Err() == context.DeadlineExceeded {
				cb(Event{Type: EventError, Text: fmt.Sprintf("timeout after %ds", timeout)})
			}
		} else if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			cb(Event{Type: EventError, Text: waitErr.Error()})
			return waitErr
		}
	}
	cb(Event{Type: EventDone, ExitCode: exitCode})
	return nil
}
