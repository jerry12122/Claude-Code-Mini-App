package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/proc"
)

const acpRequestTimeout = 15 * time.Second

// ACP is line-delimited JSON-RPC. Raw messages are never included in errors:
// the agent can echo the session's private instructions in any response.
type acpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *acpRPCError) Error() string {
	return fmt.Sprintf("antigravity ACP request failed (RPC code %d)", e.Code)
}

type acpReply struct {
	result json.RawMessage
	err    error
}

type acpHandlers struct {
	notification func(acpMessage) error
	request      func(context.Context, acpMessage) (any, error)
}

type acpClient struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	writeGate    chan struct{}
	nextID       atomic.Int64
	mu           sync.Mutex
	pending      map[int64]chan acpReply
	terminal     error
	done         chan struct{}
	readDone     chan struct{}
	waitDone     chan struct{}
	waitErr      error
	stopOnce     sync.Once
	cancelOnce   sync.Once
	sessionID    string // protected by mu
	handlers     acpHandlers
	requests     chan acpMessage
	workerDone   chan struct{}
	workerCtx    context.Context
	workerCancel context.CancelFunc
}

func startACP(ctx context.Context, opts agent.RunOptions, handlers ...acpHandlers) (*acpClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The caller's context is watched below so shutdown owns process-tree cleanup.
	args := []string{"acp"}
	if mode := strings.TrimSpace(opts.ExtraArgs[agent.ArgPermissionMode]); mode == "bypassPermissions" || mode == "yolo" {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.CommandContext(context.Background(), Binary(), args...)
	configureCmd(cmd)
	cmd.Dir = opts.WorkDir
	// os/exec drains stderr separately; no unbounded buffer or private diagnostics.
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("antigravity ACP stdin setup failed")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, errors.New("antigravity ACP stdout setup failed")
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, errors.New("antigravity ACP process could not start")
	}
	c := &acpClient{
		cmd: cmd, stdin: stdin, stdout: stdout, writeGate: make(chan struct{}, 1),
		pending: make(map[int64]chan acpReply), done: make(chan struct{}),
		readDone: make(chan struct{}), waitDone: make(chan struct{}),
		requests: make(chan acpMessage, 16), workerDone: make(chan struct{}),
	}
	c.workerCtx, c.workerCancel = context.WithCancel(context.Background())
	if len(handlers) > 0 {
		c.handlers = handlers[0]
	}
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}
	go c.readLoop()
	go c.requestWorker()
	go func() {
		<-c.readDone
		c.waitErr = cmd.Wait()
		close(c.waitDone)
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.abort(ctx.Err())
		case <-c.done:
			// A clean EOF can follow a final response and process exit. Let
			// Wait observe its actual status before deciding to kill it.
			if errors.Is(c.failure(), io.EOF) {
				select {
				case <-c.waitDone:
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}
		c.stop()
	}()
	return c, nil
}

func (c *acpClient) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return
	}
	c.terminal = err
	for id, ch := range c.pending {
		ch <- acpReply{err: err}
		delete(c.pending, id)
	}
	close(c.done)
}

func (c *acpClient) stop() {
	c.stopOnce.Do(func() {
		c.workerCancel()
		_ = c.stdin.Close()
		select {
		case <-c.waitDone:
		case <-time.After(100 * time.Millisecond):
			_ = proc.KillTree(c.cmd.Process.Pid)
		}
		_ = c.stdout.Close()
	})
}

func (c *acpClient) close() error {
	c.fail(errors.New("antigravity ACP transport closed"))
	c.stop()
	select {
	case <-c.waitDone:
		return c.waitWorker()
	case <-time.After(6 * time.Second):
		return errors.New("antigravity ACP process cleanup timed out")
	}
}

func (c *acpClient) waitWorker() error {
	select {
	case <-c.workerDone:
		return nil
	case <-time.After(time.Second):
		return errors.New("antigravity ACP permission handler cleanup timed out")
	}
}

func (c *acpClient) setSessionID(id string) {
	c.mu.Lock()
	c.sessionID = id
	c.mu.Unlock()
}

func (c *acpClient) abort(err error) {
	c.cancelOnce.Do(func() {
		c.mu.Lock()
		id := c.sessionID
		c.mu.Unlock()
		if id != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_ = c.write(ctx, map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": id}})
		}
	})
	c.fail(err)
}

// finish permits native ACP servers to stay alive until stdin closes, and
// distinguishes a natural process failure from our intentional final cleanup.
func (c *acpClient) finish() error {
	if err := c.failure(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	_ = c.stdin.Close()
	select {
	case <-c.waitDone:
		if c.waitErr != nil {
			return fmt.Errorf("antigravity ACP process exited unsuccessfully: %w", c.waitErr)
		}
	case <-time.After(250 * time.Millisecond):
		if err := c.failure(); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		c.stop()
		select {
		case <-c.waitDone:
		case <-time.After(6 * time.Second):
			return errors.New("antigravity ACP process cleanup timed out")
		}
	}
	c.workerCancel()
	if err := c.waitWorker(); err != nil {
		return err
	}
	return nil
}

func (c *acpClient) requestWorker() {
	defer close(c.workerDone)
	for {
		select {
		case <-c.workerCtx.Done():
			return
		case msg := <-c.requests:
			ctx, cancel := context.WithTimeout(c.workerCtx, acpPromptTimeout)
			var result any
			err := errors.New("antigravity ACP requested an unsupported client interaction")
			if c.handlers.request != nil {
				result, err = c.handlers.request(ctx, msg)
			}
			cancel()
			reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
			if err != nil {
				reply["error"] = map[string]any{"code": -32601, "message": "Unsupported client interaction"}
			} else {
				reply["result"] = result
			}
			wctx, wcancel := context.WithTimeout(context.Background(), acpRequestTimeout)
			writeErr := c.write(wctx, reply)
			wcancel()
			if writeErr != nil {
				c.fail(writeErr)
				return
			}
			if err != nil {
				c.fail(err)
				return
			}
		}
	}
}

func (c *acpClient) readLoop() {
	defer close(c.readDone)
	sc := bufio.NewScanner(c.stdout)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var msg acpMessage
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.JSONRPC != "2.0" {
			c.fail(errors.New("antigravity ACP returned an invalid JSON-RPC message"))
			return
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 && string(msg.ID) != "null" {
				select {
				case c.requests <- msg:
				default:
					c.fail(errors.New("antigravity ACP client request queue exceeded"))
					return
				}
			} else if c.handlers.notification != nil {
				if err := c.handlers.notification(msg); err != nil {
					c.fail(err)
					return
				}
			}
			continue
		}
		var id int64
		if json.Unmarshal(msg.ID, &id) != nil || id <= 0 {
			c.fail(errors.New("antigravity ACP returned an invalid response ID"))
			return
		}
		if (msg.Error == nil) == (len(msg.Result) == 0) {
			c.fail(errors.New("antigravity ACP returned an invalid response"))
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		if ch != nil {
			reply := acpReply{result: msg.Result}
			if msg.Error != nil {
				reply.err = msg.Error
			}
			ch <- reply
		}
		c.mu.Unlock()
	}
	if sc.Err() != nil {
		c.fail(errors.New("antigravity ACP stdout read failed"))
	} else {
		c.fail(fmt.Errorf("antigravity ACP stdout closed: %w", io.EOF))
	}
}

func (c *acpClient) write(ctx context.Context, msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return errors.New("antigravity ACP request encoding failed")
	}
	select {
	case c.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.failure()
	}
	written := make(chan error, 1)
	go func() {
		defer func() { <-c.writeGate }()
		_, err := c.stdin.Write(append(payload, '\n'))
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			c.fail(errors.New("antigravity ACP stdin write failed"))
			return c.failure()
		}
		return nil
	case <-ctx.Done():
		// A partial line cannot be reused safely by a later request.
		c.fail(ctx.Err())
		return ctx.Err()
	case <-c.done:
		return c.failure()
	}
}

func (c *acpClient) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal
}

func (c *acpClient) request(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if timeout <= 0 {
		timeout = acpRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan acpReply, 1)
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return nil, err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.write(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		return reply.result, reply.err
	case <-ctx.Done():
		c.abort(ctx.Err())
		return nil, ctx.Err()
	}
}
