// Package mcp 讓其他 agent 透過 MCP（Streamable HTTP）操作本專案的 session。
//
// 送出/等結果分離成非阻塞設計：tool call 立刻回，實際執行狀態靠 loopback 到
// 既有 /sessions/:id/ws 的 WebSocket client 讀取、快取，get_status 再輪詢讀出。
// 不重寫 internal/ws 的執行邏輯，只是多一個呼叫方。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
)

// 對外簡化過的狀態機，對應 internal/ws 內部各種 STATE 字串。
const (
	StateIdle               = "idle"
	StateRunning            = "running"
	StateAwaitingPermission = "awaiting_permission"
	StateAwaitingInput      = "awaiting_input"
	StateShellPending       = "shell_pending"
)

type pendingShell struct {
	Command string `json:"command"`
	WorkDir string `json:"work_dir"`
}

// sessionState 是單一 session 的即時狀態快取，由 wsClient 的讀取 goroutine 更新。
type sessionState struct {
	mu                sync.Mutex
	conn              *websocket.Conn
	state             string
	latestText        string
	pendingPermission json.RawMessage
	pendingQuestion   json.RawMessage
	pendingShellCmd   *pendingShell
	lastErr           string
	// turnDone 由 send 建立，本輪離開 running（idle／等授權／等 shell 確認）或連線中斷時關閉，供 ask_session 等待。
	turnDone chan struct{}
	// writeMu 序列化 conn 寫入：websocket 只允許單一並行 writer，多個 MCP 呼叫同時操作同一 session 會寫壞 frame。
	writeMu sync.Mutex
}

func (s *sessionState) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteJSON(v)
}

// endTurnLocked 呼叫前須持有 s.mu。
func (s *sessionState) endTurnLocked() {
	if s.turnDone != nil {
		close(s.turnDone)
		s.turnDone = nil
	}
}

func (s *sessionState) snapshot() (state, text string, perm json.RawMessage, shell *pendingShell, lastErr string, question json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.latestText, s.pendingPermission, s.pendingShellCmd, s.lastErr, s.pendingQuestion
}

// serverEvent 只解析我們關心的欄位，其餘忽略（鏡射 internal/ws.serverMsg）。
type serverEvent struct {
	Question json.RawMessage `json:"question"`
	Type     string          `json:"type"`
	Value    string          `json:"value"`
	Content  string          `json:"content"`
	Tools    json.RawMessage `json:"tools"`
	Command  string          `json:"command"`
	WorkDir  string          `json:"work_dir"`
}

func normalizeState(v string) string {
	switch v {
	case "AWAITING_INPUT":
		return StateAwaitingInput
	case "AWAITING_CONFIRM":
		return StateAwaitingPermission
	case "AWAITING_SHELL_CONFIRM", "SHELL_AWAITING_APPROVAL":
		return StateShellPending
	case "IDLE", "SHELL_IDLE":
		return StateIdle
	default:
		return StateRunning // THINKING / STREAMING / SHELL_RUNNING / SHELL_EXEC ...
	}
}

// Registry 管理每個 session 一條 loopback WS 連線與其狀態快取。
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	askLocks sync.Map                      // sessionID → *sync.Mutex
	wsURL    func(sessionID string) string // ws://127.0.0.1:<port>/sessions/:id/ws
	header   http.Header                   // 帶 mcp_token 的 Authorization header
}

func NewRegistry(wsURL func(sessionID string) string, authHeader http.Header) *Registry {
	return &Registry{
		sessions: make(map[string]*sessionState),
		wsURL:    wsURL,
		header:   authHeader,
	}
}

// ensure 回傳該 session 的狀態快取；若尚未連線就 dial 並起讀取 goroutine。
func (r *Registry) ensure(sessionID string) (*sessionState, error) {
	r.mu.Lock()
	st, ok := r.sessions[sessionID]
	if ok && st.conn != nil {
		r.mu.Unlock()
		return st, nil
	}
	r.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(r.wsURL(sessionID), r.header)
	if err != nil {
		return nil, fmt.Errorf("連線 session %s 失敗: %w", sessionID, err)
	}

	st = &sessionState{conn: conn, state: StateIdle}
	r.mu.Lock()
	// 並行呼叫時可能兩邊都 dial 了：以先登記者為準，自己這條關掉，避免兩條連線、狀態快取分裂。
	if cur, ok := r.sessions[sessionID]; ok && cur.conn != nil {
		r.mu.Unlock()
		conn.Close()
		return cur, nil
	}
	r.sessions[sessionID] = st
	r.mu.Unlock()

	go r.readLoop(sessionID, st)
	return st, nil
}

func (r *Registry) readLoop(sessionID string, st *sessionState) {
	defer func() {
		r.mu.Lock()
		if r.sessions[sessionID] == st { // 只刪自己，別刪到之後重連的新 entry
			delete(r.sessions, sessionID)
		}
		r.mu.Unlock()
		st.mu.Lock()
		if st.turnDone != nil {
			st.lastErr = "與 session 的連線中斷"
			st.endTurnLocked()
		}
		st.mu.Unlock()
		st.conn.Close()
	}()
	for {
		_, data, err := st.conn.ReadMessage()
		if err != nil {
			return
		}
		var ev serverEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		st.mu.Lock()
		switch ev.Type {
		case "status", "sync":
			st.state = normalizeState(ev.Value)
		case "delta", "thinking":
			st.latestText += ev.Content
		case "message_result_text":
			st.latestText = ev.Content
			st.state = StateIdle
		case "question_request":
			st.pendingPermission = nil
			st.pendingQuestion = ev.Question
			st.state = StateAwaitingInput
		case "question_resolved":
			st.pendingQuestion = nil
		case "permission_request":
			st.pendingPermission = ev.Tools
			st.state = StateAwaitingPermission
		case "shell_command_request", "shell_approval_request":
			st.pendingShellCmd = &pendingShell{Command: ev.Command, WorkDir: ev.WorkDir}
			st.state = StateShellPending
		case "shell_delta":
			st.latestText += ev.Content
		case "shell_done":
			st.state = StateIdle
		case "shell_error", "error":
			st.lastErr = ev.Content
		}
		// 只在「本輪最後一個事件」結束：message_result_text／shell_done 後面還會跟一個 status，
		// 若提早結束，那個尾巴 status 會把下一輪誤判成已完成。等授權則等 permission_request（帶內容）才算。
		switch {
		case ev.Type == "status" && st.state == StateIdle,
			ev.Type == "permission_request",
			ev.Type == "question_request",
			ev.Type == "shell_command_request",
			ev.Type == "shell_approval_request":
			st.endTurnLocked()
		}
		st.mu.Unlock()
	}
}

func (r *Registry) send(sessionID string, payload any) error {
	_, _, err := r.begin(sessionID, payload)
	return err
}

// begin 送出一輪操作並回傳本輪的結束通知 channel。
func (r *Registry) begin(sessionID string, payload any) (<-chan struct{}, *sessionState, error) {
	st, err := r.ensure(sessionID)
	if err != nil {
		return nil, nil, err
	}
	st.mu.Lock()
	// 新一輪操作開始：清掉上一輪殘留的文字/錯誤，讓 get_status 只看到這一輪的結果。
	st.latestText = ""
	st.lastErr = ""
	st.pendingPermission = nil
	st.pendingQuestion = nil
	st.state = StateRunning
	st.endTurnLocked() // 上一輪若還有人在等，視為結束（被新操作取代）
	done := make(chan struct{})
	st.turnDone = done
	st.mu.Unlock()
	return done, st, st.writeJSON(payload)
}

// AskResult 是 ask_session 等到的一輪結果。
type AskResult struct {
	State             string
	Text              string
	PendingPermission json.RawMessage
	PendingQuestion   json.RawMessage
	Error             string
	TimedOut          bool
}

// Ask 送出訊息並阻塞到該輪結束、timeout 或 ctx 取消。timeout 時回傳目前累積的部分文字。
// ponytail: 同一目標 session 的 Ask 以 askLock 序列化，但 send_message／set_model 等其他 begin() 呼叫不受此鎖；
// 它們會結束目前這輪，等待中的 Ask 以 Error 回報「被中斷」。要嚴格互斥就得改成依 msgID 追蹤每一輪。
func (r *Registry) Ask(ctx context.Context, sessionID, text string, timeout time.Duration) (AskResult, error) {
	lock := r.askLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	done, st, err := r.begin(sessionID, map[string]string{"type": "input", "data": text})
	if err != nil {
		return AskResult{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var timedOut bool
	select {
	case <-done:
	case <-timer.C:
		timedOut = true
	case <-ctx.Done():
		return AskResult{}, ctx.Err()
	}
	state, latest, perm, _, lastErr, question := st.snapshot()
	// 沒逾時卻還是 running：本輪被同 session 的其他操作（set_model 等 send）取代，結果不完整。
	if !timedOut && state == StateRunning && lastErr == "" {
		lastErr = "本輪等待被同一 session 的其他操作中斷，回覆可能不完整；請用 get_status 查看"
	}
	return AskResult{State: state, Text: latest, PendingPermission: perm, PendingQuestion: question, Error: lastErr, TimedOut: timedOut}, nil
}

func (r *Registry) askLock(sessionID string) *sync.Mutex {
	m, _ := r.askLocks.LoadOrStore(sessionID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Status 回傳目前狀態快取，供 get_status tool 使用。
func (r *Registry) Status(sessionID string) (state, text string, perm json.RawMessage, shell *pendingShell, lastErr string, connected bool, question json.RawMessage) {
	r.mu.Lock()
	st, ok := r.sessions[sessionID]
	r.mu.Unlock()
	if !ok {
		return StateIdle, "", nil, nil, "", false, nil
	}
	state, text, perm, shell, lastErr, question = st.snapshot()
	return state, text, perm, shell, lastErr, true, question
}

func (r *Registry) SendMessage(sessionID, text string) error {
	return r.send(sessionID, map[string]string{"type": "input", "data": text})
}

func (r *Registry) ShellExec(sessionID, command string) error {
	return r.send(sessionID, map[string]string{"type": "shell_exec", "data": command})
}

func (r *Registry) RespondPermission(sessionID, decision string, tools []string) error {
	switch decision {
	case "allow_once":
		return r.send(sessionID, map[string]any{"type": "allow_once", "tools": tools})
	case "deny_once":
		return r.send(sessionID, map[string]string{"type": "deny_once"})
	default:
		return fmt.Errorf("未知的 decision: %s（僅支援 allow_once / deny_once）", decision)
	}
}

func (r *Registry) SetPermissionMode(sessionID, mode string) error {
	return r.send(sessionID, map[string]string{"type": "set_mode", "mode": mode})
}

func (r *Registry) SetModel(sessionID, model string) error {
	return r.send(sessionID, map[string]string{"type": "set_model", "model": model})
}

func (r *Registry) SetEffort(sessionID, effort string) error {
	return r.send(sessionID, map[string]string{"type": "set_effort", "effort": effort})
}

// ResumeQueue 對應 UI 的 queue_resume：只送 WS 訊息，不走 begin()。
// begin() 會清掉 latestText／lastErr 並把狀態設成 running；佇列為空時 resume 不會啟動 run，狀態會卡在 running。
func (r *Registry) ResumeQueue(sessionID string) error {
	st, err := r.ensure(sessionID)
	if err != nil {
		return err
	}
	return st.writeJSON(map[string]string{"type": "queue_resume"})
}

func (r *Registry) Interrupt(sessionID string) error {
	st, err := r.ensure(sessionID)
	if err != nil {
		return err
	}
	return st.writeJSON(map[string]string{"type": "interrupt"})
}
