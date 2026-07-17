package kiroacp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

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
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// client 是單一 kiro-cli acp 子進程的 JSON-RPC over stdio 客戶端。
type client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu      sync.Mutex
	nextID  atomic.Int64
	pending map[int64]chan rpcResponse

	onUpdate func(sessionUpdateBody)

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
	}
	go c.readLoop()
	return c
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
			log.Printf("[kiroacp] skip non-json line: %s", truncateBytes(line, 120))
			continue
		}
		c.dispatch(msg)
	}
	if err := sc.Err(); err != nil {
		c.readErr = err
	}
}

func (c *client) dispatch(msg rpcResponse) {
	// Server → client request（例如 fs）；回傳 method not found，避免卡住。
	if msg.Method != "" && msg.ID != nil && msg.Result == nil && msg.Error == nil {
		c.mu.Lock()
		errResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      *msg.ID,
			"error": map[string]any{
				"code":    -32601,
				"message": "client method not implemented: " + msg.Method,
			},
		}
		b, _ := json.Marshal(errResp)
		_, _ = c.stdin.Write(append(b, '\n'))
		c.mu.Unlock()
		return
	}

	if msg.Method == "session/update" {
		var p sessionUpdateParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			log.Printf("[kiroacp] session/update unmarshal: %v", err)
			return
		}
		var body sessionUpdateBody
		if err := json.Unmarshal(p.Update, &body); err != nil {
			log.Printf("[kiroacp] session/update body: %v", err)
			return
		}
		if c.onUpdate != nil {
			c.onUpdate(body)
		}
		return
	}

	if msg.ID != nil {
		c.mu.Lock()
		ch, ok := c.pending[*msg.ID]
		if ok {
			delete(c.pending, *msg.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

func (c *client) write(req rpcRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *client) notify(method string, params any) error {
	return c.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.readDone:
		return nil, fmt.Errorf("acp stdout closed: %w", c.readErr)
	case msg := <-ch:
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	}
}

func (c *client) close() {
	_ = c.stdin.Close()
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
