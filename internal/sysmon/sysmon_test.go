package sysmon

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    cpuTimes
		wantErr bool
	}{
		// idle = idle + iowait；total = user..steal 八欄相加（guest 已含在 user，不重複計）
		{"完整欄位", "cpu  100 10 50 800 20 5 5 10 0 0\ncpu0 1 1 1 1 1 1 1 1 0 0\n", cpuTimes{idle: 820, total: 1000}, false},
		{"舊核心只有四欄", "cpu 100 10 50 800\n", cpuTimes{idle: 800, total: 960}, false},
		{"欄位不足", "cpu 100 10\n", cpuTimes{}, true},
		{"沒有彙總列", "cpu0 1 1 1 1\nintr 5\n", cpuTimes{}, true},
		{"空輸入", "", cpuTimes{}, true},
		{"非數字", "cpu a b c d\n", cpuTimes{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseProcStat(strings.NewReader(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestParseMeminfo(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    memory
		wantErr bool
	}{
		{"正常（kB 換成 bytes）", "MemTotal:       16000 kB\nMemFree:  100 kB\nMemAvailable:    4000 kB\n", memory{used: 12000 * 1024, total: 16000 * 1024}, false},
		{"缺 MemAvailable", "MemTotal: 16000 kB\nMemFree: 100 kB\n", memory{}, true},
		{"缺 MemTotal", "MemAvailable: 4000 kB\n", memory{}, true},
		{"MemTotal 為零", "MemTotal: 0 kB\nMemAvailable: 0 kB\n", memory{}, true},
		{"available 大於 total 時不得下溢", "MemTotal: 100 kB\nMemAvailable: 200 kB\n", memory{used: 0, total: 100 * 1024}, false},
		{"空輸入", "", memory{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMeminfo(strings.NewReader(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestCPUPercent(t *testing.T) {
	tests := []struct {
		name   string
		prev   cpuTimes
		cur    cpuTimes
		want   float64
		wantOK bool
	}{
		{"一半忙碌", cpuTimes{idle: 50, total: 100}, cpuTimes{idle: 100, total: 200}, 50, true},
		{"全閒置", cpuTimes{idle: 0, total: 0}, cpuTimes{idle: 100, total: 100}, 0, true},
		{"全忙碌", cpuTimes{idle: 10, total: 10}, cpuTimes{idle: 10, total: 110}, 100, true},
		{"兩次取樣之間 total 沒增加（除以零）", cpuTimes{idle: 5, total: 10}, cpuTimes{idle: 5, total: 10}, 0, false},
		{"total 計數回繞", cpuTimes{idle: 5, total: 1000}, cpuTimes{idle: 6, total: 10}, 0, false},
		{"idle 計數回繞", cpuTimes{idle: 500, total: 1000}, cpuTimes{idle: 10, total: 2000}, 0, false},
		{"idle 增量大於 total 增量時夾在 0", cpuTimes{idle: 0, total: 0}, cpuTimes{idle: 120, total: 100}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cpuPercent(tc.prev, tc.cur)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Fatalf("got (%v, %v) want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSamplerFirstCallHasNoCPUBaseline(t *testing.T) {
	seq := []cpuTimes{{idle: 50, total: 100}, {idle: 100, total: 200}}
	var i int
	s := &Sampler{read: func() (cpuTimes, memory, error) {
		c := seq[i]
		i++
		return c, memory{used: 1, total: 4}, nil
	}}

	first, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.CPUPercent != nil {
		t.Fatalf("第一次沒有基準，cpu 應為 nil，got %v", *first.CPUPercent)
	}
	if first.MemUsedBytes != 1 || first.MemTotalBytes != 4 || first.SampledAt.IsZero() {
		t.Fatalf("記憶體與時間戳不該受 CPU 基準影響: %+v", first)
	}

	second, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if second.CPUPercent == nil || *second.CPUPercent != 50 {
		t.Fatalf("第二次應為 50，got %v", second.CPUPercent)
	}
}

func TestSamplerCounterWrapRebaselines(t *testing.T) {
	seq := []cpuTimes{{idle: 500, total: 1000}, {idle: 1, total: 10}, {idle: 6, total: 20}}
	var i int
	s := &Sampler{read: func() (cpuTimes, memory, error) {
		c := seq[i]
		i++
		return c, memory{}, nil
	}}
	_, _ = s.Snapshot()
	if st, _ := s.Snapshot(); st.CPUPercent != nil {
		t.Fatalf("回繞那次應回 nil，got %v", *st.CPUPercent)
	}
	// 回繞後以新值當基準，下一次要能恢復正常計算：idle +5、total +10 → 50%
	if st, _ := s.Snapshot(); st.CPUPercent == nil || *st.CPUPercent != 50 {
		t.Fatalf("回繞後下一次應以新基準算出 50，got %v", st.CPUPercent)
	}
}

func TestSamplerPropagatesUnsupported(t *testing.T) {
	s := &Sampler{read: func() (cpuTimes, memory, error) { return cpuTimes{}, memory{}, ErrUnsupported }}
	if _, err := s.Snapshot(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err=%v，預期 ErrUnsupported", err)
	}
}

// 第一次 cpu_percent 必須序列化成 JSON null，而不是 0 或被省略。
func TestStatsJSONNullCPU(t *testing.T) {
	b, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"cpu_percent":null`) {
		t.Fatalf("got %s", b)
	}
}
