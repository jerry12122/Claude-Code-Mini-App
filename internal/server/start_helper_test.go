package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// startTestServer 在暫存目錄以給定的 config.yaml 內容啟動，並把 server.port 改成空閒埠、db.path 放進同一目錄。
// viper 是全域的，所以每個測試前後都重置，避免吃到別的測試留下的設定。
func startTestServer(t *testing.T, configYAML string) string {
	t.Helper()
	// 路由測試不呼叫本機 CLI，避免背景模型同步的子進程占用暫存目錄。
	t.Setenv("PATH", "")
	t.Setenv("CC_AGY_BIN", "agy")
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	dbPath := filepath.ToSlash(filepath.Join(dir, "claude-miniapp.db"))
	body := fmt.Sprintf("%s\nserver:\n  port: %d\ndb:\n  path: %q\n", configYAML, port, dbPath)
	// 在 TempDir 刪目錄之前先把內容清掉。SQLite 在 Close 之後仍可能短暫鎖住檔案，Windows 不會重試這個錯誤。
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		var err error
		for {
			runtime.GC()
			err = os.RemoveAll(dir)
			if err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err != nil {
			t.Errorf("清理暫存目錄: %v", err)
		}
	})
	t.Chdir(dir)
	if err := os.WriteFile("config.yaml", []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	srv, err := Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		// 背景 syncModelOptions 會在這個工作目錄叫 CLI。沒等它結束，Windows 會因為檔案仍被占用而刪不掉暫存目錄。
		done := make(chan struct{})
		go func() {
			modelSyncMu.Lock()
			modelSyncMu.Unlock()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			t.Errorf("等待 model sync 結束逾時")
		}
	})
	return srv.URL()
}
