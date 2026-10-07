package kiroacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
)

// rpcRequest 是送往 kiro-cli acp 的 JSON-RPC 請求。
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse 是 JSON-RPC 回應（含 notification：無 id）。
// ID 用 json.RawMessage 容忍 server 端可能用字串或數字 id（ACP request_permission 實測非 int）。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// hasID 回報是否帶有效 id（非空且非 null）。
func (r rpcResponse) hasID() bool {
	return len(r.ID) > 0 && string(r.ID) != "null"
}

// intID 嘗試把 id 解析為 int64（用於配對本地送出的請求）。
func (r rpcResponse) intID() (int64, bool) {
	if !r.hasID() {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(r.ID, &n); err == nil {
		return n, true
	}
	return 0, false
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"` // -32603 "Internal error" 的真正原因常在這裡
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("jsonrpc error %d: %s (%s)", e.Code, e.Message, truncateBytes(e.Data, 300))
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// errStdoutClosed：kiro-cli 的 stdout 關閉（進程死掉／被殺）。
var errStdoutClosed = errors.New("acp stdout closed")

// sessionNewResult 對應 session/new（與 load）的主要欄位。
type sessionNewResult struct {
	SessionID string `json:"sessionId"`
	Models    *struct {
		CurrentModelID  string `json:"currentModelId"`
		AvailableModels []struct {
			ModelID string `json:"modelId"`
			Name    string `json:"name"`
		} `json:"availableModels"`
	} `json:"models"`
}

// sessionUpdateParams 對應 session/update notification。
type sessionUpdateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

type sessionUpdateBody struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Status        string          `json:"status,omitempty"`
	Text          string          `json:"text,omitempty"`
	// RawOutput 是 tool_call/tool_call_update 帶回的 MCP 工具原始輸出（如截圖工具），
	// 結構為 rawOutput.items[].Json.content[]，每項是標準 MCP content block（text/image/...）。
	RawOutput json.RawMessage `json:"rawOutput,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// mcpContentBlock 對應標準 MCP tool result 的 content block（text/image/...）。
type mcpContentBlock struct {
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// imageBlock 是從 tool_call_update.rawOutput 取出的圖片資料，交給 media.SaveBase64Image 落地存檔。
type imageBlock struct {
	MediaType string
	Data      string
}

// images 掃描 body.RawOutput（rawOutput.items[].Json.content[]），取出所有 image content block。
// 非 MCP 工具（無 rawOutput）或 content 非陣列時回傳空。
func (b sessionUpdateBody) images() []imageBlock {
	if len(b.RawOutput) == 0 {
		return nil
	}
	var raw struct {
		Items []struct {
			Json struct {
				Content []mcpContentBlock `json:"content"`
			} `json:"Json"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b.RawOutput, &raw); err != nil {
		return nil
	}
	var out []imageBlock
	for _, item := range raw.Items {
		for _, c := range item.Json.Content {
			if c.Type == "image" && c.Data != "" {
				out = append(out, imageBlock{MediaType: c.MimeType, Data: c.Data})
			}
		}
	}
	return out
}

// permissionParams 對應 session/request_permission 的 params。
type permissionParams struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ToolCallID string `json:"toolCallId"`
		Title      string `json:"title"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// client 是單一 kiro-cli acp 子進程的 JSON-RPC over stdio 客戶端。
type client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu      sync.Mutex // 只保護 pending；絕不可在持有它時寫 stdin
	nextID  atomic.Int64
	pending map[int64]chan rpcResponse

	// 寫入 stdin 可能卡在 pipe（CLI 不讀 stdin、params 又大於 pipe 容量）。寫入都在獨立 goroutine、由 wmu 序列化，
	// 呼叫端用自己的 ctx／close 放棄等待；真正解除阻塞的是子進程結束（讀端關閉）。
	wmu            sync.Mutex
	broken         atomic.Bool  // 曾放棄過一次寫入：串流可能停在半行，之後一律不再寫，避免把壞資料送給下一輪
	inflightWrites atomic.Int32 // 測試用：還沒結束的寫入 goroutine 數
	closed         chan struct{}
	closeOnce      sync.Once
	// 授權請求逐一處理（WS 一次只持有一個待授權），但不佔用 readLoop。
	// 佇列上限 permQueueSize：超量的授權請求直接回「取消」，絕不阻塞 readLoop（否則讀不到 EOF／回應）。
	permQ    chan func()
	permDone chan struct{} // permWorker 結束時關閉：收尾時用來確認舊授權流程已結束

	onUpdate func(sessionUpdateBody)
	// onPermission 處理 server→client 的 session/request_permission。
	// 回傳選定的 optionID；空字串代表拒絕／取消。nil 時 dispatch 會退回 -32601。
	onPermission func(permissionParams) string

	readDone chan struct{}
	readErr  error
}

func newClient(cmd *exec.Cmd, stdout io.ReadCloser, stdin io.WriteCloser) *client {
	c := &client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		pending:  make(map[int64]chan rpcResponse),
		readDone: make(chan struct{}),
		closed:   make(chan struct{}),
		permQ:    make(chan func(), permQueueSize),
		permDone: make(chan struct{}),
	}
	go c.readLoop()
	go c.permWorker()
	return c
}

// permQueueSize：等待中的授權請求上限。實務上 kiro 一次只會問一個；這個數字只是防護上限。
const permQueueSize = 32

func (c *client) permWorker() {
	defer close(c.permDone)
	for {
		select {
		case f := <-c.permQ:
			f()
		case <-c.closed:
			return
		}
	}
}

func (c *client) readLoop() {
	defer close(c.readDone)
	sc := bufio.NewScanner(c.stdout)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcResponse
		if err := json.Unmarshal(line, &msg); err != nil {
			slog.Info(fmt.Sprintf("[kiroacp] skip non-json line: %s", truncateBytes(line, 120)))
			continue
		}
		c.dispatch(msg)
	}
	if err := sc.Err(); err != nil {
		c.readErr = err
	}
}

func (c *client) dispatch(msg rpcResponse) {
	// Server → client request。
	if msg.Method != "" && msg.hasID() && msg.Result == nil && msg.Error == nil {
		// 互動式授權：session/request_permission。
		if msg.Method == "session/request_permission" && c.onPermission != nil {
			var p permissionParams
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				slog.Info(fmt.Sprintf("[kiroacp] request_permission unmarshal: %v", err))
			} else {
				// 交給 permWorker 依序處理（一次只問一個）：不可卡住 readLoop，否則等授權期間進程死掉／回應 prompt 都讀不到，
				// 這一輪收尾不了，也就無法取消這次 attempt 的 ctx 來釋放還在等的授權。
				id, cb := msg.ID, c.onPermission
				select {
				case c.permQ <- func() { c.replyPermission(id, cb(p)) }:
				default:
					// 佇列已滿：直接取消這個超量的授權（在獨立 goroutine 回覆，不佔用 readLoop）。
					slog.Info(fmt.Sprintf("[kiroacp] 授權請求超過上限 %d，直接取消: %s", permQueueSize, p.ToolCall.Title))
					go c.replyPermission(id, "")
				}
				return
			}
		}
		// 其他 client 請求（例如 fs）或無 handler：回 method not found，避免卡住。
		errResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"error": map[string]any{
				"code":    -32601,
				"message": "client method not implemented: " + msg.Method,
			},
		}
		b, _ := json.Marshal(errResp)
		go func() { _ = c.writeLine(context.Background(), b) }() // 不可在 readLoop 內同步寫
		return
	}

	if msg.Method == "session/update" {
		var p sessionUpdateParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			slog.Info(fmt.Sprintf("[kiroacp] session/update unmarshal: %v", err))
			return
		}
		var body sessionUpdateBody
		if err := json.Unmarshal(p.Update, &body); err != nil {
			slog.Info(fmt.Sprintf("[kiroacp] session/update body: %v", err))
			return
		}
		if c.onUpdate != nil {
			c.onUpdate(body)
		}
		return
	}

	if id, ok := msg.intID(); ok {
		c.mu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

var errWriteAbandoned = errors.New("acp stdin 寫入已放棄")

// writeLine 寫一行到 stdin，受 ctx 與 close() 控制。放棄等待後 broken 會擋掉之後所有寫入（含仍在排隊的）。
// 卡在 pipe 的那次 Write 會留在它的 goroutine 裡，直到子進程結束讀端關閉；每個 client 只對應一個子進程，
// 不會有跨 attempt 的寫入者。
func (c *client) writeLine(ctx context.Context, b []byte) error {
	if c.broken.Load() {
		return errWriteAbandoned
	}
	line := append(append([]byte(nil), b...), '\n')
	done := make(chan error, 1)
	c.inflightWrites.Add(1)
	go func() {
		defer c.inflightWrites.Add(-1)
		c.wmu.Lock()
		defer c.wmu.Unlock()
		if c.broken.Load() {
			done <- errWriteAbandoned
			return
		}
		_, err := c.stdin.Write(line)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		c.broken.Store(true)
		return ctx.Err()
	case <-c.closed:
		c.broken.Store(true)
		return errWriteAbandoned
	}
}

func (c *client) write(ctx context.Context, req rpcRequest) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.writeLine(ctx, b)
}

func (c *client) notify(ctx context.Context, method string, params any) error {
	return c.write(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

// replyPermission 回覆 session/request_permission。
// optionID 非空 → selected；空字串 → cancelled（等同拒絕）。id 原樣 echo。
func (c *client) replyPermission(id json.RawMessage, optionID string) {
	var outcome map[string]any
	if optionID != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": optionID}
	} else {
		outcome = map[string]any{"outcome": "cancelled"}
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"outcome": outcome},
	})
	_ = c.writeLine(context.Background(), b) // 受 close() 控制；不綁 attempt ctx，因為授權回覆本來就該在 attempt 進行中送出
}

func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(ctx, rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	result := func(msg rpcResponse) (json.RawMessage, error) {
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.readDone:
		// 回應與 EOF 同時就緒時（例如 kiro 回完錯誤就退出）以回應為準：error.data 是分類用的證據，不能被 select 隨機丟掉。
		// readLoop 先派送回應、才關 readDone，所以此時回應若存在一定已在 ch 裡。
		select {
		case msg := <-ch:
			return result(msg)
		default:
		}
		if c.readErr != nil {
			return nil, fmt.Errorf("%w: %v", errStdoutClosed, c.readErr)
		}
		return nil, errStdoutClosed
	case msg := <-ch:
		return result(msg)
	}
}

// close 放棄所有等待中的寫入並關閉 stdin。實測（Windows、Go os.File pipe）寫入卡在 pipe 時 Close 會立刻返回，
// 並讓卡住的 Write 以「file already closed」結束；子進程結束（讀端關閉）同樣會解除。
func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.stdin.Close()
	})
}
func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func parseSessionResult(raw json.RawMessage) (sessionNewResult, error) {
	var out sessionNewResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func extractAgentText(body sessionUpdateBody) string {
	if body.SessionUpdate != "agent_message_chunk" {
		return ""
	}
	if body.Text != "" {
		return body.Text
	}
	var tc textContent
	if err := json.Unmarshal(body.Content, &tc); err == nil && tc.Text != "" {
		return tc.Text
	}
	// content 可能是字串
	var s string
	if err := json.Unmarshal(body.Content, &s); err == nil {
		return s
	}
	return ""
}
