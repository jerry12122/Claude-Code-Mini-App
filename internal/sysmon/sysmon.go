// Package sysmon 取得整機 CPU／記憶體的即時快照（唯讀、無狀態儲存，只在記憶體保留上一次 CPU 取樣）。
//
// 平台差異用檔名後綴處理：Windows 呼叫 kernel32，Linux 讀 /proc，其餘平台回 ErrUnsupported。
package sysmon

import (
	"errors"
	"sync"
	"time"
)

// ErrUnsupported 表示此平台沒有實作系統資源取樣。
var ErrUnsupported = errors.New("sysmon: 此平台不支援系統資源監控")

// Stats 是單次快照。CPUPercent 為 nil 代表沒有可比較的基準（第一次呼叫、或計數器回繞），
// 序列化成 JSON null，避免前端把「不知道」誤顯示成 0%。
type Stats struct {
	CPUPercent    *float64  `json:"cpu_percent"`
	MemUsedBytes  uint64    `json:"mem_used_bytes"`
	MemTotalBytes uint64    `json:"mem_total_bytes"`
	SampledAt     time.Time `json:"sampled_at"`
}

// cpuTimes 是某一刻的 CPU 累計時間（單位不重要，只用差值）。total 含 idle。
type cpuTimes struct{ idle, total uint64 }

type memory struct{ used, total uint64 }

// Sampler 保留上一次 CPU 取樣，用前後差值算百分比。可並行呼叫。
type Sampler struct {
	mu   sync.Mutex
	prev *cpuTimes
	read func() (cpuTimes, memory, error)
}

// NewSampler 回傳使用目前平台取樣實作的 Sampler。
func NewSampler() *Sampler { return &Sampler{read: readSystem} }

// Snapshot 取一次快照。同時有多個呼叫者時，彼此共用基準，間隔太短的那次可能回 nil。
func (s *Sampler) Snapshot() (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, mem, err := s.read()
	if err != nil {
		return Stats{}, err
	}
	var pct *float64
	if s.prev != nil {
		if v, ok := cpuPercent(*s.prev, cur); ok {
			pct = &v
		}
	}
	// 即使這次算不出來（回繞）也要換基準，下一次才能恢復。
	s.prev = &cur
	return Stats{CPUPercent: pct, MemUsedBytes: mem.used, MemTotalBytes: mem.total, SampledAt: time.Now()}, nil
}

// cpuPercent 由兩次取樣算出忙碌百分比（0–100）。
// 算不出來時回 false：total 沒增加（除以零）或任一計數器變小（回繞／重置）。
func cpuPercent(prev, cur cpuTimes) (float64, bool) {
	if cur.total <= prev.total || cur.idle < prev.idle {
		return 0, false
	}
	dTotal := cur.total - prev.total
	dIdle := cur.idle - prev.idle
	if dIdle >= dTotal {
		return 0, true
	}
	return float64(dTotal-dIdle) * 100 / float64(dTotal), true
}
