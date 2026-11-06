package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/clicheck"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/config"
	"github.com/spf13/viper"
)

func TestSetupSaveInvalid(t *testing.T) {
	srv, port, dir := newSetupForSave(t, setupOptions{
		startNormal: func(context.Context) (*Server, error) {
			return nil, errors.New("驗證失敗不該啟動")
		},
	})
	db := filepath.Join(dir, "app.db")
	cases := []struct {
		name  string
		body  string
		field string
	}{
		{"壞 JSON", `{`, "body"},
		{"未知欄位", `{"password":"longpassword","port":1,"db_path":"` + jsonEscape(db) + `","extra":true}`, "body"},
		{"change-me", string(mustSetupJSON(t, config.SetupInput{Password: "change-me", Port: port, DBPath: db})), "password"},
		{"短密碼", string(mustSetupJSON(t, config.SetupInput{Password: "shortpw", Port: port, DBPath: db})), "password"},
		{"空白密碼", string(mustSetupJSON(t, config.SetupInput{Password: "        ", Port: port, DBPath: db})), "password"},
		{"埠 0", string(mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: 0, DBPath: db})), "port"},
		{"埠太大", string(mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: 70000, DBPath: db})), "port"},
		{"資料庫目錄不存在", string(mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: port, DBPath: filepath.Join(dir, "missing", "app.db")})), "db_path"},
		{"空 bot token", string(mustSetupJSON(t, config.SetupInput{
			Password: "longpassword", Port: port, DBPath: db,
			MiniApp: config.SetupMiniApp{Enabled: true, BotToken: "  "},
		})), "miniapp.bot_token"},
		{"白名單不是正整數", string(mustSetupJSON(t, config.SetupInput{
			Password: "longpassword", Port: port, DBPath: db,
			MiniApp: config.SetupMiniApp{Enabled: true, BotToken: "123:ABC", WhitelistTgIDs: []int64{0}},
		})), "miniapp.whitelist_tg_ids"},
	}
	host := hostFor(port)
	token := setupTokenFromURL(t, srv.EntryURL())
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			status, body := setupPost(t, srv.URL()+"/setup/save", host, token, []byte(tt.body))
			var got saveErrorBody
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("status %d body %s: %v", status, body, err)
			}
			if status != 400 || got.Code != "invalid_input" || got.Field != tt.field || got.Error == "" {
				t.Fatalf("status=%d body=%s", status, body)
			}
			if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
				t.Fatalf("不該寫出 config.yaml: %v", err)
			}
		})
	}
}

func TestSetupSaveWritesConfig(t *testing.T) {
	srv, _, dir := newSetupForSave(t, setupOptions{
		startNormal: func(context.Context) (*Server, error) {
			t.Error("不同埠不該切換")
			return nil, errors.New("不該切換")
		},
	})
	other := freePort(t)
	db := filepath.Join(dir, "app.db")
	body := mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: other, DBPath: db})
	status, raw := setupPost(t, srv.URL()+"/setup/save", hostFor(srvPort(t, srv)), setupTokenFromURL(t, srv.EntryURL()), body)
	var got saveOKBody
	if err := json.Unmarshal(raw, &got); err != nil || status != 200 || !got.OK || got.Applied != "restart_required" {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	viper.Reset()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.Password != "longpassword" || cfg.BotToken != "" || len(cfg.WhitelistTgIDs) != 0 || strings.Contains(string(text), "bot_token") {
		t.Fatalf("載入結果 token=%q ids=%v 檔案含 bot_token=%v", cfg.BotToken, cfg.WhitelistTgIDs, strings.Contains(string(text), "bot_token"))
	}
}

func TestSetupSaveAlreadyConfigured(t *testing.T) {
	srv, port, dir := newSetupForSave(t, setupOptions{})
	path := filepath.Join(dir, "config.yaml")
	const original = "already: here\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	body := mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: port, DBPath: filepath.Join(dir, "app.db")})
	status, raw := setupPost(t, srv.URL()+"/setup/save", hostFor(port), setupTokenFromURL(t, srv.EntryURL()), body)
	var got saveErrorBody
	if err := json.Unmarshal(raw, &got); err != nil || status != 409 || got.Code != "already_configured" {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	text, err := os.ReadFile(path)
	if err != nil || string(text) != original {
		t.Fatalf("檔案被改成 %q err=%v", text, err)
	}
}

func TestSetupSaveChangedPort(t *testing.T) {
	srv, port, dir := newSetupForSave(t, setupOptions{
		startNormal: func(context.Context) (*Server, error) {
			t.Error("換埠不該呼叫 startNormal")
			return nil, errors.New("不該切換")
		},
	})
	other := freePort(t)
	body := mustSetupJSON(t, config.SetupInput{Password: "longpassword", Port: other, DBPath: filepath.Join(dir, "app.db")})
	token := setupTokenFromURL(t, srv.EntryURL())
	host := hostFor(port)
	status, raw := setupPost(t, srv.URL()+"/setup/save", host, token, body)
	var got saveOKBody
	if err := json.Unmarshal(raw, &got); err != nil || status != 200 || !got.OK || got.Applied != "restart_required" || got.URL != "http://127.0.0.1:"+itoa(other)+"/" {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	status, raw = setupDo(t, http.MethodGet, srv.URL()+"/setup/status", host, "")
	if status != 200 || !contains(raw, `"needs_setup":true`) {
		t.Fatalf("仍應是設定模式: %d %s", status, raw)
	}
	status, raw = setupPost(t, srv.URL()+"/setup/save", host, token, body)
	var again saveErrorBody
	if err := json.Unmarshal(raw, &again); err != nil || status != 409 || again.Code != "already_configured" {
		t.Fatalf("第二次 status=%d body=%s", status, raw)
	}
}

func TestSetupSaveConcurrent(t *testing.T) {
	srv, _, dir := newSetupForSave(t, setupOptions{})
	other := freePort(t)
	db := filepath.Join(dir, "app.db")
	host := hostFor(srvPort(t, srv))
	token := setupTokenFromURL(t, srv.EntryURL())
	passwords := []string{"password-one", "password-two"}
	var wg sync.WaitGroup
	start := make(chan struct{})
	type result struct {
		status int
		body   []byte
		err    error
	}
	results := make([]result, 2)
	for i, pw := range passwords {
		wg.Add(1)
		go func(i int, pw string) {
			defer wg.Done()
			<-start
			body := mustSetupJSON(t, config.SetupInput{Password: pw, Port: other, DBPath: db})
			req, err := http.NewRequest(http.MethodPost, srv.URL()+"/setup/save", bytes.NewReader(body))
			if err != nil {
				results[i].err = err
				return
			}
			req.Host = host
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Setup-Token", token)
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				results[i].err = err
				return
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			results[i] = result{status: resp.StatusCode, body: raw}
		}(i, pw)
	}
	close(start)
	wg.Wait()
	var winner string
	okN, conflictN := 0, 0
	for i, r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		switch r.status {
		case 200:
			okN++
			winner = passwords[i]
		case 409:
			conflictN++
			if jsonCode(t, r.body) != "already_configured" {
				t.Fatalf("409 body=%s", r.body)
			}
		default:
			t.Fatalf("status=%d body=%s", r.status, r.body)
		}
	}
	if okN != 1 || conflictN != 1 || winner == "" {
		t.Fatalf("成功 %d 衝突 %d", okN, conflictN)
	}
	text, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.Password != winner || strings.Contains(string(text), passwords[0]) == strings.Contains(string(text), passwords[1]) {
		t.Fatalf("檔案密碼=%q winner=%q", cfg.Web.Password, winner)
	}
}

func TestSetupSaveSwitches(t *testing.T) {
	// 驗證模式切換即可，模型同步不應啟動本機已安裝的 CLI。
	t.Setenv("PATH", "")
	t.Setenv("CC_AGY_BIN", "agy")
	viper.Reset()
	t.Cleanup(viper.Reset)
	dir := t.TempDir()
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		var err error
		for {
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
	port := freePort(t)
	srv, err := startSetup(context.Background(), port, setupOptions{
		check: func(context.Context) []clicheck.Tool { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
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

	waitErr := make(chan error, 1)
	go func() { waitErr <- srv.Wait() }()
	select {
	case err := <-waitErr:
		t.Fatalf("切換前 Wait 已返回: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	const password = "longpassword"
	body := mustSetupJSON(t, config.SetupInput{Password: password, Port: port, DBPath: filepath.Join(dir, "app.db")})
	token := setupTokenFromURL(t, srv.EntryURL())
	status, raw := setupPost(t, srv.URL()+"/setup/save", hostFor(port), token, body)
	var got saveOKBody
	if err := json.Unmarshal(raw, &got); err != nil || status != 200 || !got.OK || got.Applied != "switched" || got.URL != "http://127.0.0.1:"+itoa(port)+"/" {
		t.Fatalf("status=%d body=%s err=%v", status, raw, err)
	}
	select {
	case err := <-waitErr:
		t.Fatalf("切換後 Wait 提早返回: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if strings.Contains(srv.EntryURL(), "?setup=") || srv.EntryURL() != srv.URL() {
		t.Fatal("切換後 EntryURL 仍帶憑證或與 URL 不同")
	}

	login := func(pw string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL()+"/auth/login", strings.NewReader(`{"password":"`+pw+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if login(password) != 200 {
		t.Fatal("新密碼應可登入")
	}
	if login("wrong-password") != 401 {
		t.Fatal("錯誤密碼應是 401")
	}
	resp, err := http.Get(srv.URL() + "/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !contains(raw, `"needs_setup":false`) {
		t.Fatalf("status=%d %s", resp.StatusCode, raw)
	}
	resp, err = http.Get(srv.URL() + "/setup/cli-check")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("cli-check = %d", resp.StatusCode)
	}
	status, _ = setupPost(t, srv.URL()+"/setup/save", hostFor(port), token, body)
	if status != 404 {
		t.Fatalf("舊憑證 save = %d", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waitErr:
	case <-time.After(20 * time.Second):
		t.Fatal("Shutdown 後 Wait 沒返回")
	}
}

func TestSetupSaveStartFailed(t *testing.T) {
	const password = "longpassword"
	var dbPath string
	srv, port, dir := newSetupForSave(t, setupOptions{
		startNormal: func(ctx context.Context) (*Server, error) {
			// 通過輸入驗證後，把檔案路徑變成資料夾，實際觸發資料庫啟動失敗。
			if err := os.Mkdir(dbPath, 0700); err != nil {
				return nil, err
			}
			return startNormal(ctx)
		},
	})
	dbPath = filepath.Join(dir, "app.db")
	body := mustSetupJSON(t, config.SetupInput{Password: password, Port: port, DBPath: dbPath})
	token := setupTokenFromURL(t, srv.EntryURL())
	host := hostFor(port)
	status, raw := setupPost(t, srv.URL()+"/setup/save", host, token, body)
	var got saveErrorBody
	if err := json.Unmarshal(raw, &got); err != nil || status != 500 || got.Code != "start_failed" || !strings.Contains(got.Error, "DB 初始化失敗") || strings.Contains(got.Error, password) {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	status, raw = setupDo(t, http.MethodGet, srv.URL()+"/setup/status", host, "")
	if status != 200 || !contains(raw, `"needs_setup":true`) {
		t.Fatalf("失敗後連不上設定頁: %d %s", status, raw)
	}
	status, raw = setupPost(t, srv.URL()+"/setup/save", host, token, body)
	if status != 409 || jsonCode(t, raw) != "already_configured" {
		t.Fatalf("再次儲存 status=%d body=%s", status, raw)
	}
}

func newSetupForSave(t *testing.T, opts setupOptions) (*Server, int, string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	dir := t.TempDir()
	t.Chdir(dir)
	port := freePort(t)
	if opts.check == nil {
		opts.check = func(context.Context) []clicheck.Tool { return nil }
	}
	srv, err := startSetup(context.Background(), port, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv, port, dir
}

type saveErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
	Field string `json:"field"`
}

type saveOKBody struct {
	OK      bool   `json:"ok"`
	Applied string `json:"applied"`
	URL     string `json:"url"`
}

func mustSetupJSON(t *testing.T, in config.SetupInput) []byte {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func setupPost(t *testing.T, rawURL, host, token string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Setup-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func hostFor(port int) string {
	return "127.0.0.1:" + itoa(port)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func srvPort(t *testing.T, srv *Server) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(srv.URL(), "http://127.0.0.1:%d", &port); err != nil || port == 0 {
		t.Fatalf("URL %s: %v", srv.URL(), err)
	}
	return port
}
