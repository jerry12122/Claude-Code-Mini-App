//go:build !windows

package proc

import (
	"fmt"
	"syscall"
	"time"
)

// SysProcAttr 回傳 Unix 專屬的進程屬性。
// Setpgid: true 讓子進程建立獨立的 process group，
// 使 KillTree / SendInterrupt 能用負 PID 一次終止整棵進程樹。
func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// KillTree 對進程群組發送 SIGKILL，強制終止所有子孫進程。
func KillTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("SIGKILL process group -%d: %w", pid, err)
	}
	return nil
}

// SendInterrupt 對進程群組發送 SIGINT，讓進程有機會優雅退出。
func SendInterrupt(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGINT); err != nil {
		return fmt.Errorf("SIGINT process group -%d: %w", pid, err)
	}
	return nil
}

// GracefulStop 先送 SIGINT，給進程 timeout 時間優雅退出；
// 逾時後才 SIGKILL 強制終止整棵進程樹。
func GracefulStop(pid int, timeout time.Duration) error {
	if err := SendInterrupt(pid); err != nil {
		return KillTree(pid)
	}
	go func() {
		time.Sleep(timeout)
		_ = KillTree(pid)
	}()
	return nil
}
