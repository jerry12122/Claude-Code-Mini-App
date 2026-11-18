//go:build windows

package sysmon

import (
	"testing"
	"time"
)

// 真的呼叫 kernel32：確認結構大小與回傳欄位沒有填錯（解析函式的測試涵蓋不到這段）。
func TestReadSystemWindows(t *testing.T) {
	s := NewSampler()
	first, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.CPUPercent != nil {
		t.Fatalf("第一次應無基準，got %v", *first.CPUPercent)
	}
	if first.MemTotalBytes == 0 || first.MemUsedBytes == 0 || first.MemUsedBytes > first.MemTotalBytes {
		t.Fatalf("記憶體數值不合理: %+v", first)
	}
	time.Sleep(300 * time.Millisecond)
	second, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if second.CPUPercent == nil || *second.CPUPercent < 0 || *second.CPUPercent > 100 {
		t.Fatalf("第二次 cpu 應在 0–100，got %v", second.CPUPercent)
	}
}
