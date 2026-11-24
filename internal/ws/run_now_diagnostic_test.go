package ws

import (
	"context"
	"encoding/json"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

const (
	diagPlanned = iota
	diagRunEntry
	diagPlanTaken
	diagStartBefore
	diagStartAfter
	diagDeltaBefore
	diagDeltaAfter
	diagDoneBefore
	diagDoneAfter
	diagReady
	diagReturned
	diagPhaseCount
)

var runNowDiagnosticRig atomic.Uint64
var runNowDiagnosticNames = [diagPhaseCount]string{
	"planned", "run_entry", "plan_taken", "stream_start_before", "stream_start_after",
	"delta_before", "delta_after", "linger_done_before", "linger_done_after", "ready_closed", "run_returning",
}

// 僅記測試身份與相對單調時間；成功路徑沒有輸出或磁碟 IO。
type runNowDiagnostic struct {
	test    string
	rig     uint64
	start   time.Time
	entries atomic.Int64
}

type runNowTurnDiagnostic struct {
	owner *runNowDiagnostic
	turn  int
	at    [diagPhaseCount]atomic.Int64
}

func newDiagnosticRunNowRunner(t *testing.T) *runNowRunner {
	return &runNowRunner{plans: make(chan *runNowTurn, 16), diagnostic: &runNowDiagnostic{
		test: t.Name(), rig: runNowDiagnosticRig.Add(1), start: time.Now(),
	}}
}

func (d *runNowDiagnostic) enter() int64 {
	d.entries.Add(1)
	return time.Since(d.start).Nanoseconds() + 1
}

func (d *runNowTurnDiagnostic) mark(phase int) {
	d.at[phase].Store(time.Since(d.owner.start).Nanoseconds() + 1)
}

func runNowReadyClosed(p *runNowTurn) bool {
	select {
	case <-p.ready:
		return true
	default:
		return false
	}
}

func runNowDiagnosticSnapshot(p *runNowTurn) map[string]any {
	d := p.diagnostic
	phases := make(map[string]any, diagPhaseCount)
	for i, name := range runNowDiagnosticNames {
		phases[name] = "unknown"
		if ns := d.at[i].Load(); ns != 0 {
			phases[name] = ns - 1
		}
	}
	return map[string]any{
		"test": d.owner.test, "rig": d.owner.rig, "turn": d.turn,
		"runner_entries": d.owner.entries.Load(), "ready_closed": runNowReadyClosed(p),
		"phase_ns": phases, "snapshot_ns": time.Since(d.owner.start).Nanoseconds(),
		"limit": "原子欄位逐一讀取，非原子快照；unknown僅代表未觀測，run_returning不等於產品defer完成",
	}
}

func logRunNowTimeout(t *testing.T, p *runNowTurn, waitStarted time.Time) {
	t.Helper()
	snapshot := runNowDiagnosticSnapshot(p)
	snapshot["waiting_test"] = t.Name()
	snapshot["wait_start_ns"] = waitStarted.Sub(p.diagnostic.owner.start).Nanoseconds()
	snapshot["wait_elapsed_ns"] = time.Since(waitStarted).Nanoseconds()
	snapshot["deadline_ns"] = int64(time.Second)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Logf("RUN_NOW_DIAGNOSTIC marshal_error=%T", err)
	} else {
		t.Logf("RUN_NOW_DIAGNOSTIC %s", data)
	}
	// 僅超時取一次有界 stack；不查 DB、gate，也不取 callback 使用的鎖。
	buf := make([]byte, 64<<10)
	n := runtime.Stack(buf, true)
	t.Logf("RUN_NOW_STACK bytes=%d possibly_truncated=%t\n%s", n, n == len(buf), buf[:n])
}

func TestRunNowDiagnosticPhases(t *testing.T) {
	for _, blocked := range []agent.EventType{agent.EventStreamStart, agent.EventDelta, ""} {
		t.Run(string(blocked)+"callback", func(t *testing.T) {
			r := newDiagnosticRunNowRunner(t)
			x := &runNowRig{r: r}
			p := x.plan("")
			before := runNowDiagnosticSnapshot(p)
			if before["test"] != t.Name() || before["rig"] != r.diagnostic.rig || before["turn"] != 1 || before["runner_entries"] != int64(0) || before["ready_closed"] != false || before["phase_ns"].(map[string]any)["run_entry"] != "unknown" {
				t.Fatalf("未Run身份不符：%v", before)
			}
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() { close(release); p.release(); <-returned; cancel() })
			go func() {
				defer close(returned)
				_ = r.Run(ctx, agent.RunOptions{}, func(e agent.Event) {
					if e.Type == blocked {
						close(entered)
						<-release
					}
				})
			}()
			if blocked == "" {
				readyRunNow(t, p)
			} else {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("可控 callback 未進入")
				}
			}
			s := runNowDiagnosticSnapshot(p)
			phases := s["phase_ns"].(map[string]any)
			if s["runner_entries"] != int64(1) || phases["run_entry"] == "unknown" || phases["plan_taken"] == "unknown" {
				t.Fatalf("Run身份/取plan未被觀測：%v", s)
			}
			if blocked == "" {
				if s["ready_closed"] != true {
					t.Fatalf("ready通道未關閉：%v", s)
				}
			} else {
				prefix := "stream_start"
				if blocked == agent.EventDelta {
					prefix = "delta"
				}
				if phases[prefix+"_before"] == "unknown" || phases[prefix+"_after"] != "unknown" || s["ready_closed"] != false {
					t.Fatalf("callback阻擋位置不符：%v", s)
				}
			}
		})
	}
}
