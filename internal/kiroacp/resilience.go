package kiroacp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

// kiro-cli acp 偶爾會卡住不回、回 -32603 Internal error、或 prompt 成功但一字未出。
//
// 判斷只採信「這一輪」的證據：ACP 回應、子進程狀態，以及這個子進程專屬的 kiro log
// （runOnce 以 KIRO_CHAT_LOG_FILE 隔離，不會混進其他會話或工具的錯誤）。
//
// Run 的處置：
//   - 額度／節流類錯誤（kiro log 顯示）、stopReason=refusal → 換會話沒用，直接報原因
//   - kiro log 出現 EmptyResponse（-32603 的已知成因，同 session 會反覆發生）→ 不重試，直接交接
//   - 其他可重試故障（卡住／進程死掉／load 失敗／prompt 空回覆／原因不明的 -32603）
//     → 沒產出時同 session 重試一次，仍失敗才交接
//   - 已有部分產出 → 不重試（避免重複副作用），直接交接
//   - kiro 明確說 session 不存在（load 失敗 "Session not found"）→ 不重試同一個 SID，直接交接
//   - 啟動階段（initialize、session/new|load）有 setupLimit 逾時，算可重試故障；使用者中止（父 ctx 取消）一律不重試
//   - 失敗後若無法確認子進程已退出（errProcAlive）→ 停止恢復，不重試、不交接，只回一般錯誤；
//     收尾開始後遲到的 update 丟棄、授權請求直接拒絕
//
// 交接＝回傳 agent.ErrHandoff，由呼叫端（ws）建立新會話接手。

var (
	errIdleTimeout   = errors.New("kiro 長時間無任何回應")
	errSetupTimeout  = errors.New("kiro 啟動階段逾時")
	errEmptyResponse = errors.New("kiro 回應為空")
	errLoadFailed    = errors.New("kiro session/load 失敗")
	errRefusal       = errors.New("kiro 因內容過濾拒絕回應（stopReason=refusal）")
	// errProcAlive：失敗後無法確認 kiro-cli 子進程已退出。此時不能再重試或交接（舊進程可能還活著、還能動工具），只回一般錯誤。
	errProcAlive = errors.New("kiro 子進程無法確認已退出")
	// errWorkerStuck：授權 worker 在收尾後仍未結束（callback 不遵守 ctx 取消）。同樣不重試、不交接、不宣告完成。
	errWorkerStuck = errors.New("kiro 授權處理無法結束")
)

// ponytail: 門檻不是實測值（實測見 runOnce 結尾的「idle 統計」log，有資料後再調）。
// idleLimit 參考 Codex 的 stream 閒置逾時預設 300s（codex-rs model-provider-info：DEFAULT_STREAM_IDLE_TIMEOUT_MS）。
// 工具執行中不設上限（使用者決定）；代價是 kiro 的工具本身卡死（如 Kiro#11793 shell 工具無逾時）時，
// 只能靠使用者手動中斷。
// setupLimit 涵蓋 initialize 與 session/new|load（含 MCP server 啟動）：實測正常 load 約 5–7s，60s 僅是寬鬆的上限。
var (
	kiroBin       = "kiro-cli"
	idleLimit     = 300 * time.Second
	idleTick      = 5 * time.Second
	setupLimit    = 60 * time.Second
	exitGrace     = 5 * time.Second // 正常結束：關 stdin 後等進程自己退出
	exitGraceFail = 2 * time.Second // 失敗：給 kiro 把 log flush 完的時間，逾時就砍
	killWait      = 5 * time.Second // 砍了之後確認退出的上限
	permDrainWait = 2 * time.Second // 收尾後確認授權 worker 結束的上限（callback 應在 attempt ctx 取消後立刻返回）
	killTree      = proc.KillTree
	maxAttempts   = 2
)

func isRetriable(err error) bool {
	if errors.Is(err, errIdleTimeout) || errors.Is(err, errSetupTimeout) || errors.Is(err, errEmptyResponse) ||
		errors.Is(err, errStdoutClosed) || errors.Is(err, errLoadFailed) {
		return true
	}
	var re *rpcError
	return errors.As(err, &re) && re.Code == -32603
}

// newKiroLog 建立一個空的專屬 log 檔供單一 kiro-cli 子進程使用；失敗回空字串（＝不隔離、不分類）。
func newKiroLog() string {
	f, err := os.CreateTemp("", "kiro-acp-*.log")
	if err != nil {
		return ""
	}
	f.Close()
	return f.Name()
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// 只認「模型回應串流失敗」這兩種來源的 ERROR 行，避免同進程內其他元件（telemetry、MCP、工具的 HTTP）
// 的 403／429 被誤認成 kiro 帳號故障。
var streamErrMarkers = []string{"response stream encountered an error", "failed to send rts request"}

// kiroErrors 讀取子進程專屬 log 中模型回應串流失敗的 ERROR 行（去 ANSI；只讀尾端 1MiB、最多最後 20 行）。
// 回傳完整內容供分類，顯示時再截斷（原因常在行尾）。
func kiroErrors(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 1<<20 {
		_, _ = f.Seek(-(1 << 20), io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	var keep []string
next:
	for _, l := range strings.Split(ansiRe.ReplaceAllString(string(b), ""), "\n") {
		if !strings.Contains(l, " ERROR ") {
			continue
		}
		for _, m := range streamErrMarkers {
			if strings.Contains(l, m) {
				keep = append(keep, strings.TrimSpace(l))
				continue next
			}
		}
	}
	if len(keep) > 20 {
		keep = keep[len(keep)-20:]
	}
	return strings.Join(keep, "\n")
}

// accountLimited：帳號／額度層級的錯誤（用量上限、節流、權限）。換新會話也一樣會失敗，不該交接。
func accountLimited(kiroLog string) bool {
	for _, k := range []string{"MonthlyLimitReached", "Throttling", "Some(403)", "Some(429)"} {
		if strings.Contains(kiroLog, k) {
			return true
		}
	}
	return false
}

// Run 包裝 runOnce，見檔頭說明。
func (r *Runner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	var out atomic.Bool // 本次呼叫是否已對外送出內容（文字或工具活動）
	var held []agent.Event
	wrapped := func(e agent.Event) {
		switch e.Type {
		case agent.EventError:
			held = append(held, e) // 是否要讓使用者看到，等本輪結果決定
			return
		case agent.EventDelta:
			if e.Text != "" {
				out.Store(true)
			}
		case agent.EventActivity:
			out.Store(true)
		}
		cb(e)
	}
	flush := func() {
		for _, e := range held {
			cb(e)
		}
	}

	for attempt := 1; ; attempt++ {
		held = nil
		logPath := newKiroLog()
		var done agent.Event
		err := r.runOnce(ctx, opts, wrapped, &out, logPath, &done)
		kiroLog := ""
		if err != nil && ctx.Err() == nil {
			kiroLog = kiroErrors(logPath)
		}
		if logPath != "" {
			_ = os.Remove(logPath)
		}
		// 父 ctx（使用者中止）優先於一切：即使 prompt 已成功，只要在等收尾／退出時被中斷，就回 ctx.Err()、不送 Done。
		if ctx.Err() != nil {
			flush()
			return ctx.Err()
		}
		if err == nil {
			flush()
			if done.Type != "" { // 收尾已確認成功才宣告完成
				cb(done)
			}
			return nil
		}
		// 舊進程可能還活著：不能再開新一輪（會有兩個 kiro 同時動同一個 session／工作目錄），也不交接。
		// 只回這個準確的退出失敗錯誤，不再 flush 本輪原本暫存的錯誤（避免使用者看到兩個不同的錯誤）。
		if errors.Is(err, errProcAlive) || errors.Is(err, errWorkerStuck) {
			slog.Info(fmt.Sprintf("[kiroacp] 停止恢復: %v", err))
			return err
		}
		if kiroLog != "" {
			slog.Info(fmt.Sprintf("[kiroacp] 失敗時本進程 kiro log 的錯誤:\n%s", truncateBytes([]byte(kiroLog), 2000)))
		}
		if !isRetriable(err) {
			flush()
			return err
		}
		// 分類用完整證據（不先截斷）：ACP error.data + 本進程 kiro log 的串流錯誤行。
		evidence := err.Error() + "\n" + kiroLog
		var re *rpcError
		if errors.As(err, &re) {
			evidence += "\n" + string(re.Data)
		}
		if accountLimited(evidence) {
			err = fmt.Errorf("%w（kiro 帳號層級錯誤，換會話也沒用）:\n%s", err, truncateBytes([]byte(kiroLog), 600))
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		// EmptyResponse 是「換乾淨會話」的策略訊號（已知成因，同 session 會反覆發生），不是 session 永久損壞的證明。
		emptyResp := strings.Contains(evidence, "EmptyResponse")
		// kiro 明確說該 session 不存在：重試同一個不存在的 SID 只是再失敗一次，直接交接。
		sessionGone := errors.Is(err, errLoadFailed) && strings.Contains(strings.ToLower(evidence), "session not found")
		// 首次對話（opts.SessionID 為空）重試會重新 session/new：舊的沒有任何歷史，重建比載入空 session 乾淨。
		if !emptyResp && !sessionGone && !out.Load() && attempt < maxAttempts {
			slog.Info(fmt.Sprintf("[kiroacp] 第 %d 次嘗試失敗，重試: %v", attempt, err))
			continue
		}
		slog.Info(fmt.Sprintf("[kiroacp] 無法恢復（嘗試 %d 次、已有產出=%v、EmptyResponse=%v），交給上層換會話接手: %v", attempt, out.Load(), emptyResp, err))
		return fmt.Errorf("%w: %w", agent.ErrHandoff, err)
	}
}

// activity 追蹤一輪 session/prompt 的「有沒有動靜」，供 idle watchdog 判斷。
type activity struct {
	mu          sync.Mutex
	last        time.Time
	tools       map[string]struct{} // 進行中的 tool call
	waitingPerm int                 // 等使用者授權中：由人決定，不計時
	// 以下為實測統計，不影響判斷。
	maxGap       time.Duration
	maxGapInTool bool
}

func newActivity() *activity {
	return &activity{last: time.Now(), tools: map[string]struct{}{}}
}

func (a *activity) touch() {
	a.mu.Lock()
	now := time.Now()
	if gap := now.Sub(a.last); gap > a.maxGap {
		a.maxGap, a.maxGapInTool = gap, len(a.tools) > 0
	}
	a.last = now
	a.mu.Unlock()
}

// gapStats 回報本輪最長的無動靜間隔與當時是否有工具在跑；用來依實測調整 idleLimit。
func (a *activity) gapStats() (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.maxGap, a.maxGapInTool
}

func (a *activity) tool(id, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch strings.ToLower(status) {
	case "completed", "failed", "cancelled", "canceled":
		delete(a.tools, id)
	default:
		a.tools[id] = struct{}{}
	}
}

func (a *activity) perm(delta int) {
	a.mu.Lock()
	a.waitingPerm += delta
	a.last = time.Now()
	a.mu.Unlock()
}

func (a *activity) stalled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 等授權（由人決定）或工具執行中：不計時。工具可以跑多久由工具自己／使用者決定。
	if a.waitingPerm > 0 || len(a.tools) > 0 {
		return false
	}
	return time.Since(a.last) > idleLimit
}

// watchIdle 在 a 停滯時呼叫 trip 一次後結束；ctx 結束也會結束。
func watchIdle(ctx context.Context, a *activity, trip func()) {
	t := time.NewTicker(idleTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a.stalled() {
				trip()
				return
			}
		}
	}
}
