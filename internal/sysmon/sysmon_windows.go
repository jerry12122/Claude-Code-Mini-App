//go:build windows

package sysmon

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows 沒有 GetSystemTimes／GlobalMemoryStatusEx 的封裝，這裡自己呼叫 kernel32；
// 用 NewLazySystemDLL 只從 System32 載入，避免 DLL 搜尋路徑被劫持。
var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx 對應 MEMORYSTATUSEX。
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func filetime(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

func readSystem() (cpuTimes, memory, error) {
	var idle, kernel, user windows.Filetime
	if r, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)),
	); r == 0 {
		return cpuTimes{}, memory{}, fmt.Errorf("GetSystemTimes: %w", err)
	}
	// kernel 時間已包含 idle，所以 total = kernel + user，不再加 idle。
	c := cpuTimes{idle: filetime(idle), total: filetime(kernel) + filetime(user)}

	ms := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r == 0 {
		return cpuTimes{}, memory{}, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	if ms.TotalPhys == 0 || ms.AvailPhys > ms.TotalPhys {
		return cpuTimes{}, memory{}, fmt.Errorf("GlobalMemoryStatusEx: 回傳值不合理 total=%d avail=%d", ms.TotalPhys, ms.AvailPhys)
	}
	return c, memory{used: ms.TotalPhys - ms.AvailPhys, total: ms.TotalPhys}, nil
}
