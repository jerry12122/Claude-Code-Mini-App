package kiroacp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

// TestMain：FAKE_ACP_MODE 有值時，這個測試執行檔自己扮演 kiro-cli acp（假伺服器），
// 讓 Run 的重試／交接邏輯不需要真的 kiro-cli 就能驗證。
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_ACP_MODE"); mode != "" {
		fakeACP(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeACP(mode string) {
	if mode == "no-read" {
		time.Sleep(time.Hour) // 完全不讀 stdin：用來驗證寫入卡在 pipe 時的處理
	}
	// 每個進程啟動時在 state 檔追加 1 byte，檔案大小 = 這是第幾次嘗試。
	attempt := 1
	if p := os.Getenv("FAKE_ACP_STATE"); p != "" {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		f.WriteString("x")
		f.Close()
		if st, err := os.Stat(p); err == nil {
			attempt = int(st.Size())
		}
	}
	out := bufio.NewWriter(os.Stdout)
	kiroLog := func(msg string) { // 模擬 kiro-cli 寫自己的 log（含 ANSI 色碼）
		if p := os.Getenv("KIRO_CHAT_LOG_FILE"); p != "" {
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			fmt.Fprintf(f, "\x1b[2m2026-10-05T06:35:12Z\x1b[0m \x1b[31mERROR\x1b[0m agent::agent: response stream encountered an error err=%s\n", msg)
			f.Close()
		}
	}
	send := func(v any) { b, _ := json.Marshal(v); out.Write(append(b, '\n')); out.Flush() }
	update := func(u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1", "update": u}})
	}
	chunk := func() {
		update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "hi"}})
	}
	permRequest := func() {
		send(map[string]any{"jsonrpc": "2.0", "id": "p1", "method": "session/request_permission", "params": map[string]any{
			"sessionId": "s1", "toolCall": map[string]any{"toolCallId": "t1", "title": "write"},
			"options": []map[string]any{{"optionId": "y", "name": "Allow", "kind": "allow_once"}}}})
	}
	exitLog := "" // kiro 真實行為：log 要等進程優雅退出才 flush；模擬成 stdin 關閉後才寫
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) }
		failData := func(code int, data string) {
			e := map[string]any{"code": code, "message": "Internal error"}
			if data != "" {
				e["data"] = data
			}
			send(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": e})
		}
		switch req.Method {
		case "initialize":
			if mode == "init-hang" {
				time.Sleep(time.Hour) // 不能用 select{}：Go 會判定死結直接 crash
			}
			reply(map[string]any{})
			continue
		case "session/load":
			switch mode {
			case "replay-tool-hang":
				// 歷史回放裡有一個永遠沒有 completed 的工具（例如上次被中斷）
				update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "old", "title": "old", "status": "in_progress"})
			case "load-gone":
				failData(-32603, "Failed to start session: Session not found: s1")
				continue
			case "load-transient":
				if attempt == 1 {
					failData(-32603, "temporary boom")
					continue
				}
			}
			reply(map[string]any{"sessionId": "s1"})
			continue
		case "session/new":
			reply(map[string]any{"sessionId": "s1"})
			if mode == "no-read-prompt" {
				time.Sleep(time.Hour) // 之後完全不讀 stdin：大 prompt 會塞滿 pipe，寫入卡住
			}
			continue
		case "session/prompt":
		default:
			reply(map[string]any{"sessionId": "s1"})
			continue
		}

		switch mode {
		case "ok", "load-transient", "replay-ok":
			chunk()
			reply(map[string]any{})
		case "empty-then-ok":
			if attempt > 1 {
				chunk()
			}
			reply(map[string]any{})
		case "internal":
			kiroLog("EmptyResponse") // 真實情況：-32603 對應 kiro log 的 EmptyResponse
			failData(-32603, "")
		case "internal-data": // 只有 ACP error.data 帶原因，kiro log 沒有
			failData(-32603, "stream failed: EmptyResponse")
		case "internal-unknown": // -32603 但沒有任何可辨識的原因
			failData(-32603, "")
		case "internal-quota":
			kiroLog("kind: MonthlyLimitReached")
			failData(-32603, "")
		case "quota-flush-on-exit": // 原因只在進程退出後才寫進 log
			exitLog = "kind: MonthlyLimitReached"
			failData(-32603, "")
		case "data-long-quota": // 原因在很長的 error.data 尾端（Error() 會截斷顯示，分類不可受影響）
			failData(-32603, strings.Repeat("x", 2000)+"MonthlyLimitReached")
		case "refusal":
			reply(map[string]any{"stopReason": "refusal"})
		case "partial-internal":
			chunk()
			failData(-32603, "")
		case "bad-params":
			failData(-32602, "")
		case "empty":
			reply(map[string]any{})
		case "tool-running": // 本輪工具執行超過 idleLimit 仍靜默：不可被誤判為卡住
			update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "build", "status": "in_progress"})
			time.Sleep(900 * time.Millisecond)
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed"})
			chunk()
			reply(map[string]any{})
		case "perm-wait": // 等使用者授權超過 idleLimit：不可被誤判為卡住
			permRequest()
			if sc.Scan() && strings.Contains(sc.Text(), `"optionId":"y"`) {
				chunk()
			}
			reply(map[string]any{})
		case "stubborn": // 失敗後不理 stdin 關閉、也殺不掉（測試把 killTree 換成空操作）
			failData(-32603, "")
		case "ok-stubborn": // 回覆成功，但收尾時拒絕退出、也殺不掉
			chunk()
			reply(map[string]any{})
		case "perm-crash": // 等授權時進程直接死掉
			permRequest()
			time.Sleep(100 * time.Millisecond)
			os.Exit(3)
		case "hang", "replay-tool-hang":
			time.Sleep(time.Hour)
		}
	}
	// stdin 已關閉
	if exitLog != "" {
		kiroLog(exitLog)
	}
	if mode == "stubborn" || mode == "ok-stubborn" {
		if mode == "stubborn" {
			// 遲到的輸出：收尾後才送出的 update 與授權請求，不可再產生 callback 或讓使用者授權
			chunk()
			permRequest()
		}
		time.Sleep(time.Hour)
	}
}

type runResult struct {
	err       error
	parentErr error // Run 返回後父 ctx 的狀態：要證明「沒被父 ctx 取消」時用
	attempts  int
	events    []agent.Event
	asks      *int32 // 授權 callback 現在另開 goroutine，可能在 Run 返回後才跑完：用指標＋atomic 讀
}

func (r runResult) permAsks() int32 { return atomic.LoadInt32(r.asks) }

type fakeCfg struct {
	sid          string
	ctx          context.Context              // nil＝30 秒逾時的 Background
	setup        time.Duration                // 0＝5s
	permWait     time.Duration                // 0＝2s（permDrainWait）
	idle         time.Duration                // 0＝300ms
	noKill       bool                         // 把 killTree 換成空操作（模擬殺不掉）
	keepLeftover bool                         // 允許暫存 log 殘留（進程還活著時無法刪）
	perm         func(context.Context) string // 非 nil＝提供 RequestPermission（傳入的是這次 attempt 的 ctx）
	prompt       string                       // 空＝"p"
	onEvent      func(agent.Event)            // 每個事件都呼叫（在 Run 的 callback 內）
}

func runFake(t *testing.T, mode string) runResult { return runFakeCfg(t, mode, fakeCfg{}) }

func runFakeCfg(t *testing.T, mode string, cfg fakeCfg) runResult {
	t.Helper()
	state := filepath.Join(t.TempDir(), "attempts")
	tmp := t.TempDir() // 讓 os.TempDir() 指到這裡，才能斷言暫存 log 有沒有被清掉
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(k, tmp)
	}
	t.Setenv("FAKE_ACP_MODE", mode)
	t.Setenv("FAKE_ACP_STATE", state)

	oBin, oIdle, oTick, oSetup, oG, oGF, oKW, oKill, oPW := kiroBin, idleLimit, idleTick, setupLimit, exitGrace, exitGraceFail, killWait, killTree, permDrainWait
	kiroBin, idleLimit, idleTick = os.Args[0], 300*time.Millisecond, 20*time.Millisecond
	setupLimit = 5 * time.Second
	if cfg.setup > 0 {
		setupLimit = cfg.setup
	}
	if cfg.idle > 0 {
		idleLimit = cfg.idle
	}
	exitGrace, exitGraceFail, killWait = 300*time.Millisecond, 300*time.Millisecond, 300*time.Millisecond
	permDrainWait = 2 * time.Second
	if cfg.permWait > 0 {
		permDrainWait = cfg.permWait
	}
	if cfg.noKill {
		killTree = func(int) error { return nil }
	}
	t.Cleanup(func() {
		kiroBin, idleLimit, idleTick, setupLimit, exitGrace, exitGraceFail, killWait, killTree, permDrainWait = oBin, oIdle, oTick, oSetup, oG, oGF, oKW, oKill, oPW
	})

	asks := new(int32)
	res := runResult{asks: asks}
	var pids []int
	opts := agent.RunOptions{Prompt: "p", WorkDir: t.TempDir(), SessionID: cfg.sid, OnStart: func(pid int) { pids = append(pids, pid) }}
	if cfg.prompt != "" {
		opts.Prompt = cfg.prompt
	}
	if cfg.perm != nil {
		opts.RequestPermission = func(ctx context.Context, _ agent.PermissionRequest) string {
			atomic.AddInt32(asks, 1)
			return cfg.perm(ctx)
		}
	}
	if cfg.noKill { // 只有刻意殺不掉的案例才手動清掉殘留的假進程
		t.Cleanup(func() {
			for _, pid := range pids {
				_ = proc.KillTree(pid)
			}
		})
	}
	ctx := cfg.ctx
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	res.err = (&Runner{}).Run(ctx, opts, func(e agent.Event) {
		res.events = append(res.events, e)
		if cfg.onEvent != nil {
			cfg.onEvent(e)
		}
	})
	res.parentErr = ctx.Err()
	if st, err := os.Stat(state); err == nil {
		res.attempts = int(st.Size())
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "kiro-acp-*.log")); len(left) > 0 && !cfg.keepLeftover {
		t.Errorf("暫存 kiro log 應被清掉，殘留: %v", left)
	}
	return res
}

func (r runResult) count(tp agent.EventType) (n int) {
	for _, e := range r.events {
		if e.Type == tp {
			n++
		}
	}
	return
}

func TestRun_Resilience(t *testing.T) {
	cases := []struct {
		mode         string
		wantHandoff  bool
		wantErr      bool
		wantAttempts int
		wantDone     int
		wantErrEvent int
	}{
		{"ok", false, false, 1, 1, 0},
		{"empty-then-ok", false, false, 2, 1, 0},      // prompt 成功但空 → 重試一次成功，使用者無感
		{"internal", true, true, 1, 0, 0},             // -32603 + 本進程 log 有 EmptyResponse（同 session 不會好）→ 不重試，直接交接
		{"internal-data", true, true, 1, 0, 0},        // 只有 ACP error.data 帶 EmptyResponse（kiro log 沒有）→ 一樣直接交接
		{"internal-unknown", true, true, 2, 0, 0},     // -32603 但原因不明 → 沒證據是 session 壞掉，先重試一次，仍失敗才交接
		{"tool-running", false, false, 1, 1, 0},       // 本輪工具執行超過 idleLimit 仍靜默 → 不可誤判卡住（工具不設上限）
		{"internal-quota", false, true, 1, 0, 1},      // kiro log 顯示額度用盡 → 換會話也沒用，不交接，報真正原因
		{"quota-flush-on-exit", false, true, 1, 0, 1}, // log 要等進程優雅退出才寫出：先關 stdin 再讀，才看得到
		{"data-long-quota", false, true, 1, 0, 1},     // 原因在超長 error.data 尾端：分類用完整內容，不受顯示截斷影響
		{"refusal", false, true, 1, 0, 1},             // stopReason=refusal（內容過濾）→ 不重試不交接，直接告知
		{"empty", true, true, 2, 0, 0},                // 一直空 → 交接，不報「完成」
		{"hang", true, true, 2, 0, 0},                 // 卡住 → idle watchdog 砍掉重試，再卡 → 交接
		{"partial-internal", true, true, 1, 0, 0},     // 已有產出不重試（避免重複副作用），直接交接
		{"bad-params", false, true, 1, 0, 1},          // 不可重試的錯誤維持原行為：EventError + 原錯誤
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			r := runFake(t, c.mode)
			if got := errors.Is(r.err, agent.ErrHandoff); got != c.wantHandoff {
				t.Errorf("handoff=%v want %v (err=%v)", got, c.wantHandoff, r.err)
			}
			if c.mode == "hang" && !errors.Is(r.err, errIdleTimeout) {
				t.Errorf("hang 應由 idle watchdog 判定，而不是別的原因: %v", r.err)
			}
			if (r.err != nil) != c.wantErr {
				t.Errorf("err=%v wantErr=%v", r.err, c.wantErr)
			}
			if r.attempts != c.wantAttempts {
				t.Errorf("attempts=%d want %d", r.attempts, c.wantAttempts)
			}
			if n := r.count(agent.EventDone); n != c.wantDone {
				t.Errorf("done=%d want %d", n, c.wantDone)
			}
			if n := r.count(agent.EventError); n != c.wantErrEvent {
				t.Errorf("errorEvents=%d want %d", n, c.wantErrEvent)
			}
		})
	}
}

// 回歸：load 回放的歷史工具（永遠沒有 completed）不可讓 watchdog 失效。
func TestRun_LoadReplayToolDoesNotDisableWatchdog(t *testing.T) {
	r := runFakeCfg(t, "replay-tool-hang", fakeCfg{sid: "s1"})
	if !errors.Is(r.err, agent.ErrHandoff) || !errors.Is(r.err, errIdleTimeout) || r.attempts != 2 {
		t.Fatalf("err=%v attempts=%d，應判定卡住、重試一次後交接", r.err, r.attempts)
	}
}

// 等使用者授權超過 idleLimit 不可被判成卡住。
func TestRun_PermissionWaitNotStalled(t *testing.T) {
	r := runFakeCfg(t, "perm-wait", fakeCfg{perm: func(context.Context) string { time.Sleep(700 * time.Millisecond); return "y" }})
	if r.err != nil || r.attempts != 1 || r.count(agent.EventDone) != 1 || r.permAsks() != 1 {
		t.Fatalf("err=%v attempts=%d done=%d asks=%d", r.err, r.attempts, r.count(agent.EventDone), r.permAsks())
	}
}

// session/load：明確「Session not found」不重試同一個不存在的 SID，直接交接；一般暫時失敗仍重試一次。
func TestRun_LoadFailure(t *testing.T) {
	if r := runFakeCfg(t, "load-gone", fakeCfg{sid: "s1"}); !errors.Is(r.err, agent.ErrHandoff) || r.attempts != 1 {
		t.Fatalf("Session not found 應不重試直接交接: err=%v attempts=%d", r.err, r.attempts)
	}
	if r := runFakeCfg(t, "load-transient", fakeCfg{sid: "s1"}); r.err != nil || r.attempts != 2 || r.count(agent.EventDone) != 1 {
		t.Fatalf("暫時性 load 失敗應重試一次成功: err=%v attempts=%d", r.err, r.attempts)
	}
}

// 啟動階段（initialize）卡住要有逾時；逾時算可重試故障，重試仍卡住才交接。
func TestRun_SetupTimeout(t *testing.T) {
	r := runFakeCfg(t, "init-hang", fakeCfg{setup: 300 * time.Millisecond})
	if !errors.Is(r.err, agent.ErrHandoff) || !errors.Is(r.err, errSetupTimeout) || r.attempts != 2 {
		t.Fatalf("err=%v attempts=%d", r.err, r.attempts)
	}
}

// 使用者中止（父 ctx 取消）不可重試、不可交接；啟動階段與 prompt 階段都一樣。
func TestRun_ParentCancelNotRetried(t *testing.T) {
	for _, mode := range []string{"init-hang", "hang"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			time.AfterFunc(500*time.Millisecond, cancel)
			r := runFakeCfg(t, mode, fakeCfg{ctx: ctx, idle: time.Minute}) // idle 要大於取消時間，否則先被 watchdog 判成卡住
			if !errors.Is(r.err, context.Canceled) || errors.Is(r.err, agent.ErrHandoff) || r.attempts != 1 {
				t.Fatalf("err=%v attempts=%d", r.err, r.attempts)
			}
		})
	}
}

// prompt 寫入卡在 pipe（CLI 不讀 stdin、prompt 大於 pipe 容量）：原本 call 會卡在同步寫入、連 watchdog 都救不了。
// 現在 watchdog 判定卡住 → 取消 prompt → 收尾 close()/砍進程 → 重試一次 → 交接，整體要在有限時間內完成。
func TestRun_BlockedPromptWriteTripsWatchdog(t *testing.T) {
	start := time.Now()
	r := runFakeCfg(t, "no-read-prompt", fakeCfg{prompt: strings.Repeat("x", 4<<20)})
	if !errors.Is(r.err, agent.ErrHandoff) || !errors.Is(r.err, errIdleTimeout) || r.attempts != 2 {
		t.Fatalf("err=%v attempts=%d", r.err, r.attempts)
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("卡住的寫入不應拖長整體時間: %v", d)
	}
}

// prompt 已成功，但在等子進程退出（收尾）時使用者中斷：父 ctx 取消優先，回 ctx.Err()，不可再送 Done。
func TestRun_ParentCancelDuringTeardownWinsOverSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// ok-stubborn：回覆成功後不理 stdin 關閉，收尾會停在等退出；第一個 Delta 之後 100ms 使用者中斷。
	var once sync.Once
	r := runFakeCfg(t, "ok-stubborn", fakeCfg{ctx: ctx, onEvent: func(e agent.Event) {
		if e.Type == agent.EventDelta {
			once.Do(func() { time.AfterFunc(100*time.Millisecond, cancel) })
		}
	}})
	if !errors.Is(r.err, context.Canceled) || errors.Is(r.err, agent.ErrHandoff) {
		t.Fatalf("應回 context.Canceled: %v", r.err)
	}
	if r.count(agent.EventDone) != 0 {
		t.Fatal("使用者中斷後不可送 Done")
	}
	if r.count(agent.EventDelta) != 1 || r.attempts != 1 {
		t.Fatalf("delta=%d attempts=%d", r.count(agent.EventDelta), r.attempts)
	}
}

// 失敗後子進程殺不掉／無法確認退出：停止恢復（不重試、不交接），只回這個準確的退出失敗錯誤，
// 且收尾後遲到的 update 與授權請求不可再產生 callback 或打擾使用者。
// 父 ctx 全程仍活著（r.parentErr==nil），所以擋住遲到輸出的是這次 attempt 自己的 ctx，而不是父 ctx 取消。
func TestRun_ProcessNotExitedStopsRecovery(t *testing.T) {
	r := runFakeCfg(t, "stubborn", fakeCfg{noKill: true, keepLeftover: true, perm: func(context.Context) string { return "y" }})
	if r.parentErr != nil {
		t.Fatalf("測試前提：父 ctx 必須仍活著: %v", r.parentErr)
	}
	if !errors.Is(r.err, errProcAlive) || errors.Is(r.err, agent.ErrHandoff) {
		t.Fatalf("應回 errProcAlive 且不交接: %v", r.err)
	}
	if r.attempts != 1 {
		t.Fatalf("不可在舊進程可能還活著時重試: attempts=%d", r.attempts)
	}
	if n := r.count(agent.EventDelta) + r.count(agent.EventActivity); n != 0 {
		t.Fatalf("收尾後遲到的 update 不應產生 callback: %d", n)
	}
	if r.permAsks() != 0 {
		t.Fatalf("收尾後遲到的授權請求不應打擾使用者: %d", r.permAsks())
	}
	if n := r.count(agent.EventError); n != 0 {
		t.Fatalf("不應再 flush 本輪原本的錯誤（只顯示退出失敗這一個）: %d", n)
	}
}

// 成功回覆但收尾時拒絕退出＋殺不掉：不可宣告完成（EventDone 要等收尾成功才送），回 errProcAlive，不可再開下一輪。
func TestRun_SuccessButProcessNotExited(t *testing.T) {
	r := runFakeCfg(t, "ok-stubborn", fakeCfg{noKill: true, keepLeftover: true})
	if r.parentErr != nil {
		t.Fatalf("測試前提：父 ctx 必須仍活著: %v", r.parentErr)
	}
	if !errors.Is(r.err, errProcAlive) || errors.Is(r.err, agent.ErrHandoff) || r.attempts != 1 {
		t.Fatalf("err=%v attempts=%d", r.err, r.attempts)
	}
	if r.count(agent.EventDone) != 0 {
		t.Fatal("收尾失敗時不可送 EventDone")
	}
	if r.count(agent.EventDelta) != 1 {
		t.Fatalf("已串流的回覆內容仍應保留: delta=%d", r.count(agent.EventDelta))
	}
}

// 等授權時進程死掉：授權 callback 不可卡住 readLoop，這次 attempt 收尾時要能取消它的 ctx 釋放等待（父 ctx 仍活著）。
func TestRun_PermissionReleasedWhenAttemptEnds(t *testing.T) {
	var released int32
	r := runFakeCfg(t, "perm-crash", fakeCfg{perm: func(ctx context.Context) string {
		<-ctx.Done()
		atomic.AddInt32(&released, 1)
		return ""
	}})
	if r.parentErr != nil {
		t.Fatalf("測試前提：父 ctx 必須仍活著: %v", r.parentErr)
	}
	if !errors.Is(r.err, agent.ErrHandoff) || !errors.Is(r.err, errStdoutClosed) || r.attempts != 2 || r.permAsks() != 2 {
		t.Fatalf("err=%v attempts=%d asks=%d", r.err, r.attempts, r.permAsks())
	}
	// Run 返回時就必須已釋放（收尾會等授權 worker 結束），不是事後才慢慢釋放。
	if n := atomic.LoadInt32(&released); n != 2 {
		t.Fatalf("Run 返回時每次 attempt 的授權 callback 都應已釋放: released=%d", n)
	}
}

// 授權 callback 不遵守 ctx 取消（舊 attempt 的授權流程還卡著）：收尾確認不了 worker 結束，
// 就停止恢復——一般錯誤、不重試、不交接、不送 Done。
func TestRun_PermissionWorkerStuckStopsRecovery(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // 測試結束才放掉，避免洩漏 goroutine
	r := runFakeCfg(t, "perm-crash", fakeCfg{permWait: 300 * time.Millisecond, perm: func(context.Context) string {
		<-release // 故意無視 ctx
		return ""
	}})
	if r.parentErr != nil {
		t.Fatalf("測試前提：父 ctx 必須仍活著: %v", r.parentErr)
	}
	if !errors.Is(r.err, errWorkerStuck) || errors.Is(r.err, agent.ErrHandoff) || r.attempts != 1 || r.permAsks() != 1 {
		t.Fatalf("err=%v attempts=%d asks=%d", r.err, r.attempts, r.permAsks())
	}
	if r.count(agent.EventDone) != 0 || r.count(agent.EventError) != 0 {
		t.Fatalf("done=%d errorEvents=%d", r.count(agent.EventDone), r.count(agent.EventError))
	}
}

// idle 統計要包含「靜默到逾時／結束」的最後一段，否則卡住的那段不會出現，無法用來調 idleLimit。
func TestRun_IdleStatsIncludeTrailingGap(t *testing.T) {
	gaps := func(mode string) (max float64, inTool bool) {
		buf := &syncBuf{}
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
		defer slog.SetDefault(old)
		runFake(t, mode)
		for _, m := range regexp.MustCompile(`最長無動靜 ([0-9.]+)s（當時工具執行中=(true|false)）`).FindAllStringSubmatch(buf.String(), -1) {
			g, _ := strconv.ParseFloat(m[1], 64)
			if g > max {
				max, inTool = g, m[2] == "true"
			}
		}
		return
	}
	if g, _ := gaps("hang"); g < 0.25 { // idleLimit=300ms：卡到被判定逾時的那段
		t.Fatalf("hang 的最長無動靜應接近 idleLimit(0.3s)，got %.2fs", g)
	}
	if g, in := gaps("tool-running"); g < 0.8 || !in { // 工具靜默跑 900ms
		t.Fatalf("tool-running 應記到約 0.9s 且標記工具執行中，got %.2fs inTool=%v", g, in)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// 隔離：子進程寫的是 runner 指定的專屬 log，不碰呼叫端環境裡的共用 log，
// 共用 log 裡別人的額度錯誤也不會影響本輪判斷。
func TestRun_KiroLogIsolatedPerProcess(t *testing.T) {
	shared := t.TempDir() + "/shared.log"
	seed := "2026 ERROR agent::agent: response stream encountered an error err=MonthlyLimitReached (別的會話)\n"
	os.WriteFile(shared, []byte(seed), 0o600)
	t.Setenv("KIRO_CHAT_LOG_FILE", shared)
	r := runFake(t, "internal")
	if !errors.Is(r.err, agent.ErrHandoff) {
		t.Fatalf("共用 log 的額度錯誤不應擋下本輪交接: %v", r.err)
	}
	if b, _ := os.ReadFile(shared); string(b) != seed {
		t.Fatalf("子進程不應寫入共用 log: %q", b)
	}
}

func TestKiroErrors(t *testing.T) {
	long := strings.Repeat("x", 2000) // 原因在很後面：分類不可被截斷影響
	p := t.TempDir() + "/k.log"
	os.WriteFile(p, []byte("\x1b[2m2026\x1b[0m \x1b[31mERROR\x1b[0m agent::agent: response stream encountered an error err=Stream("+long+" kind: MonthlyLimitReached)\n"+
		"2026 ERROR kiro_telemetry_host::thread: upload failed status Some(403)\n"+ // 別的元件的 403，不可當成 kiro 帳號故障
		"2026 ERROR tool_manager: Error loading server chrome-devtools-mcp\n"+
		"2026 INFO fine\n"), 0o600)
	got := kiroErrors(p)
	if strings.Contains(got, "telemetry") || strings.Contains(got, "chrome-devtools") || strings.Contains(got, "\x1b") || !accountLimited(got) {
		t.Fatalf("got %q", got)
	}
	// 只有別的元件的 403：不算帳號故障
	os.WriteFile(p, []byte("2026 ERROR kiro_telemetry_host::thread: upload failed status Some(403)\n"), 0o600)
	if accountLimited(kiroErrors(p)) || accountLimited("err=EmptyResponse") {
		t.Fatal("非串流來源的錯誤不應被當成帳號層級故障")
	}
	if kiroErrors("") != "" || kiroErrors(p+".missing") != "" {
		t.Fatal("無 log 時應回空字串")
	}
}

// log 超過 1MiB 時只讀尾端：決定性的最後幾行要讀得到，且回傳行數有上限；
// 落在尾端範圍之外的舊行不會被採信（已知限制）。
func TestKiroErrors_TailOver1MiB(t *testing.T) {
	filler := strings.Repeat("2026 ERROR agent::agent: response stream encountered an error err=EmptyResponse\n", 20000) // ~1.6MiB
	quota := "2026 ERROR agent::agent: response stream encountered an error err=Stream(kind: MonthlyLimitReached)\n"
	p := t.TempDir() + "/big.log"

	os.WriteFile(p, []byte(filler+quota), 0o600)
	got := kiroErrors(p)
	if !accountLimited(got) || strings.Count(got, "\n")+1 > 20 {
		t.Fatalf("尾端的額度錯誤應被讀到且最多 20 行: lines=%d", strings.Count(got, "\n")+1)
	}

	os.WriteFile(p, []byte(quota+filler), 0o600)
	if got := kiroErrors(p); accountLimited(got) || !strings.Contains(got, "EmptyResponse") {
		t.Fatalf("超出尾端範圍的舊行不應被採信: %q", truncateBytes([]byte(got), 200))
	}
}

func TestRPCErrorIncludesData(t *testing.T) {
	e := &rpcError{Code: -32603, Message: "Internal error", Data: json.RawMessage(`"boom"`)}
	if s := e.Error(); !strings.Contains(s, `"boom"`) {
		t.Fatalf("error 應帶 data: %s", s)
	}
	if s := (&rpcError{Code: 1, Message: "m"}).Error(); s != fmt.Sprintf("jsonrpc error %d: %s", 1, "m") {
		t.Fatalf("無 data 時格式不應改變: %s", s)
	}
}
