package quota

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCodexQuotaRPC(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "rpc-error", "eof", "missing-result", "server-request", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 200 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestCodexQuotaProcess$")
			cmd.Env = append(os.Environ(), "QUOTA_TEST_CODEX_PROCESS="+mode)
			info, err := readCodexQuota(cmd)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if got := FormatDisplay("codex", info); got != "5h 2% · Week 13%" {
					t.Fatalf("got %q", got)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if mode == "rpc-error" && !strings.Contains(err.Error(), "sign in required") {
				t.Fatalf("lost RPC error: %v", err)
			}
			if cmd.ProcessState == nil {
				t.Fatal("process not reaped")
			}
		})
	}
}

// Re-exec this test binary as a protocol peer, without an installed CLI or login.
func TestCodexQuotaProcess(t *testing.T) {
	mode := os.Getenv("QUOTA_TEST_CODEX_PROCESS")
	if mode == "" {
		return
	}
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	read := func(want string) int {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := dec.Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != want {
			t.Fatalf("got %s; want %s", request.Method, want)
		}
		return request.ID
	}
	id := read("initialize")
	if mode == "timeout" {
		time.Sleep(time.Minute)
		os.Exit(1)
	}
	// Notifications must not be mistaken for responses.
	_ = enc.Encode(map[string]any{"method": "account/updated", "params": map[string]any{}})
	_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{}})
	read("initialized")
	id = read("account/rateLimits/read")
	switch mode {
	case "eof":
		os.Exit(0)
	case "rpc-error":
		_ = enc.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": "sign in required"}})
	case "missing-result":
		_ = enc.Encode(map[string]any{"id": id, "result": nil})
	case "server-request":
		_ = enc.Encode(map[string]any{"id": 17, "method": "account/chatgptAuthTokens/refresh"})
	default:
		_ = enc.Encode(map[string]any{"id": id, "result": json.RawMessage(sampleCodexRateLimitsJSON)})
	}
	// Keep the peer alive: the caller must close stdin and terminate/reap it.
	var extra any
	_ = dec.Decode(&extra)
	os.Exit(0)
}

func TestQuotaFetchFailurePreservesCache(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("CC_AGY_BIN", "nonexistent-quota-test-agy")
	for _, provider := range []string{"codex", "antigravity"} {
		s := NewService()
		previous := Snapshot{Provider: provider, DisplayText: "cached quota"}
		s.cache[provider] = &cacheEntry{snap: previous, fetchedAt: time.Now()}
		got, err := s.RefreshManual(context.Background(), provider)
		if err == nil || got.DisplayText != previous.DisplayText || got.Error == "" {
			t.Fatalf("%s: snapshot=%+v err=%v", provider, got, err)
		}
	}
}
