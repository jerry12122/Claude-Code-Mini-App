package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/claude" // 註冊 claude runner
	_ "github.com/jerry12122/Claude-Code-Mini-App/internal/cursor" // 註冊 cursor runner
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

// connRegistry 記錄每個 session 當前活著的連線
// key: sessionID, value: activeConn
var connRegistry sync.Map
var statusRegistry sync.Map // key: sessionID, value: string

type activeConn struct {
	token int64
	send  func(serverMsg) bool
}

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
	Type    string      `json:"type"`
	Value   string      `json:"value,omitempty"`
	Content string      `json:"content,omitempty"`
	Tools   interface{} `json:"tools,omitempty"`
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
		var cancelFn context.CancelFunc
		agentType := sess.AgentType
		if agentType == "" {
			agentType = agent.TypeClaude
		}
		agentSessionID := sess.AgentSessionID
		permMode := sess.PermissionMode
		allowedTools := sess.AllowedTools

		isClaude := agentType == agent.TypeClaude

		// 此連線的 send（直接寫入當前 c）
		send := func(msg serverMsg) bool {
			b, _ := json.Marshal(msg)
			return c.WriteMessage(1, b) == nil
		}

		// 註冊到 registry，用 token 確保斷線時不誤刪後來的連線
		token := time.Now().UnixNano()
		connRegistry.Store(sessionID, activeConn{token: token, send: send})
		defer func() {
			if v, ok := connRegistry.Load(sessionID); ok && v.(activeConn).token == token {
				connRegistry.Delete(sessionID)
			}
		}()

		// relaySend：查 registry，送給當前活著的連線（不限於本連線）
		relaySend := func(msg serverMsg) bool {
			if v, ok := connRegistry.Load(sessionID); ok {
				return v.(activeConn).send(msg)
			}
			return false
		}

		setAndSendStatus := func(status string) {
			statusRegistry.Store(sessionID, status)
			relaySend(serverMsg{Type: "status", Value: status})
		}

		// 還原未處理的 pending_denials（僅 Claude 有此流程）
		if isClaude && sess.PendingDenials != "" {
			send(serverMsg{Type: "status", Value: StateAwaitingConfirm})
			send(serverMsg{Type: "permission_request", Tools: json.RawMessage(sess.PendingDenials)})
			statusRegistry.Store(sessionID, StateAwaitingConfirm)
			log.Printf("[ws] 還原 pending_denials for session %s", sessionID)
		} else {
			status := StateIdle
			if v, ok := statusRegistry.Load(sessionID); ok {
				if s, ok := v.(string); ok && s != "" {
					status = s
				}
			}
			send(serverMsg{Type: "status", Value: status})
			statusRegistry.Store(sessionID, status)
		}

		// runAgent 啟動子進程並串流結果
		runAgent := func(prompt string) {
			mu.Lock()
			if cancelFn != nil {
				cancelFn()
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancelFn = cancel

			extra := map[string]string{}
			if isClaude {
				extra[agent.ArgPermissionMode] = permMode
				if len(allowedTools) > 0 {
					extra[agent.ArgAllowedTools] = strings.Join(allowedTools, ",")
				}
			}
			// Cursor：CLI 的 --force 對應「放寬指令／工具核准」；與 DB 的 bypassPermissions 對齊
			if agentType == agent.TypeCursor && permMode == "bypassPermissions" {
				extra[agent.ArgForce] = "true"
			}

			opts := agent.RunOptions{
				Prompt:    prompt,
				SessionID: agentSessionID,
				WorkDir:   sess.WorkDir,
				ExtraArgs: extra,
			}
			mu.Unlock()

			runner, err := agent.NewRunner(agentType)
			if err != nil {
				log.Printf("[ws] 無法建立 %s runner: %v", agentType, err)
				relaySend(serverMsg{Type: "error", Content: err.Error()})
				setAndSendStatus(StateIdle)
				return
			}

			log.Printf("[ws] 啟動 %s.Run agentSessionID=%q mode=%s", runner.Name(), opts.SessionID, permMode)
			setAndSendStatus(StateThinking)

			go func(opts agent.RunOptions) {
				var responseBuf strings.Builder
				permDenied := false

				err := runner.Run(ctx, opts, func(e agent.Event) {
					switch e.Type {
					case agent.EventStreamStart:
						setAndSendStatus(StateStreaming)

					case agent.EventDelta:
						responseBuf.WriteString(e.Text)
						relaySend(serverMsg{Type: "delta", Content: e.Text})

					case agent.EventSessionInit:
						mu.Lock()
						if e.SessionID != "" && e.SessionID != agentSessionID {
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
						setAndSendStatus(StateAwaitingConfirm)
						relaySend(serverMsg{Type: "permission_request", Tools: e.Denials})
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

						if resp := responseBuf.String(); resp != "" {
							if err := database.AddMessage(sessionID, "claude", resp); err != nil {
								log.Printf("[ws] 儲存 assistant 訊息失敗: %v", err)
							}
						}

						if !permDenied {
							clearPendingDenials(database, sessionID)
							setAndSendStatus(StateIdle)
							go tg.Notify(botToken, tgUserID, fmt.Sprintf("✅ *%s* 任務完成", sess.Name))
						}

					case agent.EventError:
						if e.Err != nil {
							relaySend(serverMsg{Type: "error", Content: e.Err.Error()})
						}
					}
				})

				if err != nil {
					if ctx.Err() != nil {
						log.Printf("[ws] %s.Run 被 context 取消", agentType)
					} else {
						log.Printf("[ws] %s.Run 執行錯誤: %v", agentType, err)
						relaySend(serverMsg{Type: "error", Content: err.Error()})
					}
					if !permDenied {
						setAndSendStatus(StateIdle)
					}
				} else {
					log.Printf("[ws] %s.Run 正常結束", agentType)
				}
			}(opts)
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
				runAgent(msg.Data)

			case "allow_once":
				if !isClaude {
					log.Printf("[ws] agent=%s 不支援 allow_once，忽略", agentType)
					continue
				}
				clearPendingDenials(database, sessionID)
				mu.Lock()
				existing := make(map[string]bool)
				for _, t := range allowedTools {
					existing[t] = true
				}
				for _, t := range msg.Tools {
					existing[t] = true
				}
				merged := make([]string, 0, len(existing))
				for t := range existing {
					merged = append(merged, t)
				}
				allowedTools = merged
				mu.Unlock()
				if err := database.UpdateAllowedTools(sessionID, merged); err != nil {
					log.Printf("[ws] 更新 allowed_tools 失敗: %v", err)
				}
				runAgent("please retry the previous operation")

			case "set_mode":
				if agentType != agent.TypeClaude && agentType != agent.TypeCursor {
					log.Printf("[ws] agent=%s 不支援 set_mode，忽略", agentType)
					continue
				}
				clearPendingDenials(database, sessionID)
				mu.Lock()
				permMode = msg.Mode
				mu.Unlock()
				if err := database.UpdatePermissionMode(sessionID, msg.Mode); err != nil {
					log.Printf("[ws] 更新 permission_mode 失敗: %v", err)
				}
				setAndSendStatus(StateIdle)
				log.Println("[ws] permission mode 切換為:", msg.Mode)

			case "reset_context":
				mu.Lock()
				if cancelFn != nil {
					cancelFn()
					cancelFn = nil
				}
				agentSessionID = ""
				mu.Unlock()
				if err := database.UpdateAgentSessionID(sessionID, ""); err != nil {
					log.Printf("[ws] 清除 agent_session_id 失敗: %v", err)
				}
				if err := database.ClearMessages(sessionID); err != nil {
					log.Printf("[ws] 清除訊息失敗: %v", err)
				}
				clearPendingDenials(database, sessionID)
				send(serverMsg{Type: "reset"})
				setAndSendStatus(StateIdle)
				log.Printf("[ws] session %s context 已重置", sessionID)

			case "interrupt":
				mu.Lock()
				if cancelFn != nil {
					cancelFn()
					cancelFn = nil
				}
				mu.Unlock()
				setAndSendStatus(StateIdle)
			}
		}
	}
}
