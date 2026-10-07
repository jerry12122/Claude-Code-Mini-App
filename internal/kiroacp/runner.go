package kiroacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
	"log/slog"
)

func init() {
	agent.Register(agent.TypeKiroACP, func() agent.Runner {
		return &Runner{}
	})
}

// Runner 以 kiro-cli acp（JSON-RPC over stdio）實作 agent.Runner。
//
// 每則訊息 spawn 一次子進程：initialize → session/new|load → session/prompt → kill。
// 不重用 internal/kiro 的 --list-sessions / TTY 行解析。
//
// session/load：kiro-cli 2.16.0+ 跨進程 resume 已通過 PoC（同／異 cwd 皆可）；
// 需 kiro-cli >= 2.16.0（2.12.1 會 timeout，見 GitHub kirodotdev/Kiro#6753）。
type Runner struct{}

func (r *Runner) Name() string { return agent.TypeKiroACP }

// runOnce 跑一次完整的「spawn → initialize → session → prompt → 收尾」。out 由上層 Run 的 cb 包裝維護；
// logPath 是這個 kiro-cli 子進程專屬的 log 檔（空字串＝不隔離）；成功時完成事件放進 done，由 Run 在收尾成功後才送出。
func (r *Runner) runOnce(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback, out *atomic.Bool, logPath string, done *agent.Event) (retErr error) {
	cwdRes, err := resolveWorkDir(opts.WorkDir)
	if err != nil {
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	cwd := cwdRes.Path
	logWorkDirWarning(cwdRes)

	agentName := ""
	if opts.ExtraArgs != nil {
		agentName = strings.TrimSpace(opts.ExtraArgs["agent"])
	}
	mcpServers, mcpMeta, err := buildACPMcpServers(cwd, agentName)
	if err != nil {
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	logMcpLoad(mcpMeta)

	args := buildArgs(opts)
	slog.Info(fmt.Sprintf("[kiroacp] 執行: kiro-cli %s (prompt len=%d)", strings.Join(args, " "), len(opts.Prompt)))
	slog.Info(fmt.Sprintf("[kiroacp] 工作目錄: %s", cwd))

	cmd := exec.CommandContext(ctx, kiroBin, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = proc.SysProcAttr()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return proc.KillTree(cmd.Process.Pid)
		}
		return nil
	}
	// 每個 kiro-cli 子進程寫自己專屬的 log，失敗時才能確定讀到的是「這一輪」的原因；
	// 只影響我們啟動的這個子進程的環境變數，不動使用者的全域設定與其他服務。
	if logPath != "" {
		cmd.Env = append(os.Environ(), "KIRO_CHAT_LOG_FILE="+logPath)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		slog.Info(fmt.Sprintf("[kiroacp] 啟動失敗: %v", err))
		return err
	}
	slog.Info(fmt.Sprintf("[kiroacp] 子進程 PID=%d", cmd.Process.Pid))
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}

	cl := newClient(cmd, stdout, stdin)
	// actx 是「這個子進程還能對外產出」的範圍：收尾一開始就取消，之後遲到的 update 丟棄、授權請求直接拒絕，
	// 避免已判定失敗（甚至來不及確認退出）的舊進程再產生 callback，或讓使用者授權到一個已被放棄的工具呼叫。
	actx, acancel := context.WithCancel(ctx)
	finished := false // 正常跑完才給進程較長時間自己收尾；失敗時縮短，避免卡住的進程讓 cmd.Wait 等太久
	defer func() {
		acancel()
		cl.close()
		// 先關 stdin 給 kiro 一點時間把 log flush 完，逾時才砍（失敗分類要靠這份 log）。
		grace := exitGrace
		if !finished {
			grace = exitGraceFail
		}
		waited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(waited) }()
		exited := true
		select {
		case <-waited:
		case <-time.After(grace):
			_ = killTree(cmd.Process.Pid)
			select {
			case <-waited:
			case <-time.After(killWait):
				exited = false
			}
		}
		// 授權 worker 也要確認結束：acancel()+close() 之後它的 callback 應立刻返回並退出；
		// 否則舊 attempt 的授權流程可能還在操作 UI，不能再開下一輪。
		workerDone := true
		select {
		case <-cl.permDone:
		case <-time.After(permDrainWait):
			workerDone = false
		}
		if !exited || !workerDone {
			sentinel, what := errProcAlive, fmt.Sprintf("子進程 PID=%d 無法確認已退出", cmd.Process.Pid)
			if exited {
				sentinel, what = errWorkerStuck, "授權 worker 未能結束"
			}
			slog.Info(fmt.Sprintf("[kiroacp] %s，放棄等待", what))
			// 不論本輪成功或失敗都算失敗：舊進程／舊授權流程可能還在，呼叫端不可再開下一輪或宣告完成。
			result := "本輪已完成，但收尾失敗"
			if retErr != nil {
				result = retErr.Error()
			}
			retErr = fmt.Errorf("%w（%s；本輪原本的結果: %s）", sentinel, what, result)
		}
	}()

	act := newActivity()
	defer func() {
		act.touch() // 把最後一段（靜默到判定逾時／結束）也算進去，否則卡到逾時的那段不會出現在統計
		gap, inTool := act.gapStats()
		slog.Info(fmt.Sprintf("[kiroacp] idle 統計: 最長無動靜 %.1fs（當時工具執行中=%v）", gap.Seconds(), inTool))
	}()
	// 互動式授權：把 ACP session/request_permission 轉給呼叫端（WS）決定。
	if opts.RequestPermission != nil {
		cl.onPermission = func(p permissionParams) string {
			req := agent.PermissionRequest{
				ToolCallID: p.ToolCall.ToolCallID,
				Title:      p.ToolCall.Title,
			}
			for _, o := range p.Options {
				req.Options = append(req.Options, agent.PermissionOption{
					OptionID: o.OptionID, Name: o.Name, Kind: o.Kind,
				})
			}
			act.perm(1)
			defer act.perm(-1)
			if actx.Err() != nil { // 收尾中／已取消：拒絕，不打擾使用者
				return ""
			}
			return opts.RequestPermission(actx, req)
		}
	}

	streamStarted := false
	emitStreamStart := func() {
		if streamStarted {
			return
		}
		streamStarted = true
		cb(agent.Event{Type: agent.EventStreamStart})
	}

	// ACP v1：session/load 必須以 session/update 回放歷史。
	// 本專案 UI／DB 已有完整對話，若把回放當成 EventDelta 會寫進「本則新回覆」，
	// 造成複誦且隨回合雪球變大。load 期間關閉轉發；prompt 階段再開。
	var acceptUpdates atomic.Bool
	acceptUpdates.Store(true)

	cl.onUpdate = func(body sessionUpdateBody) {
		act.touch() // 任何 session/update（含思考、工具進度）都算有動靜，包含 load 回放
		if actx.Err() != nil || !acceptUpdates.Load() {
			return
		}
		// 只追蹤本輪 prompt 的工具：load 回放的歷史工具（可能永遠沒有 completed）不可算進來，否則 watchdog 永遠不觸發。
		if body.SessionUpdate == "tool_call" || body.SessionUpdate == "tool_call_update" {
			act.tool(body.ToolCallID, body.Status)
		}
		switch body.SessionUpdate {
		case "agent_message_chunk":
			text := extractAgentText(body)
			if text == "" {
				return
			}
			emitStreamStart()
			cb(agent.Event{Type: agent.EventDelta, Text: text})
		case "tool_call", "tool_call_update":
			label := strings.TrimSpace(body.Title)
			if label == "" {
				label = body.Kind
			}
			if body.Status != "" {
				label = label + " (" + body.Status + ")"
			}
			if label == "" {
				label = body.SessionUpdate
			}
			cb(agent.Event{Type: agent.EventActivity, Text: label})
			// 僅在 completed 時落地圖片，避免 started/in_progress 的空 rawOutput 或半成品重覆寫入。
			if body.SessionUpdate != "tool_call_update" || !strings.EqualFold(body.Status, "completed") {
				break
			}
			for _, img := range body.images() {
				url, err := media.SaveBase64Image(img.MediaType, img.Data)
				if err != nil {
					slog.Info(fmt.Sprintf("[kiroacp] 圖片存檔失敗: %v", err))
					continue
				}
				emitStreamStart()
				cb(agent.Event{Type: agent.EventDelta, Text: fmt.Sprintf("\n![screenshot](%s)\n", url)})
			}
		}
	}

	// 啟動階段（initialize、session/new|load）有限時：kiro 卡在這裡時 watchdog 還沒啟動，否則會永遠等。
	// 父 ctx（使用者中止）取消不算逾時，Run 看到 ctx.Err() 就不會重試。
	sctx, scancel := context.WithTimeout(actx, setupLimit)
	defer scancel()
	setupCall := func(method string, params any) (json.RawMessage, error) {
		raw, err := cl.call(sctx, method, params)
		if err != nil && ctx.Err() == nil && errors.Is(sctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("%w（%s 超過 %s）", errSetupTimeout, method, setupLimit)
		}
		return raw, err
	}

	if _, err := setupCall("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "claude-miniapp", "version": "0.1.0"},
	}); err != nil {
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	_ = cl.notify(sctx, "initialized", map[string]any{})

	sessionID := strings.TrimSpace(opts.SessionID)
	var sess sessionNewResult

	if sessionID == "" {
		raw, err := setupCall("session/new", map[string]any{
			"cwd":        cwd,
			"mcpServers": mcpServers,
		})
		if err != nil {
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sess, err = parseSessionResult(raw)
		if err != nil {
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sessionID = sess.SessionID
		if sessionID == "" {
			err := fmt.Errorf("kiroacp: session/new 未回傳 sessionId")
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		slog.Info(fmt.Sprintf("[kiroacp] session/new id=%s model=%s", sessionID, modelIDFrom(sess)))
		cb(agent.Event{
			Type:      agent.EventSessionInit,
			SessionID: sessionID,
			Model:     modelSnapshot(sess),
		})
	} else {
		// session/load：2.16.0 實測 ~0.5s；20s 足夠容錯慢磁碟／冷啟動。
		// 關閉 update 轉發，避免歷史回放寫入本則回覆。
		acceptUpdates.Store(false)
		loadCtx, cancel := context.WithTimeout(sctx, 20*time.Second)
		raw, err := cl.call(loadCtx, "session/load", map[string]any{
			"sessionId":  sessionID,
			"cwd":        cwd,
			"mcpServers": mcpServers,
		})
		cancel()
		acceptUpdates.Store(true)
		if err != nil {
			err = fmt.Errorf("kiroacp: session/load 失敗（需 kiro-cli >= 2.16.0）: %w: %w", errLoadFailed, err)
			slog.Info(fmt.Sprintf("[kiroacp] %v", err))
			cb(agent.Event{Type: agent.EventError, Err: err})
			return err
		}
		sess, _ = parseSessionResult(raw)
		slog.Info(fmt.Sprintf("[kiroacp] session/load id=%s model=%s", sessionID, modelIDFrom(sess)))
		if m := modelSnapshot(sess); m != nil {
			cb(agent.Event{Type: agent.EventSessionInit, SessionID: sessionID, Model: m})
		}
	}

	// prompt 階段：idle watchdog 判定卡住就取消 pctx，讓 call 返回。
	pctx, pcancel := context.WithCancel(actx)
	var stalled atomic.Bool
	act.touch()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		watchIdle(pctx, act, func() { stalled.Store(true); pcancel() })
	}()
	defer func() { pcancel(); <-watchDone }() // 離開前確保 watchdog goroutine 已結束

	raw, err := cl.call(pctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt": []map[string]string{
			{"type": "text", "text": opts.Prompt},
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if stalled.Load() {
			return fmt.Errorf("%w（%s 內沒有任何 session/update）", errIdleTimeout, idleLimit)
		}
		cb(agent.Event{Type: agent.EventError, Err: err})
		return err
	}
	pcancel()

	// prompt 成功卻一字未出（文字、工具活動皆無）：對使用者等於沒回應，當成可重試故障而非「完成」。
	// 例外：stopReason=refusal 是 kiro 內容過濾（官方文件），重試或換會話都一樣，直接告知。
	if !out.Load() {
		var pr struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(raw, &pr)
		if pr.StopReason == "refusal" {
			cb(agent.Event{Type: agent.EventError, Err: errRefusal})
			return errRefusal
		}
		return fmt.Errorf("%w（stopReason=%q）", errEmptyResponse, pr.StopReason)
	}

	finished = true
	// 不在這裡送 EventDone：要等收尾（確認子進程退出）成功後，由 Run 送出，避免先宣告完成、收尾卻失敗。
	*done = agent.Event{Type: agent.EventDone, SessionID: sessionID}
	return nil
}

func buildArgs(opts agent.RunOptions) []string {
	args := []string{"acp"}
	// 權限模式對應：
	//   bypassPermissions / yolo → --trust-all-tools（全自動放行）
	//   其他（default/plan/acceptEdits）且有互動授權回呼 → 不加旗標（走 session/request_permission）
	//   無回呼時退回 --trust-all-tools，避免無人回覆導致工具卡住。
	mode := ""
	if opts.ExtraArgs != nil {
		mode = strings.TrimSpace(opts.ExtraArgs[agent.ArgPermissionMode])
	}
	interactive := opts.RequestPermission != nil && mode != "bypassPermissions" && mode != "yolo"
	if !interactive {
		args = append(args, "--trust-all-tools")
	}
	if opts.ExtraArgs != nil {
		if effort := strings.TrimSpace(opts.ExtraArgs["effort"]); effort != "" {
			args = append(args, "--effort", effort)
		}
		if agentProfile := strings.TrimSpace(opts.ExtraArgs["agent"]); agentProfile != "" {
			args = append(args, "--agent", agentProfile)
		}
		if m := strings.TrimSpace(opts.ExtraArgs[agent.ArgModel]); m != "" {
			args = append(args, "--model", m)
		}
	}
	return args
}

func modelIDFrom(s sessionNewResult) string {
	if s.Models == nil {
		return ""
	}
	return strings.TrimSpace(s.Models.CurrentModelID)
}

func modelSnapshot(s sessionNewResult) *agent.ModelSnapshot {
	id := modelIDFrom(s)
	if id == "" {
		return nil
	}
	return model.AgentSnapshot(model.Info{
		Provider:    agent.TypeKiroACP,
		Model:       id,
		DisplayText: model.FormatDisplay(id),
		Source:      model.SourceInitEvent,
		Ok:          true,
	})
}
