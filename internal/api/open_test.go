package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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

func TestResolveWorkDir_Disabled(t *testing.T) {
	h := NewOpenHandler(nil, false)
	_, status, msg := h.resolveWorkDir("any")
	if status != 403 || msg == "" {
		t.Fatalf("status=%d msg=%q", status, msg)
	}
}

func TestResolveWorkDir_MissingSession(t *testing.T) {
	h := NewOpenHandler(testDB(t), true)
	_, status, _ := h.resolveWorkDir("missing")
	if status != 404 {
		t.Fatalf("status=%d", status)
	}
}

func TestResolveWorkDir_EmptyAndInvalid(t *testing.T) {
	database := testDB(t)
	h := NewOpenHandler(database, true)

	empty, err := database.CreateSession("empty", "", "  ", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, status, _ := h.resolveWorkDir(empty.ID); status != 400 {
		t.Fatalf("空白 work_dir status=%d", status)
	}

	missing, err := database.CreateSession("missing", "", filepath.Join(t.TempDir(), "no-such-dir"), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, status, _ := h.resolveWorkDir(missing.ID); status != 400 {
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
	if _, status, _ := h.resolveWorkDir(asFile.ID); status != 400 {
		t.Fatalf("檔案當目錄 status=%d", status)
	}

	dir := t.TempDir()
	okSess, err := database.CreateSession("ok", "", dir, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	got, status, msg := h.resolveWorkDir(okSess.ID)
	if status != 0 {
		t.Fatalf("有效目錄 status=%d msg=%q", status, msg)
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Fatalf("got=%q want=%q", got, abs)
	}
}

func TestOpenVSCode_DisabledReturnsJSON(t *testing.T) {
	app := fiber.New()
	h := NewOpenHandler(nil, false)
	app.Post("/sessions/:id/open-vscode", h.OpenVSCode)

	req := httptest.NewRequest("POST", "/sessions/x/open-vscode", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type=%q", ct)
	}
	if errMsg := readErr(t, resp.Body); errMsg == "" {
		t.Fatal("應回傳 error 欄位")
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
