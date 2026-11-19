package ws

import (
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

func pct(v float64) *float64 { return &v }

// guardHarness 組一個帶額度守門的 dispatcher；start 只計數並把佇列項目升為 user 訊息。
type guardHarness struct {
	d               *sessionDispatcher
	db              *db.DB
	id              string
	starts          int
	errors          []string
	snap            quota.Snapshot
	percent         float64
	queueBroadcasts int
}

func newGuardHarness(t *testing.T, snap quota.Snapshot, percent float64) *guardHarness {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/guard.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := database.CreateSession("guard", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	h := &guardHarness{db: database, id: s.ID, snap: snap, percent: percent}
	h.d = &sessionDispatcher{
		database: database, sessionID: s.ID, actor: "owner",
		start: func(in clientMsg, q *db.QueuedMessage) error {
			if q != nil {
				if _, err := database.PromoteQueuedMessage(s.ID, *q); err != nil {
					return err
				}
			}
			h.starts++
			return nil
		},
		reply:          func(serverMsg) bool { return true },
		broadcastQueue: func() { h.queueBroadcasts++ },
		broadcast: func(m serverMsg) {
			if m.Type == "error" {
				h.errors = append(h.errors, m.Content)
			}
		},
		quotaGuard: newQuotaGuard(func() quota.Snapshot { return h.snap }, percent),
	}
	return h
}

func (h *guardHarness) enqueue(t *testing.T, texts ...string) {
	t.Helper()
	for _, x := range texts {
		if _, err := h.db.EnqueueMessage(h.id, x, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func snapWith(stale bool, ws ...quota.Window) quota.Snapshot {
	return quota.Snapshot{Provider: "claude", Windows: ws, Stale: stale}
}

func TestQuotaGuardOverThresholdPausesAndKeepsQueue(t *testing.T) {
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "session", Label: "5h", Percent: pct(95)}), 90)
	h.enqueue(t, "a", "b")
	h.d.Drain()
	if h.starts != 0 {
		t.Fatalf("starts=%d，超過門檻不應啟動", h.starts)
	}
	if n := queueLen(h.db, h.id); n != 2 {
		t.Fatalf("佇列應保留 2 則，got %d", n)
	}
	if paused, _ := h.db.QueuePaused(h.id); !paused {
		t.Fatal("應沿用 queue_paused 暫停")
	}
	s, err := h.db.GetSession(h.id)
	if err != nil || !strings.Contains(s.LastErrorText, "90") || !strings.Contains(s.LastErrorText, "95") {
		t.Fatalf("session 錯誤文字應含門檻與用量: %q err=%v", s.LastErrorText, err)
	}
	if len(h.errors) != 1 {
		t.Fatalf("應廣播一則 error 讓使用者看到原因，got %v", h.errors)
	}
	if h.queueBroadcasts == 0 {
		t.Fatal("暫停後應廣播佇列狀態，否則畫面上的佇列暫停指示不會更新")
	}
	// 再次 Drain 因已暫停不會重複寫入。
	h.d.Drain()
	if len(h.errors) != 1 || h.starts != 0 {
		t.Fatalf("已暫停不應重複處理: errors=%d starts=%d", len(h.errors), h.starts)
	}
}

func TestQuotaGuardExactlyAtThresholdBlocks(t *testing.T) {
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "week", Percent: pct(90)}), 90)
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 0 || queueLen(h.db, h.id) != 1 {
		t.Fatalf("達到門檻（>=）應擋: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
}

func TestQuotaGuardBelowThresholdStarts(t *testing.T) {
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "session", Percent: pct(40)}, quota.Window{Key: "week", Percent: pct(89.9)}), 90)
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 || queueLen(h.db, h.id) != 0 {
		t.Fatalf("低於門檻應照常啟動: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
	if paused, _ := h.db.QueuePaused(h.id); paused {
		t.Fatal("不應暫停")
	}
}

func TestQuotaGuardStaleOrNoDataPasses(t *testing.T) {
	cases := map[string]quota.Snapshot{
		"stale":   snapWith(true, quota.Window{Key: "session", Percent: pct(99)}),
		"no data": {Provider: "claude", DisplayText: "—"},
	}
	for name, snap := range cases {
		t.Run(name, func(t *testing.T) {
			h := newGuardHarness(t, snap, 90)
			h.enqueue(t, "a")
			h.d.Drain()
			if h.starts != 1 || queueLen(h.db, h.id) != 0 {
				t.Fatalf("fail-open 應放行: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
			}
		})
	}
}

func TestQuotaGuardIgnoresWindowsWithoutPercent(t *testing.T) {
	// cursor plan spend：只有金額，used 超過 limit 也不參與判斷。
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "plan_spend", Used: pct(33.75), Limit: pct(20)}), 90)
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 {
		t.Fatalf("無 percent 的視窗應忽略: starts=%d", h.starts)
	}
}

func TestQuotaGuardZeroDisables(t *testing.T) {
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "session", Percent: pct(100)}), 0)
	if h.d.quotaGuard != nil {
		t.Fatal("門檻 0 應停用（不建立守門）")
	}
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 {
		t.Fatalf("門檻 0 應停用: starts=%d", h.starts)
	}
}

func TestQuotaGuardDoesNotAffectUserDirectSend(t *testing.T) {
	over := snapWith(false, quota.Window{Key: "session", Percent: pct(99)})

	t.Run("with request id", func(t *testing.T) {
		h := newGuardHarness(t, over, 90)
		h.d.Dispatch(clientMsg{Type: "input", RequestID: "r1", Data: "hello"})
		if h.starts != 1 {
			t.Fatalf("使用者直接送出不應被擋: starts=%d", h.starts)
		}
		if paused, _ := h.db.QueuePaused(h.id); paused {
			t.Fatal("不應暫停")
		}
	})
	t.Run("legacy without request id", func(t *testing.T) {
		h := newGuardHarness(t, over, 90)
		h.d.Dispatch(clientMsg{Type: "input", Data: "hello"})
		if h.starts != 1 {
			t.Fatalf("使用者直接送出不應被擋: starts=%d", h.starts)
		}
	})
}

func TestQuotaGuardResumeBypassesGuard(t *testing.T) {
	h := newGuardHarness(t, snapWith(false, quota.Window{Key: "session", Percent: pct(95)}), 90)
	h.enqueue(t, "a", "b")
	h.d.Drain()
	if paused, _ := h.db.QueuePaused(h.id); !paused {
		t.Fatal("前置：應已暫停")
	}
	h.d.Resume()
	if h.starts != 1 || queueLen(h.db, h.id) != 1 {
		t.Fatalf("使用者按繼續應啟動下一則: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
}

// newRuntimeGuardHarness 的守門每次呼叫都讀 enabled／percent，用來模擬執行期改設定。
func newRuntimeGuardHarness(t *testing.T, snap quota.Snapshot) (h *guardHarness, enabled *bool, percent *float64) {
	t.Helper()
	on := true
	threshold := 90.0
	database, err := db.Open(t.TempDir() + "/guard-runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s, err := database.CreateSession("guard-runtime", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	h = &guardHarness{db: database, id: s.ID, snap: snap, percent: threshold}
	h.d = &sessionDispatcher{
		database: database, sessionID: s.ID, actor: "owner",
		start: func(in clientMsg, q *db.QueuedMessage) error {
			if q != nil {
				if _, err := database.PromoteQueuedMessage(s.ID, *q); err != nil {
					return err
				}
			}
			h.starts++
			return nil
		},
		reply:          func(serverMsg) bool { return true },
		broadcastQueue: func() { h.queueBroadcasts++ },
		broadcast: func(m serverMsg) {
			if m.Type == "error" {
				h.errors = append(h.errors, m.Content)
			}
		},
		quotaGuard: quotaGuardFromSettings(func() quota.Snapshot { return h.snap }, func() (bool, float64) {
			return on, threshold
		}),
	}
	return h, &on, &threshold
}

func TestQuotaGuardRuntimeThresholdChange(t *testing.T) {
	h, _, percent := newRuntimeGuardHarness(t, snapWith(false, quota.Window{Key: "session", Label: "5h", Percent: pct(50)}))
	*percent = 90
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 || queueLen(h.db, h.id) != 0 {
		t.Fatalf("門檻 90、已用 50%% 應啟動: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
	*percent = 40
	h.enqueue(t, "b")
	h.d.Drain()
	if h.starts != 1 {
		t.Fatalf("門檻改 40 後不應再啟動: starts=%d", h.starts)
	}
	if paused, _ := h.db.QueuePaused(h.id); !paused {
		t.Fatal("應暫停")
	}
	s, err := h.db.GetSession(h.id)
	if err != nil || !strings.Contains(s.LastErrorText, "50") || !strings.Contains(s.LastErrorText, "40") {
		t.Fatalf("原因應含 50%% 已用與 40%% 門檻: %q err=%v", s.LastErrorText, err)
	}
}

func TestQuotaGuardRuntimeDisableAllowsNextStart(t *testing.T) {
	h, enabled, percent := newRuntimeGuardHarness(t, snapWith(false, quota.Window{Key: "session", Percent: pct(95)}))
	*percent = 90
	if h.d.quotaGuard == nil || h.d.quotaGuard() == "" {
		t.Fatal("關閉前守門應會擋下 95%")
	}
	*enabled = false
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 || queueLen(h.db, h.id) != 0 {
		t.Fatalf("關閉守門後應照跑: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
	if paused, _ := h.db.QueuePaused(h.id); paused {
		t.Fatal("關閉守門不應暫停")
	}
}

func TestQuotaGuardRuntimeDirectSendAndResumeIgnoreGuard(t *testing.T) {
	over := snapWith(false, quota.Window{Key: "session", Percent: pct(95)})
	h, _, percent := newRuntimeGuardHarness(t, over)
	*percent = 10
	h.d.Dispatch(clientMsg{Type: "input", RequestID: "r1", Data: "hello"})
	if h.starts != 1 {
		t.Fatalf("直接送出不應被守門擋住: starts=%d", h.starts)
	}
	if paused, _ := h.db.QueuePaused(h.id); paused {
		t.Fatal("直接送出不應暫停")
	}

	h.enqueue(t, "a", "b")
	h.d.Drain()
	if h.starts != 1 || queueLen(h.db, h.id) != 2 {
		t.Fatalf("自動啟動應被 10%% 門檻暫停: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
	h.d.Resume()
	if h.starts != 2 || queueLen(h.db, h.id) != 1 {
		t.Fatalf("按繼續應不受守門影響: starts=%d queue=%d", h.starts, queueLen(h.db, h.id))
	}
}

func TestQuotaGuardRuntimeStaleAndNoPercentPass(t *testing.T) {
	h, _, percent := newRuntimeGuardHarness(t, snapWith(true, quota.Window{Key: "session", Percent: pct(99)}))
	*percent = 10
	h.enqueue(t, "a")
	h.d.Drain()
	if h.starts != 1 {
		t.Fatalf("stale 應放行: starts=%d", h.starts)
	}

	h.snap = snapWith(false, quota.Window{Key: "plan_spend", Used: pct(33), Limit: pct(20)})
	h.enqueue(t, "b")
	h.d.Drain()
	if h.starts != 2 {
		t.Fatalf("無 percent 的視窗應放行: starts=%d", h.starts)
	}
}
