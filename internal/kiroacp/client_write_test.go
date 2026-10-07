package kiroacp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 回歸（io.Pipe）：CLI 不讀 stdin、params 大於 pipe 容量時，call 必須受 ctx 控制，不能卡在同步寫入。
func TestClientCallDeadlineInterruptsBlockedWrite(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	c := newClient(nil, stdoutR, stdinW)
	defer func() { stdinR.Close(); stdinW.Close(); stdoutW.Close(); stdoutR.Close(); c.close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.call(ctx, "session/prompt", strings.Repeat("x", 1<<20)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("逾時原因不見了: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("寫入卡住時 call 沒有遵守 ctx 逾時")
	}

	// 放棄後：pending 已清掉、c.mu 沒被寫入佔住、之後的寫入一律拒絕（不把壞資料送給下一輪）。
	if !c.mu.TryLock() {
		t.Fatal("c.mu 不可被卡住的寫入佔住")
	}
	if len(c.pending) != 0 {
		t.Fatalf("pending 應已清掉: %d", len(c.pending))
	}
	c.mu.Unlock()
	start := time.Now()
	if _, err := c.call(context.Background(), "x", nil); !errors.Is(err, errWriteAbandoned) || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("放棄寫入後的呼叫應立刻拒絕: err=%v after %v", err, time.Since(start))
	}
	// 讀端關閉（＝子進程結束）後，卡住的寫入 goroutine 要釋放。
	stdinR.Close()
	waitZero(t, &c.inflightWrites)
}

// close() 要能放掉所有等待：卡住的寫入、排隊的寫入、授權回覆都不可永遠卡住。
func TestClientCloseReleasesBlockedWriters(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	c := newClient(nil, stdoutR, stdinW)
	defer func() { stdinR.Close(); stdinW.Close(); stdoutW.Close(); stdoutR.Close() }()

	first := make(chan error, 1)
	go func() { _, err := c.call(context.Background(), "a", strings.Repeat("x", 1<<20)); first <- err }()
	time.Sleep(50 * time.Millisecond) // 讓第一個寫入先卡在 pipe
	queued := make(chan error, 1)
	go func() { _, err := c.call(context.Background(), "b", nil); queued <- err }()
	replied := make(chan struct{})
	go func() { c.replyPermission([]byte(`"p1"`), "y"); close(replied) }()
	time.Sleep(50 * time.Millisecond)

	c.close()
	for name, ch := range map[string]chan error{"卡住的寫入": first, "排隊的寫入": queued} {
		select {
		case err := <-ch:
			if !errors.Is(err, errWriteAbandoned) {
				t.Fatalf("%s應以 errWriteAbandoned 返回: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s在 close() 後仍卡住", name)
		}
	}
	select {
	case <-replied:
	case <-time.After(time.Second):
		t.Fatal("授權回覆在 close() 後仍卡住")
	}
	if !c.mu.TryLock() {
		t.Fatal("close 後 c.mu 不應被佔住")
	}
	c.mu.Unlock()
}

// 授權請求維持逐一處理（WS 一次只持有一個待授權），但不佔用 readLoop。
func TestClientPermissionRequestsStaySerialized(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	c := newClient(nil, stdoutR, stdinW)
	defer func() { c.close(); stdinR.Close(); stdinW.Close(); stdoutW.Close(); stdoutR.Close() }()

	entered := make(chan string, 2)
	release := make(chan struct{})
	c.onPermission = func(p permissionParams) string { entered <- p.ToolCall.Title; <-release; return "" }
	go io.Copy(io.Discard, stdinR)
	go func() {
		fmt.Fprintln(stdoutW, `{"jsonrpc":"2.0","id":"p1","method":"session/request_permission","params":{"toolCall":{"title":"first"}}}`)
		fmt.Fprintln(stdoutW, `{"jsonrpc":"2.0","id":"p2","method":"session/request_permission","params":{"toolCall":{"title":"second"}}}`)
	}()
	select {
	case got := <-entered:
		if got != "first" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first permission missing")
	}
	select {
	case second := <-entered:
		t.Fatalf("授權 callback 不可重疊: %s", second)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case got := <-entered:
		if got != "second" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("second permission 應在 first 結束後處理")
	}
}

// 真進程：子進程完全不讀 stdin，params 遠大於 OS pipe 容量。
// 驗證：call 受 ctx 控制、c.close() 不會卡住收尾、子進程結束後卡住的寫入 goroutine 會釋放。
func TestClientRealProcessBlockedWrite(t *testing.T) {
	t.Setenv("FAKE_ACP_MODE", "no-read")
	cmd := exec.Command(os.Args[0])
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := newClient(cmd, stdout, stdin)
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.call(ctx, "session/prompt", strings.Repeat("x", 4<<20))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("真進程寫入卡住時 call 應在逾時後返回: err=%v after %v", err, time.Since(start))
	}
	t.Logf("call 返回後仍在 pipe 上卡住的寫入 goroutine: %d", c.inflightWrites.Load())

	// 收尾：close() 不可被卡住的寫入拖住，且要能直接解除卡在 pipe 上的 Write（不必等子進程結束）。
	start = time.Now()
	c.close()
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("close() 被卡住的寫入拖住: %v", d)
	}
	if !c.mu.TryLock() {
		t.Fatal("c.mu 不應被卡住的寫入佔住")
	}
	c.mu.Unlock()
	waitZero(t, &c.inflightWrites) // 沒有殺進程，只靠 close()

	_ = cmd.Process.Kill()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("子進程沒有結束")
	}
}
func waitZero(t *testing.T, n *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for n.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if v := n.Load(); v != 0 {
		t.Fatalf("卡住的寫入 goroutine 應已釋放: inflight=%d", v)
	}
}

// close() 之後授權 worker 要回收；還卡在 callback 裡的那個，在 callback 被釋放（attempt ctx 取消）後也要退出。
func TestClientPermissionWorkerRecycledOnClose(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	c := newClient(nil, stdoutR, stdinW)
	defer func() { stdinR.Close(); stdinW.Close(); stdoutW.Close(); stdoutR.Close() }()
	go io.Copy(io.Discard, stdinR)

	entered := make(chan struct{})
	release := make(chan struct{}) // 模擬 attempt ctx 取消讓 callback 返回
	c.onPermission = func(permissionParams) string { close(entered); <-release; return "" }
	fmt.Fprintln(stdoutW, `{"jsonrpc":"2.0","id":"p1","method":"session/request_permission","params":{"toolCall":{"title":"t"}}}`)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback 未被呼叫")
	}
	c.close()
	close(release)
	select {
	case <-c.permDone:
	case <-time.After(time.Second):
		t.Fatal("close 且 callback 釋放後授權 worker 應回收")
	}
}

// 回應與 EOF 在 call 進入 select 前就都就緒時，要回傳 RPC error（含 error.data），不能被 select 隨機換成 stdout closed。
type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(b []byte) (int, error) { return f(b) }
func (writeFunc) Close() error                  { return nil }

func TestClientRPCErrorSurvivesImmediateEOF(t *testing.T) {
	for n := 0; n < 64; n++ {
		c := &client{pending: make(map[int64]chan rpcResponse), readDone: make(chan struct{}), closed: make(chan struct{})}
		expected := &rpcError{Code: -32603, Message: "Internal error", Data: json.RawMessage(`"MonthlyLimitReached"`)}
		c.stdin = writeFunc(func(b []byte) (int, error) { // 寫入當下回應與 EOF 已同時就緒
			c.pending[1] <- rpcResponse{Error: expected}
			close(c.readDone)
			return len(b), nil
		})
		_, err := c.call(context.Background(), "session/prompt", nil)
		if err != expected {
			t.Fatalf("第 %d 次：RPC error 被 EOF 蓋掉: %v", n, err)
		}
	}
}

// 授權佇列滿時：超量的授權請求直接取消，readLoop 不可被阻塞（否則 33+ 個授權時看不到 EOF／回應）。
func TestClientPermissionQueueFullDoesNotBlockReader(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	c := newClient(nil, stdoutR, stdinW)
	release := make(chan struct{})
	defer func() { close(release); c.close(); stdinR.Close(); stdinW.Close(); stdoutR.Close() }()

	var cancelled atomic.Int32
	go func() { // 收集我們寫回去的授權回覆
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			if strings.Contains(sc.Text(), `"outcome":"cancelled"`) {
				cancelled.Add(1)
			}
		}
	}()
	c.onPermission = func(permissionParams) string { <-release; return "" } // 第一個卡住，其餘排隊

	const total = permQueueSize + 8
	go func() {
		for i := 0; i < total; i++ {
			fmt.Fprintf(stdoutW, `{"jsonrpc":"2.0","id":"p%d","method":"session/request_permission","params":{"toolCall":{"title":"t%d"}}}`+"\n", i, i)
		}
		stdoutW.Close() // 全部送完後 EOF：reader 必須能走到這裡
	}()
	select {
	case <-c.readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("授權佇列滿時 readLoop 被阻塞，看不到 EOF")
	}
	// 1 個在 callback 內 + permQueueSize 個排隊 = 最多接受 permQueueSize+1 個，其餘都該被取消。
	deadline := time.Now().Add(time.Second)
	for cancelled.Load() < total-permQueueSize-1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := int(cancelled.Load()); n < total-permQueueSize-1 {
		t.Fatalf("超量授權應被直接取消: cancelled=%d want>=%d", n, total-permQueueSize-1)
	}
}
