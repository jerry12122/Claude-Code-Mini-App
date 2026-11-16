package quota

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestKiroQuota(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "empty", "failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestKiroQuotaProcess$")
			cmd.Env = append(os.Environ(), "QUOTA_TEST_KIRO_PROCESS="+mode)
			info, err := readKiroQuota(cmd)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				snap := snapshotFromInfo("kiro", info)
				if snap.Plan != "PRO+" || len(snap.Windows) != 1 || *snap.Windows[0].Used != 940.46 || *snap.Windows[0].Limit != 2000 || *snap.Windows[0].Percent != 47 {
					t.Fatalf("snapshot: %+v", snap)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if cmd.ProcessState == nil {
				t.Fatal("process not reaped")
			}
		})
	}
}

func TestKiroQuotaProcess(t *testing.T) {
	switch os.Getenv("QUOTA_TEST_KIRO_PROCESS") {
	case "":
		return
	case "success":
		fmt.Fprintln(os.Stdout, "Estimated Usage | resets on 2026-12-01 | KIRO PRO+\nCredits (940.46 of 2000 covered in plan), 47.0%")
	case "empty":
		fmt.Fprintln(os.Stdout, "I cannot execute /usage here.")
	case "failure":
		// Even parseable output must not hide a failed command.
		fmt.Fprintln(os.Stdout, "Credits (1 of 50), 2%")
		os.Exit(1)
	case "timeout":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}
