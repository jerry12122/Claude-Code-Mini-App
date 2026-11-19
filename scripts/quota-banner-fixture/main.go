// 任務 5.1 的 loopback 臨時程序。只讀臨時 config 與臨時 DB，額度與 runner 都留在行程內。
//
//	go run ./scripts/quota-banner-fixture flow
//	go run ./scripts/quota-banner-fixture delay 18771
//	go run ./scripts/quota-banner-fixture restart
//	go run ./scripts/quota-banner-fixture off
//	go run ./scripts/quota-banner-fixture guard
//
// 模式：flow 送一則訊息後約 30 秒內走完用盡、橫幅、到期、自動重跑、成功。
// delay 已到期的 waiting 在下一個 30 秒輪詢仍滿，只改時間、不啟動，並把新時間推給已連線的客戶端。
// restart 啟動前分別是 waiting 與 resuming，重開後都會在輪詢繼續。
// off 先看到 waiting，到設定頁關閉自動繼續，橫幅消失且該輪詢不啟動。
// guard 快取 session 50%。送出「block 第一則」後，20 秒內把「第二則」排進佇列並把守門改成 40，下一則會暫停。
// Ctrl+C 停止。這是真的 30 秒 watchQuota，不是手動 resumeDue。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/logging"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/server"
)

var failNext atomic.Bool

type fixtureRunner struct{}

func (fixtureRunner) Name() string { return "claude" }

func (fixtureRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	if failNext.CompareAndSwap(true, false) {
		return errors.New("disk full")
	}
	if strings.HasPrefix(opts.Prompt, "block") {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cb(agent.Event{Type: agent.EventDelta, Text: "ok:" + opts.Prompt})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func main() {
	mode := "flow"
	if len(os.Args) > 1 && os.Args[1] != "" {
		mode = os.Args[1]
	}
	port := "18771"
	if len(os.Args) > 2 && os.Args[2] != "" {
		port = os.Args[2]
	}
	switch mode {
	case "flow", "delay", "restart", "off", "guard":
	default:
		fatal(errors.New("用法: go run ./scripts/quota-banner-fixture [flow|delay|restart|off|guard] [port]"))
	}
	src, err := filepath.Abs(filepath.Join("internal", "static"))
	if err != nil {
		fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		fatal(fmt.Errorf("找不到前端資源 %s（請在 repo 根目錄執行）", src))
	}
	dir, err := os.MkdirTemp("", "quota-banner-")
	if err != nil {
		fatal(err)
	}
	if err := linkStatic(filepath.Join(dir, "internal", "static"), src); err != nil {
		fatal(err)
	}
	dbPath := filepath.Join(dir, "app.db")
	cfg := fmt.Sprintf("no_auth: true\nserver:\n  port: %s\ndb:\n  path: %q\n", port, filepath.ToSlash(dbPath))
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		fatal(err)
	}

	agent.Register(agent.TypeClaude, func() agent.Runner { return fixtureRunner{} })
	database, err := db.Open(dbPath)
	if err != nil {
		fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		fatal(err)
	}
	on := mode != "guard"
	raw := `{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	if on {
		raw = `{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`
	}
	if _, err := database.PutGeneralSettings([]byte(raw)); err != nil {
		fatal(err)
	}
	var full atomic.Bool
	full.Store(mode == "flow" || mode == "delay")
	if mode == "flow" {
		failNext.Store(true)
	}
	ids, err := seed(database, work, mode)
	if err != nil {
		fatal(err)
	}
	if err := database.Close(); err != nil {
		fatal(err)
	}
	if mode == "flow" {
		go watchArmed(dbPath, &full)
	}

	if err := os.Chdir(dir); err != nil {
		fatal(err)
	}
	stopLogs := logging.Init()
	defer stopLogs()
	refresh := func(context.Context, string) (quota.Snapshot, error) {
		if mode == "guard" {
			return snap(50, ""), nil
		}
		if full.Load() {
			reset := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second).Format(time.RFC3339)
			if mode == "delay" {
				reset = time.Now().UTC().Add(30 * time.Second).Truncate(time.Second).Format(time.RFC3339)
			}
			return snap(100, reset), nil
		}
		return snap(40, ""), nil
	}
	seedSnap := quota.Snapshot{}
	if mode == "guard" {
		seedSnap = snap(50, "")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv, err := server.StartIsolated(ctx, refresh, agent.TypeClaude, seedSnap)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("mode %s\nurl %s\ntemp %s\n%s\nCtrl+C 停止\n", mode, srv.EntryURL(), dir, ids)
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

func seed(database *db.DB, work, mode string) (string, error) {
	past := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	switch mode {
	case "flow":
		live, err := database.CreateSession("送一則來觸發用盡", "", work, "default", agent.TypeClaude, nil, "agent")
		if err != nil {
			return "", err
		}
		return "session " + live.ID + " 名稱「送一則來觸發用盡」。第一則任意文字會用盡並出現橫幅，下一個 30 秒輪詢自動跑完。", nil
	case "delay":
		s, err := waitingSession(database, work, "仍滿會延後", past, "keep")
		if err != nil {
			return "", err
		}
		return "session " + s.ID + " 已是 waiting。下一個輪詢只改時間，佇列 keep 仍暫停。", nil
	case "restart":
		waiting, err := waitingSession(database, work, "waiting 重開", past, "waiting-restart")
		if err != nil {
			return "", err
		}
		resuming, err := waitingSession(database, work, "resuming 重開", past, "resuming-restart")
		if err != nil {
			return "", err
		}
		if ok, err := database.TransitionQuotaResume(resuming.ID, db.QuotaResumeWaiting, db.QuotaResumeResuming, past, false); err != nil || !ok {
			return "", fmt.Errorf("resuming ok=%v err=%v", ok, err)
		}
		return "waiting " + waiting.ID + "；resuming " + resuming.ID + "。啟動復原後兩者都會在輪詢繼續。", nil
	case "off":
		s, err := waitingSession(database, work, "關掉就停", past, "stay")
		if err != nil {
			return "", err
		}
		return "session " + s.ID + " 先顯示橫幅。設定頁關閉「額度用盡後自動繼續」後橫幅消失，佇列 stay 保持暫停。", nil
	default:
		s, err := database.CreateSession("守門門檻", "", work, "default", agent.TypeClaude, nil, "agent")
		if err != nil {
			return "", err
		}
		return "session " + s.ID + "。先送「block 第一則」，接著把「第二則」排進去，並把守門百分比改成 40。", nil
	}
}

func waitingSession(database *db.DB, work, name, at, content string) (*db.Session, error) {
	s, err := database.CreateSession(name, "", work, "default", agent.TypeClaude, nil, "agent")
	if err != nil {
		return nil, err
	}
	if _, err := database.EnqueueMessage(s.ID, content, ""); err != nil {
		return nil, err
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		return nil, err
	}
	ok, err := database.TransitionQuotaResume(s.ID, db.QuotaResumeNone, db.QuotaResumeWaiting, at, false)
	if err != nil || !ok {
		return nil, fmt.Errorf("waiting ok=%v err=%v", ok, err)
	}
	return s, nil
}

func watchArmed(dbPath string, full *atomic.Bool) {
	database, err := db.Open(dbPath)
	if err != nil {
		return
	}
	defer database.Close()
	for full.Load() {
		time.Sleep(time.Second)
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sessions WHERE quota_resume_state = ?`, db.QuotaResumeWaiting).Scan(&n); err == nil && n > 0 {
			full.Store(false)
		}
	}
}

func snap(percent float64, reset string) quota.Snapshot {
	return quota.Snapshot{Windows: []quota.Window{{Key: "session", Percent: &percent, ResetsAt: reset}}}
}

func linkStatic(dst, src string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", dst, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("建立前端 junction: %w %s", err, out)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
