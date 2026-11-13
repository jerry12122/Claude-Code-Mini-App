package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "open.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func readErr(t *testing.T, body io.Reader) string {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Error
}

func TestResolveWorkDir_MissingSession(t *testing.T) {
	_, status, _ := sessionWorkDir(testDB(t), "missing")
	if status != 404 {
		t.Fatalf("status=%d", status)
	}
}

func TestResolveWorkDir_EmptyAndInvalid(t *testing.T) {
	database := testDB(t)

	empty, err := database.CreateSession("empty", "", "  ", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, status, _ := sessionWorkDir(database, empty.ID); status != 400 {
		t.Fatalf("空白 work_dir status=%d", status)
	}

	missing, err := database.CreateSession("missing", "", filepath.Join(t.TempDir(), "no-such-dir"), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, status, _ := sessionWorkDir(database, missing.ID); status != 400 {
		t.Fatalf("不存在的目錄 status=%d", status)
	}

	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	asFile, err := database.CreateSession("file", "", filePath, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, status, _ := sessionWorkDir(database, asFile.ID); status != 400 {
		t.Fatalf("檔案當目錄 status=%d", status)
	}

	dir := t.TempDir()
	okSess, err := database.CreateSession("ok", "", dir, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	got, status, msg := sessionWorkDir(database, okSess.ID)
	if status != 0 {
		t.Fatalf("有效目錄 status=%d msg=%q", status, msg)
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Fatalf("got=%q want=%q", got, abs)
	}
}

func TestOpenHostAccess(t *testing.T) {
	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"127.0.0.1/32", "::1/128"},
	})
	h := NewOpenHandler(testDB(t))
	app.Post("/sessions/:id/open-vscode", h.OpenVSCode)
	app.Post("/sessions/:id/open-folder", h.OpenFolder)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"open-vscode", "open-folder"} {
		for _, tt := range []struct {
			host, forwardedHost, forwardedIP string
			want                             int
		}{
			{"127.0.0.1:8080", "", "", 404}, // 通過本機檢查後才會找不到 session；不啟動外部程式。
			{"localhost:8080", "", "", 404},
			{"[::1]:8080", "", "", 404},
			{"127.0.0.1", "", "", 404},
			{"10.0.1.200:8080", "", "", 403},
			{"attacker.example:8080", "", "", 403},
			{"10.0.1.200:8080", "127.0.0.1:8080", "127.0.0.1", 403},
			{"127.0.0.1:8080", "", "10.0.1.20", 403},
		} {
			req, err := http.NewRequest("POST", "http://"+ln.Addr().String()+"/sessions/missing/"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tt.host
			req.Header.Set("X-Forwarded-Host", tt.forwardedHost)
			req.Header.Set("X-Forwarded-For", tt.forwardedIP)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			errMsg := readErr(t, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.want || errMsg == "" {
				t.Fatalf("%s %+v: status=%d error=%q", path, tt, resp.StatusCode, errMsg)
			}
		}

		// app.Test 的實際來源是 0.0.0.0；偽造 loopback 標頭不能讓遠端請求通過。
		req := httptest.NewRequest("POST", "/sessions/missing/"+path, nil)
		req.Host = "127.0.0.1:8080"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s: forwarded header bypassed local access: %d", path, resp.StatusCode)
		}
	}
}

func TestFolderOpenCommand(t *testing.T) {
	cmd := folderOpenCommand("/tmp")
	switch runtime.GOOS {
	case "windows":
		if cmd.Args[0] != "explorer" {
			t.Fatalf("args=%v", cmd.Args)
		}
	case "darwin":
		if cmd.Args[0] != "open" {
			t.Fatalf("args=%v", cmd.Args)
		}
	default:
		if cmd.Args[0] != "xdg-open" {
			t.Fatalf("args=%v", cmd.Args)
		}
	}
}

func TestVscodeURI(t *testing.T) {
	if got := vscodeURI(`C:\Users\a b\proj`); got != "vscode://file/C:/Users/a b/proj" {
		t.Fatalf("got=%q", got)
	}
}

func TestVscodeNoAdminCommand(t *testing.T) {
	cmd := vscodeNoAdminCommand(`C:\Users\a b\proj`)
	if cmd.Args[0] != "explorer.exe" {
		t.Fatalf("args=%v", cmd.Args)
	}
	if cmd.Args[1] != "vscode://file/C:/Users/a b/proj" {
		t.Fatalf("args=%v", cmd.Args)
	}
}
