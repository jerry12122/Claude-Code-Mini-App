package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	fiberws "github.com/gofiber/contrib/websocket"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude" // 註冊 claude runner
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor" // 註冊 cursor runner
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/gemini" // 註冊 gemini runner
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// clearPendingDenials 清除 DB 中的待授權紀錄
func clearPendingDenials(database *db.DB, sessionID string) {
	if err := database.UpdatePendingDenials(sessionID, ""); err != nil {
		log.Printf("[ws] 清除 pending_denials 失敗: %v", err)
	}
}

const (
	StateIdle            = "IDLE"
	StateThinking        = "THINKING"
	StateStreaming       = "STREAMING"
	StateAwaitingConfirm = "AWAITING_CONFIRM"
)

type clientMsg struct {
	Type  string   `json:"type"`
	Data  string   `json:"data,omitempty"`
	Tools []string `json:"tools,omitempty"`
	Mode  string   `json:"mode,omitempty"`
}

type serverMsg struct {
	Type     string          `json:"type"`
	Value    string          `json:"value,omitempty"`
	Content  string          `json:"content,omitempty"`
	Tools    interface{}     `json:"tools,omitempty"`
	Messages json.RawMessage `json:"messages,omitempty"`
}

func NewHandler(database *db.DB, botToken string) func(*fiberws.Conn) {
	return func(c *fiberws.Conn) {
		sessionID := c.Params("id")
		tgUserID, _ := c.Locals("tg_id").(int64)

		sess, err := database.GetSession(sessionID)
		if err != nil {
			log.Printf("[ws] session %s 不存在: %v", sessionID, err)
			c.Close()
			return
		}

		log.Printf("[ws] session %s 已連線 (agent=%s agentSessionID=%q mode=%s)", sessionID, sess.AgentType, sess.AgentSessionID, sess.PermissionMode)
		defer log.Printf("[ws] session %s 已斷線", sessionID)

		var mu sync.Mutex
		agentType := sess.AgentType
		if agentType == "" {
			agentType = agent.TypeClaude
		}
		agentSessionID := sess.AgentSessionID

		isClaude := agentType == agent.TypeClaude

		send := func(msg serverMsg) bool {
			b, _ := json.Marshal(msg)
			return c.WriteMessage(1, b) == nil
		}

		unsub := hub.Subscribe(sessionID, send)
		defer unsub()

		broadcast := func(msg serverMsg) {
			hub.Broadcast(sessionID, msg)
		}

		// 連線建立：sync（歷史 + 狀態）
		uiState, msgsJSON, err := buildSyncPayload(database, sessionID)
		if err != nil {
			log.Printf("[ws] buildSyncPayload: %v", err)
			uiState = StateIdle
		}
		syncMsg := serverMsg{Type: "sync", Value: uiState, Messages: msgsJSON}
		send(syncMsg)

		if isClaude && sess.PendingDenials != "" {
			send(serverMsg{Type: "permission_request", Tools: json.RawMessage(sess.PendingDenials)})
			log.Printf("[ws] 還原 pending_denials for session %s", sessionID)
		}

		// runAgent：與 WS 解耦，任務在背景執行。
		// allowedOnce：僅「允許此操作」該次 retry 帶入 --allowedTools，不可寫入 DB（否則變成永久 allowlist）。
		runAgent := func(prompt string, allowedOnce []string) {
			taskCancel(sessionID)
			if err := database.FinalizePendingMessagesForSession(sessionID); err != nil {
				log.Printf("[ws] FinalizePendingMessagesForSession: %v", err)
			}

			s, err := database.GetSession(sessionID)
			if err != nil {
				log.Printf("[ws] GetSession: %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}
			mu.Lock()
			pm := s.PermissionMode
			agSid := s.AgentSessionID
			wdir := s.WorkDir
			var cliExtra []string
			if len(s.CliExtraArgs) > 0 {
				cliExtra = append([]string(nil), s.CliExtraArgs...)
			}
			mu.Unlock()

			msgID, err := database.CreatePendingMessage(sessionID)
			if err != nil {
				log.Printf("[ws] CreatePendingMessage: %v", err)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			taskStart(sessionID, cancel, msgID)

			if err := database.UpdateSessionStatus(sessionID, db.SessionStatusRunning); err != nil {
				log.Printf("[ws] UpdateSessionStatus running: %v", err)
			}
			broadcast(serverMsg{Type: "status", Value: StateThinking})

			extra := map[string]string{}
			if pm != "" {
				extra[agent.ArgPermissionMode] = pm
			}
			if isClaude && len(allowedOnce) > 0 {
				extra[agent.ArgAllowedTools] = strings.Join(allowedOnce, ",")
			}
			if agentType == agent.TypeCursor && pm == "bypassPermissions" {
				extra[agent.ArgForce] = "true"
			}

			opts := agent.RunOptions{
				Prompt:       prompt,
				SessionID:    agSid,
				WorkDir:      wdir,
				ExtraArgs:    extra,
				CliExtraArgs: cliExtra,
			}

			runner, err := agent.NewRunner(agentType)
			if err != nil {
				log.Printf("[ws] 無法建立 %s runner: %v", agentType, err)
				_ = database.FinalizeMessage(msgID)
				_ = database.UpdateSessionStatus(sessionID, db.SessionStatusIdle)
				taskEnd(sessionID)
				broadcast(serverMsg{Type: "error", Content: err.Error()})
				broadcast(serverMsg{Type: "status", Value: StateIdle})
				return
			}

			log.Printf("[ws] 啟動 %s.Run agentSessionID=%q mode=%s msgID=%d", runner.Name(), opts.SessionID, pm, msgID)

			go func(opts agent.RunOptions, msgID int64) {
				defer taskEnd(sessionID)

				permDenied := false

				err := runner.Run(ctx, opts, func(e agent.Event) {
					if ctx.Err() != nil {
						return
					}
					switch e.Type {
					case agent.EventStreamStart:
						broadcast(serverMsg{Type: "status", Value: StateStreaming})

					case agent.EventDelta:
						if e.Text != "" {
							if err := database.AppendMessageContent(msgID, e.Text); err != nil {
								log.Printf("[ws] AppendMessageContent: %v", err)
							}
							broadcast(serverMsg{Type: "delta", Content: e.Text})
						}

					case agent.EventSessionInit:
						if e.SessionID == "" {
							return
						}
						mu.Lock()
						if e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								log.Printf("[ws] 更新 agent_session_id 失敗: %v", err)
							}
						}
						mu.Unlock()

					case agent.EventPermDenied:
						if !isClaude {
							return
						}
						permDenied = true
						if raw, err := json.Marshal(e.Denials); err == nil {
							if err := database.UpdatePendingDenials(sessionID, string(raw)); err != nil {
								log.Printf("[ws] 儲存 pending_denials 失敗: %v", err)
							}
						}
						if err := database.UpdateSessionStatus(sessionID, db.SessionStatusAwaitingConfirm); err != nil {
							log.Printf("[ws] UpdateSessionStatus awaiting_confirm: %v", err)
						}
						broadcast(serverMsg{Type: "status", Value: StateAwaitingConfirm})
						broadcast(serverMsg{Type: "permission_request", Tools: e.Denials})
						go tg.Notify(botToken, tgUserID, fmt.Sprintf("⚠️ *%s* 需要授權確認，請開啟 App", sess.Name))

					case agent.EventDone:
						mu.Lock()
						if e.SessionID != "" && e.SessionID != agentSessionID {
							agentSessionID = e.SessionID
							if err := database.UpdateAgentSessionID(sessionID, agentSessionID); err != nil {
								log.Printf("[ws] 更新 agent_session_id 失敗: %v", err)
							}
						}
						mu.Unlock()

						if !permDenied {
							if err := database.FinalizeMessage(msgID); err != nil {
								log.Printf("[ws] FinalizeMessage: %v", err)
							}
							clearPendingDenials(database, sessionID)
							if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
								log.Printf("[ws] UpdateSessionStatus idle: %v", err)
							}
							broadcast(serverMsg{Type: "status", Value: StateIdle})
							go tg.Notify(botToken, tgUserID, fmt.Sprintf("✅ *%s* 任務完成", sess.Name))
						}

					case agent.EventError:
						if e.Err != nil {
							broadcast(serverMsg{Type: "error", Content: e.Err.Error()})
						}
					}
				})

				if err != nil {
					if ctx.Err() != nil {
						log.Printf("[ws] %s.Run 被 context 取消", agentType)
					} else {
						log.Printf("[ws] %s.Run 執行錯誤: %v", agentType, err)
						broadcast(serverMsg{Type: "error", Content: err.Error()})
					}
					if err := database.FinalizeMessage(msgID); err != nil {
						log.Printf("[ws] FinalizeMessage (err path): %v", err)
					}
					if !permDenied {
						if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
							log.Printf("[ws] UpdateSessionStatus idle: %v", err)
						}
						broadcast(serverMsg{Type: "status", Value: StateIdle})
					}
				} else {
					log.Printf("[ws] %s.Run 正常結束", agentType)
				}
			}(opts, msgID)
		}

		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				break
			}

			var msg clientMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "input":
				log.Printf("[ws] 收到 input，prompt 長度=%d", len(msg.Data))
				if err := database.AddMessage(sessionID, "user", msg.Data); err != nil {
					log.Printf("[ws] 儲存 user 訊息失敗: %v", err)
				}
				broadcast(serverMsg{Type: "user_message", Content: msg.Data})
				runAgent(msg.Data, nil)

			case "allow_once":
				if !isClaude {
					log.Printf("[ws] agent=%s 不支援 allow_once，忽略", agentType)
					continue
				}
				clearPendingDenials(database, sessionID)
				// 清除舊版誤寫入的永久 allowlist（「允許此操作」不應持久化）
				if err := database.UpdateAllowedTools(sessionID, nil); err != nil {
					log.Printf("[ws] 清除 allowed_tools 失敗: %v", err)
				}
				once := make([]string, 0, len(msg.Tools))
				for _, t := range msg.Tools {
					t = strings.TrimSpace(t)
					if t != "" {
						once = append(once, t)
					}
				}
				if len(once) == 0 {
					log.Printf("[ws] allow_once 無工具名稱，略過")
					continue
				}
				runAgent("please retry the previous operation", once)

			case "set_mode":
				if agentType != agent.TypeClaude && agentType != agent.TypeCursor && agentType != agent.TypeGemini {
					log.Printf("[ws] agent=%s 不支援 set_mode，忽略", agentType)
					continue
				}
				clearPendingDenials(database, sessionID)
				if err := database.UpdatePermissionMode(sessionID, msg.Mode); err != nil {
					log.Printf("[ws] 更新 permission_mode 失敗: %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: StateIdle})
				log.Println("[ws] permission mode 切換為:", msg.Mode)

			case "reset_context":
				taskCancel(sessionID)
				_ = database.FinalizePendingMessagesForSession(sessionID)
				mu.Lock()
				agentSessionID = ""
				mu.Unlock()
				if err := database.UpdateAgentSessionID(sessionID, ""); err != nil {
					log.Printf("[ws] 清除 agent_session_id 失敗: %v", err)
				}
				if err := database.ClearMessages(sessionID); err != nil {
					log.Printf("[ws] 清除訊息失敗: %v", err)
				}
				clearPendingDenials(database, sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle: %v", err)
				}
				broadcast(serverMsg{Type: "reset"})
				broadcast(serverMsg{Type: "status", Value: StateIdle})
				log.Printf("[ws] session %s context 已重置", sessionID)

			case "interrupt":
				taskCancel(sessionID)
				_ = database.FinalizePendingMessagesForSession(sessionID)
				if err := database.UpdateSessionStatus(sessionID, db.SessionStatusIdle); err != nil {
					log.Printf("[ws] UpdateSessionStatus idle: %v", err)
				}
				broadcast(serverMsg{Type: "status", Value: StateIdle})
			}
		}
	}
}
