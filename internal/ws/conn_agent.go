// wsConn 啟動 agent 任務：runAgent、輸入處理與額度自動繼續。

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/antigravity"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/kiroacp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// runAgent：與 WS 解耦，任務在背景執行。
// allowedOnce：僅「允許此操作」該次 retry 帶入 --allowedTools，不可寫入 DB（否則變成永久 allowlist）。
// userMsgID：本次 prompt 對應的本機 user 訊息 ID（0 表示非使用者輸入，例如重試提示）；Telegram 發送據此綁定本機列。
func (h *wsConn) runAgent(prompt string, allowedOnce []string, userMsgID int64, tgFiles []string, interrupted ...bool) {
	// 閘門必須在第一個 cancel 與任何寫入之前：維護中直接拒絕，不取消、不寫 running、不取出佇列。
	release, err := admitWork()
	if err != nil {
		h.send(updateInProgressMsg(""))
		return
	}
	defer release()
	shellTaskCancel(h.sessionID)
	clearShellPending(h.database, h.sessionID)
	clearInMemoryShellApproval(h.sessionID)
	taskCancel(h.sessionID)
	if err := h.database.FinalizePendingMessagesForSession(h.sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] FinalizePendingMessagesForSession: %v", err))
	}

	s, err := h.database.GetSession(h.sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] GetSession: %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	runnerPrompt := prompt
	firstInstruction := false
	if g := s.GlobalInstruction; userMsgID > 0 && g.Text != "" {
		if g.Mode == db.GlobalInstructionFirst {
			sent, err := h.database.GlobalInstructionSent(h.sessionID)
			if err != nil {
				slog.Error("[ws] read global instruction delivery", "err", err)
				h.broadcast(serverMsg{Type: "error", Content: "無法確認首則全域指令是否已送出"})
				return
			}
			firstInstruction = !sent
		}
		if firstInstruction || g.Mode == db.GlobalInstructionEvery {
			runnerPrompt = "全域指令\n" + g.Text + "\n\n使用者訊息\n" + prompt
		}
	}
	runnerPrompt = withPMCore(h.database, h.sessionID, userMsgID, runnerPrompt)
	if len(interrupted) > 0 && interrupted[0] {
		runnerPrompt = runNowNotice + "\n" + runnerPrompt
	}
	h.mu.Lock()
	pm := s.PermissionMode
	agSid := s.AgentSessionID
	wdir := s.WorkDir
	var cliExtra []string
	if len(s.CliExtraArgs) > 0 {
		cliExtra = append([]string(nil), s.CliExtraArgs...)
	}
	h.mu.Unlock()

	msgID, err := h.database.CreatePendingMessage(h.sessionID)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] CreatePendingMessage: %v", err))
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		return
	}
	if err := h.database.BindRoomJob(h.sessionID, userMsgID, msgID); err != nil {
		slog.Error("[room] bind job", "err", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := taskStartWithDone(h.sessionID, cancel, msgID)

	if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning); err != nil {
		slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus running: %v", err))
	}
	h.broadcast(serverMsg{Type: "status", Value: StateThinking})

	extra := map[string]string{
		"miniapp_session_id": h.sessionID,
	}
	if pm != "" {
		extra[agent.ArgPermissionMode] = pm
	}
	if h.isClaude && len(allowedOnce) > 0 {
		extra[agent.ArgAllowedTools] = strings.Join(allowedOnce, ",")
	}
	if h.agentType == agent.TypeCursor && pm == "bypassPermissions" {
		extra[agent.ArgForce] = "true"
	}
	if m := model.ParseModelFromCliArgs(cliExtra); m != "" {
		extra[agent.ArgModel] = m
	}
	if s.Model != "" {
		extra[agent.ArgModel] = s.Model
	}
	if s.Effort != "" && h.agentType != agent.TypeCursor {
		extra[agent.ArgEffort] = s.Effort
	}
	if h.agentType == agent.TypeTelegram && userMsgID > 0 {
		extra["tg_local_message_id"] = fmt.Sprint(userMsgID)
	}
	if h.agentType == agent.TypeTelegram && len(tgFiles) > 0 {
		extra["tg_attachment_ids"] = strings.Join(tgFiles, ",")
	}

	opts := agent.RunOptions{
		Prompt:            runnerPrompt,
		SystemInstruction: nativeSystemInstruction(s),
		SessionID:         agSid,
		WorkDir:           wdir,
		ExtraArgs:         extra,
		CliExtraArgs:      cliExtra,
		OnStart:           func(pid int) { taskSetPid(h.sessionID, msgID, pid) },
	}

	// 等授權一定要有人處理：由 MCP（mcp_token）發起的 run 沒有 tg_id，退回白名單唯一使用者；訪客不退回。
	confirmNotifyID := func() int64 {
		if h.tgUserID == 0 && !h.isGuest {
			id, _ := h.database.DefaultNotifyTgIDIfSingle()
			return id
		}
		return h.tgUserID
	}

	// askUser：中途授權共用流程（kiroacp 的 RequestPermission 與 Claude 的 --permission-prompt-tool 都走這裡）。
	// 廣播 permission_request 給前端並阻塞，直到使用者 allow_once/deny_once 或 rctx 取消。
	askUser := func(rctx context.Context, tools any) bool {
		pe := &pendingPermEntry{ch: make(chan bool, 1), tools: tools}
		permSet(h.sessionID, pe)

		_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusAwaitingConfirm)
		h.broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
		h.broadcast(serverMsg{Type: "permission_request", Tools: tools})
		notifyTaskAsync(h.botToken, confirmNotifyID(), h.notifyCfg, tg.TaskAlert{
			SessionName: h.sess.Name,
			Outcome:     tg.OutcomeConfirm,
		})

		var allow bool
		select {
		case allow = <-pe.ch:
		case <-rctx.Done():
			allow = false
		}

		permClear(h.sessionID, pe)
		if rctx.Err() == nil {
			_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning)
			h.broadcast(serverMsg{Type: "status", Value: StateStreaming})
		}
		return allow
	}

	// ACP 互動式授權：有原生指令的 Antigravity 與 kiroacp 共用既有決策流程。
	if h.agentType == agent.TypeKiroACP || (h.agentType == agent.TypeAntigravity && opts.SystemInstruction != "") {
		opts.RequestPermission = func(rctx context.Context, req agent.PermissionRequest) string {
			title := strings.TrimSpace(req.Title)
			if title == "" {
				title = "工具授權請求"
			}
			allow := askUser(rctx, []map[string]string{{"tool_name": title}})

			// 依 kind 把粗粒度 allow/deny 對應成該請求的 optionId（robust，不寫死 id）。
			wantKinds := []string{"reject_once", "reject_always"}
			if allow {
				wantKinds = []string{"allow_once", "allow_always"}
			}
			for _, k := range wantKinds {
				for _, o := range req.Options {
					if o.Kind == k {
						return o.OptionID
					}
				}
			}
			if allow && h.agentType == agent.TypeKiroACP {
				for _, o := range req.Options {
					if !strings.HasPrefix(o.Kind, "reject") {
						return o.OptionID
					}
				}
			}
			return "" // deny / cancel
		}
	}

	owned, generation, observed := false, int64(0), int64(-1)
	if o, held, seen, snapErr := h.database.SnapshotQuotaResume(h.sessionID); snapErr != nil {
		slog.Info(fmt.Sprintf("[ws] quota resume snapshot: %v", snapErr))
	} else {
		owned, generation, observed = o, held, seen
	}
	runner, err := agent.NewRunner(h.agentType)
	if err != nil {
		slog.Info(fmt.Sprintf("[ws] NewRunner %s: %v", h.agentType, err))
		_ = h.database.FinalizeMessage(msgID)
		_ = h.database.FinishRoomJob(h.sessionID, msgID, "failed")
		_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle)
		taskEnd(h.sessionID, msgID)
		h.broadcast(serverMsg{Type: "error", Content: err.Error()})
		h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		recordSessionFailure(h.database, h.sessionID, err.Error())
		if relErr := releaseQuotaResumeRun(h.database, h.sessionID, owned, generation); relErr != nil {
			slog.Info(fmt.Sprintf("[ws] quota resume release: %v", relErr))
		}
		taskFinish(h.sessionID, msgID)
		close(done)
		h.pauseQueue()
		notifyTaskAsync(h.botToken, h.tgUserID, h.notifyCfg, tg.TaskAlert{
			SessionName: h.sess.Name,
			AgentType:   h.agentType,
			Outcome:     tg.OutcomeError,
			Prompt:      prompt,
			Error:       err.Error(),
		})
		return
	}
	slog.Info(fmt.Sprintf("[ws] start %s.Run agentSessionID=%q mode=%s msgID=%d", runner.Name(), opts.SessionID, pm, msgID))
	h.resumeLaunched = true

	// All Claude modes use the prompt bridge for both permissions and AskUserQuestion.
	var permAsker *claudeAsker
	var askAudit *claudeAskAudit
	permCleanup := func() {}
	if h.isClaude {
		if _, _, ok := claudePermMCP(); ok {
			if cfgPath, rmCfg, err := h.ho.writeClaudeMCPConfig(h.sessionID, extra); err != nil {
				slog.Info(fmt.Sprintf("[ws] perm mcp 設定檔建立失敗，略過權限詢問: %v", err))
			} else {
				askQuestion := func(rctx context.Context, input json.RawMessage) (json.RawMessage, error) {
					q, err := newQuestion(input)
					if err != nil {
						return nil, err
					}
					q.ctx = rctx
					pendingQuestions.Lock()
					pendingQuestions.m[h.sessionID] = q
					pendingQuestions.Unlock()
					defer func() {
						pendingQuestions.Lock()
						if pendingQuestions.m[h.sessionID] == q {
							delete(pendingQuestions.m, h.sessionID)
						}
						pendingQuestions.Unlock()
						h.broadcast(serverMsg{Type: "question_resolved", RequestID: q.RequestID})
						if rctx.Err() == nil {
							_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusRunning)
							h.broadcast(serverMsg{Type: "status", Value: StateStreaming})
						}
					}()
					_ = h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusAwaitingInput)
					h.broadcast(serverMsg{Type: "status", Value: StateAwaitingInput})
					h.broadcast(serverMsg{Type: "question_request", Question: q})
					notifyTaskAsync(h.botToken, confirmNotifyID(), h.notifyCfg, tg.TaskAlert{SessionName: h.sess.Name, Outcome: tg.OutcomeQuestion})
					select {
					case answer := <-q.ch:
						return answer, rctx.Err()
					case <-rctx.Done():
						return nil, rctx.Err()
					}
				}
				askAudit = newClaudeAskAudit(h.sessionID)
				autoAllow := func() bool {
					// 權限模式用本輪的 pm，不中途重讀；開關每次詢問才讀。
					return claudeAutoAllow(pm, func() (bool, error) {
						gs, err := h.database.GetGeneralSettings()
						if err != nil {
							return false, err
						}
						return gs.ClaudeAutoAllowAsk, nil
					})
				}
				a, unreg := registerClaudeAsker(h.sessionID, ctx, askUser, askQuestion, autoAllow, askAudit.record)
				permAsker = a
				extra[agent.ArgPermPromptConfig] = cfgPath
				permCleanup = func() { unreg(); rmCfg() }
			}
		}
	}

	// goroutine 接手自己的 token；它必須是第一個 defer，確保 taskEnd／drain 之後才放行。
	goRelease := holdWork()
	go func(opts agent.RunOptions, msgID int64, userPrompt string) {
		defer goRelease()
		defer permCleanup()
		// run 結束一定要取消 ctx：卡在授權／提問的請求靠它解除，否則 claude 子進程先結束時項目會永遠殘留。
		defer cancel()

		content := &messageBuffer{write: func(text string) error { return h.database.AppendMessageContent(msgID, text) }, send: h.broadcast}
		defer content.Flush()
		permDenied := false
		runFailed := false
		succeeded := false
		errText := ""
		var cancelled bool
		var interjected bool

		// result 後程序可能仍活著（例如等 ScheduleWakeup 喚醒），此時後端仍視為忙碌、新訊息會排隊，
		// 但 status 已是 IDLE（MCP 的 ask_session 靠它判斷回合結束，不能改）。另發 lingering 讓前端顯示中斷鈕。
		var lingerMu sync.Mutex
		runEnded := false

		// appendNotice：把說明寫進本則回覆（落 DB＋即時廣播）。尚未開始串流時先補 streaming 狀態，
		// 前端的 delta 只會接在最後一則訊息上。自動允許的稽核可能從 MCP goroutine 呼叫，所以用 atomic。
		var streamed atomic.Bool
		appendNotice := func(text string) {
			if text == "" {
				return
			}
			if streamed.CompareAndSwap(false, true) {
				h.broadcast(serverMsg{Type: "status", Value: StateStreaming})
			}
			if err := errors.Join(content.Append(text, serverMsg{Type: "delta", Content: text}), content.Flush()); err != nil {
				slog.Info(fmt.Sprintf("[ws] AppendMessageContent (notice): %v", err))
			}
		}
		// 晚於授權通道註冊才綁定；尚未綁定時稽核只寫 log，允許行為不丟。
		if askAudit != nil {
			askAudit.bindNotice(appendNotice)
		}

		// 收尾決定佇列走向。等授權時不動佇列；成功才續跑；失敗／中斷暫停，避免錯誤連鎖。
		// 最後登記的收尾 defer 先解除舊任務登記，drainIfIdle 才會看到閒置。
		defer func() {
			switch {
			case interjected:
				if err := releaseQuotaResumeRun(h.database, h.sessionID, owned, generation); err != nil {
					slog.Info(fmt.Sprintf("[ws] quota resume release: %v", err))
				}
			case permDenied:
			case succeeded && !runFailed && !cancelled:
				clearSessionFailure(h.database, h.sessionID)
				if err := releaseQuotaResumeRun(h.database, h.sessionID, owned, generation); err != nil {
					slog.Info(fmt.Sprintf("[ws] quota resume release: %v", err))
				}
				h.drainIfIdle()
			default:
				refreshQuota := h.ho.quotaResumeRefresh
				if refreshQuota == nil && h.quotaSvc != nil {
					refreshQuota = h.quotaSvc.RefreshAfterRun
				}
				if err := finishHeldQuotaResume(h.database, h.sessionID, h.agentType, userMsgID, runFailed || !succeeded, cancelled, permDenied, refreshQuota, time.Now(), generation, observed); err != nil {
					slog.Info(fmt.Sprintf("[ws] quota resume arm: %v", err))
				}
				if err := releaseQuotaResumeRun(h.database, h.sessionID, owned, generation); err != nil {
					slog.Info(fmt.Sprintf("[ws] quota resume release: %v", err))
				}
				h.pauseQueue()
			}
		}()
		// 本體已定稿並寫回 idle；必須先通知，再進入可能等待 dispatchLock 的佇列決策。
		defer func() {
			interjected = taskFinish(h.sessionID, msgID)
			if cancelled && !permDenied && !interjected {
				notifyTaskAsync(h.botToken, h.tgUserID, h.notifyCfg, tg.TaskAlert{
					SessionName: h.sess.Name,
					AgentType:   h.agentType,
					Outcome:     tg.OutcomeCancelled,
					Prompt:      userPrompt,
				})
			}
			close(done)
		}()

		// stream 不含 model，run 前解析並推送。
		if isStreamless(h.agentType) || h.agentType == agent.TypeKiroACP {
			info := resolveStreamless(s, h.agentType)
			if p := persistInfoUpdate(h.database, h.sessionID, info); p != nil {
				h.broadcast(serverMsg{Type: "model_update", Model: p})
			}
		}

		err := runner.Run(ctx, opts, func(e agent.Event) {
			if firstInstruction && !runFailed {
				// A model response confirms receipt. Starting the CLI alone does not.
				switch e.Type {
				case agent.EventDelta, agent.EventThinking, agent.EventToolStarted, agent.EventPermDenied, agent.EventDone:
					if err := h.database.MarkGlobalInstructionSent(h.sessionID, userMsgID); err != nil {
						slog.Error("[ws] save global instruction delivery", "err", err)
					} else {
						firstInstruction = false
					}
				}
			}
			if e.Type != agent.EventDelta {
				if flushErr := content.Flush(); flushErr != nil {
					runFailed = true
					errText = flushErr.Error()
					cancel()
					h.broadcast(serverMsg{Type: "error", Content: errText})
					return
				}
			}

			// 中止後仍處理 delta/done/error，避免進程收尾時文字被丟棄成空白訊息。
			if ctx.Err() != nil {
				switch e.Type {
				case agent.EventDelta, agent.EventDone, agent.EventError, agent.EventPermDenied, agent.EventSessionInit:
				default:
					return
				}
			}
			switch e.Type {
			case agent.EventStreamStart:
				streamed.Store(true)
				h.broadcast(serverMsg{Type: "status", Value: StateStreaming})

			case agent.EventThinking:
				// 思考鏈：不寫 DB、不改狀態，直接廣播給前端覆寫顯示。
				if e.Text != "" {
					h.broadcast(serverMsg{Type: "thinking", Content: e.Text})
				}

			case agent.EventActivity:
				if e.Text != "" {
					h.broadcast(serverMsg{Type: "activity", Content: e.Text})
				}

			case agent.EventDelta:
				if e.Text != "" {
					if err := content.Append(e.Text, serverMsg{Type: "delta", Content: e.Text}); err != nil {
						slog.Info(fmt.Sprintf("[ws] AppendMessageContent: %v", err))
						cancel()
					}
				}

			case agent.EventSessionInit:
				if e.SessionID == "" && e.Model == nil {
					return
				}
				h.mu.Lock()
				if e.SessionID != "" && e.SessionID != h.agentSessionID {
					h.agentSessionID = e.SessionID
					if err := h.database.UpdateAgentSessionID(h.sessionID, h.agentSessionID); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateAgentSessionID: %v", err))
					}
				}
				h.mu.Unlock()
				if e.Model != nil {
					p := persistModelUpdate(h.database, h.sessionID, e.Model)
					if p.DisplayText != "" && p.DisplayText != "—" {
						h.broadcast(serverMsg{Type: "model_update", Model: &p})
					}
				}

			case agent.EventPermDenied:
				if !h.isClaude {
					return
				}
				// 已由使用者在授權面板回答過的（prompt-tool 回 deny 後 result 仍會列出），不再重複詢問。
				denials := permAsker.dropAnswered(e.Denials)
				filtered := denials[:0:0]
				for _, d := range denials {
					if d.ToolName == "AskUserQuestion" {
						appendNotice("\n\nClaude 的問題未能透過互動通道送達，請在聊天中補充答案或請它重新提問。\n" + string(d.ToolInput))
					} else {
						filtered = append(filtered, d)
					}
				}
				denials = filtered
				// 被 deny 規則擋下、或「允許」後重跑仍被拒的，按允許也不會過：改成說明原因，不進等授權。
				pending, blocked := splitDenials(denials)
				pending, still := splitStillDenied(allowedOnce, pending)
				appendNotice(blockedNotice(blocked))
				appendNotice(stillDeniedNotice(still))
				if len(pending) == 0 {
					return
				}
				permDenied = true
				if raw, err := json.Marshal(pending); err == nil {
					if err := h.database.UpdatePendingDenials(h.sessionID, string(raw)); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdatePendingDenials: %v", err))
					}
				}
				if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusAwaitingConfirm); err != nil {
					slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus awaiting_confirm: %v", err))
				}
				h.broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
				h.broadcast(serverMsg{Type: "permission_request", Tools: pending, RetryTools: true})
				notifyTaskAsync(h.botToken, confirmNotifyID(), h.notifyCfg, tg.TaskAlert{
					SessionName: h.sess.Name,
					Outcome:     tg.OutcomeConfirm,
				})

			case agent.EventDone:
				h.mu.Lock()
				if e.SessionID != "" && e.SessionID != h.agentSessionID {
					h.agentSessionID = e.SessionID
					if err := h.database.UpdateAgentSessionID(h.sessionID, h.agentSessionID); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateAgentSessionID: %v", err))
					}
				}
				h.mu.Unlock()

				rt := strings.TrimSpace(e.ResultText)
				if rt != "" {
					if err := h.database.UpdateMessageResultText(msgID, rt); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateMessageResultText: %v", err))
					}
					if err := h.database.FillMessageContentIfEmpty(msgID, rt); err != nil {
						slog.Info(fmt.Sprintf("[ws] FillMessageContentIfEmpty: %v", err))
					}
					h.broadcast(serverMsg{Type: "message_result_text", ID: msgID, Content: rt})
				}

				// 中止中收到 result：只救內容，不當成功完成（避免提早「任務完成」）。
				if ctx.Err() != nil {
					return
				}

				if !permDenied && !runFailed {
					succeeded = true
					if h.agentType == agent.TypeTelegram {
						_ = h.database.DeleteEmptyMessage(msgID)
					}
					if err := h.database.FinalizeMessage(msgID); err != nil {
						slog.Info(fmt.Sprintf("[ws] FinalizeMessage: %v", err))
					}
					clearPendingDenials(h.database, h.sessionID)
					if err := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); err != nil {
						slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle: %v", err))
					}
					h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
					// 延遲一下再通知：一般情況程序毫秒內就退出，不需閃一下中斷鈕。
					time.AfterFunc(lingerNoticeDelay, func() {
						lingerMu.Lock()
						defer lingerMu.Unlock()
						if !runEnded {
							h.broadcast(serverMsg{Type: "lingering", Value: "on"})
						}
					})
					notifyTaskAsync(h.botToken, h.tgUserID, h.notifyCfg, tg.TaskAlert{
						SessionName: h.sess.Name,
						Outcome:     tg.OutcomeSuccess,
					})
					if h.quotaSvc != nil {
						go func(at string) {
							snap, err := h.quotaSvc.RefreshAfterRun(context.Background(), at)
							if err != nil {
								slog.Info(fmt.Sprintf("[quota] refresh after run %s: %v", at, err))
							}
							p := snap.ToPayload()
							h.broadcast(serverMsg{Type: "quota_update", Quota: &p})
						}(h.agentType)
					}
				}

			case agent.EventError:
				if e.Err != nil {
					runFailed = true
					errText = agent.PreferErrorText(errText, e.Err.Error())
					h.broadcast(serverMsg{Type: "error", Content: errText})
				}
			}
		})

		if flushErr := content.Flush(); flushErr != nil {
			err = errors.Join(err, flushErr)
		}
		lingerMu.Lock()
		runEnded = true
		h.broadcast(serverMsg{Type: "lingering", Value: "off"})
		lingerMu.Unlock()

		if errors.Is(ctx.Err(), context.Canceled) {
			cancelled = true
		}

		// agent session 救不回來：保留本會話，另開新會話讓它透過 MCP 讀本會話後接手。
		// 自動交接沒有成功建立新會話時，廣播結構化資訊讓前端提供「建立接手會話」按鈕（不解析錯誤顯示字串）。
		handedOff := false
		if err != nil && !cancelled && errors.Is(err, agent.ErrHandoff) {
			ns, herr := startHandoff(h.database, s, userPrompt, err)
			if herr != nil {
				slog.Info(fmt.Sprintf("[ws] %s 自動交接失敗，改回報原錯誤: %v", h.agentType, herr))
				info := newManualHandoffInfo(s, userPrompt, err)
				h.broadcast(serverMsg{Type: "handoff_available", Handoff: &info})
			} else {
				handedOff = true
				slog.Info(fmt.Sprintf("[ws] %s 已自動交接 %s → %s", h.agentType, h.sessionID, ns.ID))
				appendNotice(fmt.Sprintf("⚠️ %s 這個會話執行失敗（%v）。已自動建立新會話「%s」接手，它會讀取本會話的對話後繼續處理，請到側欄查看。", h.agentType, strings.TrimPrefix(err.Error(), agent.ErrHandoff.Error()+": "), ns.Name))
			}
		}

		if err != nil && !handedOff {
			if cancelled {
				slog.Info(fmt.Sprintf("[ws] %s.Run cancelled", h.agentType))
			} else {
				slog.Info(fmt.Sprintf("[ws] %s.Run error: %v", h.agentType, err))
				if !runFailed {
					runFailed = true
					errText = err.Error()
				} else {
					errText = agent.PreferErrorText(errText, err.Error())
				}
				h.broadcast(serverMsg{Type: "error", Content: errText})
			}
		} else if cancelled {
			slog.Info(fmt.Sprintf("[ws] %s.Run cancelled (exit 0)", h.agentType))
		} else {
			slog.Info(fmt.Sprintf("[ws] %s.Run finished OK", h.agentType))
		}

		if (err != nil || cancelled || (runFailed && err == nil)) && !permDenied {
			if h.agentType == agent.TypeTelegram && !runFailed {
				_ = h.database.DeleteEmptyMessage(msgID)
			}
			if finErr := h.database.FinalizeMessage(msgID); finErr != nil {
				slog.Info(fmt.Sprintf("[ws] FinalizeMessage (err/cancel path): %v", finErr))
			}
			if stErr := h.database.UpdateSessionStatus(h.sessionID, db.SessionStatusIdle); stErr != nil {
				slog.Info(fmt.Sprintf("[ws] UpdateSessionStatus idle (err/cancel path): %v", stErr))
			}
			h.broadcast(serverMsg{Type: "status", Value: idleUIStatus(h.database, h.sessionID)})
		}

		if permDenied {
			return
		}
		outcome := "done"
		if cancelled {
			outcome = "interrupted"
		} else if runFailed || !succeeded {
			outcome = "failed"
		}
		if jobErr := h.database.FinishRoomJob(h.sessionID, msgID, outcome); jobErr != nil {
			slog.Error("[room] finish job", "err", jobErr)
		}
		if runFailed && !cancelled {
			recordSessionFailure(h.database, h.sessionID, errText)
			notifyTaskAsync(h.botToken, h.tgUserID, h.notifyCfg, tg.TaskAlert{
				SessionName: h.sess.Name,
				AgentType:   h.agentType,
				Outcome:     tg.OutcomeError,
				Prompt:      userPrompt,
				Error:       errText,
			})
		}
	}(opts, msgID, prompt)
}

func (h *wsConn) startInput(input clientMsg, queued *db.QueuedMessage) error {
	if queued != nil && queued.BatchID != "" {
		return h.startQueuedBatch(queued)
	}
	prompt, err := attachmentPrompt(h.database, h.sessionID, input.Data, input.AttachmentIDs)
	if err != nil {
		return err
	}
	// 排隊訊息保留原送出者（可能是別條連線、別人排的），其餘用本連線身分。
	who := h.author
	if queued != nil {
		who = queued.Author
	}
	// 署名帶進 prompt，工作室的內部識別轉成對應的通訊角色。
	if who != "" {
		prompt = "[" + db.RoomMessageAuthor(who) + "] " + prompt
	}
	if strings.HasPrefix(who, "room-job:") {
		prompt += "\n[工作室完工入口] 本輪最後答覆會依 job_id=" + strings.TrimPrefix(who, "room-job:") + " 自動回報目前 PM。不要另送未標識的完工通知；若仍需 send_message 完工通知，必帶 completion_job_id=" + strings.TrimPrefix(who, "room-job:") + "，由 server 合併到同一份自動回報。一般詢問不帶此欄位，新版交付另派新 job。"
	}
	// Telegram 的對話內容不進 messages 表：待傳氣泡只存在這次連線，重整後改向 Telegram 要最新頁。
	// 佇列裡的項目也只出列，不晉升成歷史列。
	if h.agentType == agent.TypeTelegram {
		content := input.Data
		files := input.AttachmentIDs
		if queued != nil {
			if err := h.database.DeleteQueuedMessage(h.sessionID, queued.ID); err != nil {
				return err
			}
			content = queued.Content
			files = queued.AttachmentIDs
		}
		id := time.Now().UnixNano()
		h.broadcast(serverMsg{
			Type: "user_message", ID: id, Content: content, Author: who,
			CreatedAt:       time.Now().UTC().Format("2006-01-02 15:04:05"),
			ClientMessageID: fmt.Sprint(id), Delivery: "pending",
		})
		if input.RequestID != "" {
			h.send(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
		}
		// 附件走 sendMedia，不把伺服器路徑當成聊天文字。
		caption := content
		if who != "" {
			caption = "[" + who + "] " + caption
		}
		h.runAgent(caption, nil, id, files)
		return nil
	}
	var m *db.Message
	if queued != nil {
		m, err = h.database.PromoteQueuedMessage(h.sessionID, *queued)
	} else if input.SourceSessionID != "" {
		m, err = h.database.AddUserMessageFromSession(h.sessionID, input.Data, who, input.SourceSessionID, input.AttachmentIDs)
	} else {
		m, err = h.database.AddUserMessageWithAttachments(h.sessionID, input.Data, who, input.AttachmentIDs)
	}
	if err != nil {
		return err
	}
	h.broadcast(serverMsg{Type: "user_message", ID: m.ID, Content: m.Content, CreatedAt: m.CreatedAt, Attachments: m.Attachments, Author: m.Author})
	if input.RequestID != "" {
		h.send(serverMsg{Type: "input_accepted", RequestID: input.RequestID})
	}
	h.runAgent(prompt, nil, m.ID, nil, input.interrupted)
	return nil
}

func (h *wsConn) resumeForQuota() bool {
	h.resumeLaunched = false
	h.dispatcher.ResumeAutomatic()
	return h.resumeLaunched
}
