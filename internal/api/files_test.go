package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/fsbrowse"
)

type filesEnv struct {
	t    *testing.T
	app  *fiber.App
	root string
	sid  string
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, c := range map[string]string{
		"hello.txt":      "hello",
		"sub/a.txt":      "in-sub",
		"config.yaml":    "bot_token: SECRET-TOKEN",
		".env":           "API_KEY=SECRET-KEY",
		"../outside.txt": "OUTSIDE-SECRET",
		"image.bin":      "ab\x00cd",
		"big.txt":        strings.Repeat("a", fsbrowse.MaxFileBytes+1),
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	database := testDB(t)
	sess, err := database.CreateSession("files", "", root, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	h := NewFilesHandler(database)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/files", h.List)
	app.Get("/sessions/:id/files/content", h.Content)
	return &filesEnv{t: t, app: app, root: root, sid: sess.ID}
}

func (e *filesEnv) get(sid, endpoint, rawQuery string) (int, string) {
	e.t.Helper()
	target := "/sessions/" + sid + "/files" + endpoint
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	res, err := e.app.Test(httptest.NewRequest("GET", target, nil))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	body := string(b)
	// 任何回應（含錯誤）都不得洩漏 work_dir 絕對路徑
	for _, needle := range []string{e.root, filepath.ToSlash(e.root), strings.ReplaceAll(e.root, `\`, `\\`), filepath.Dir(e.root)} {
		if strings.Contains(body, needle) {
			e.t.Errorf("回應洩漏路徑 %q: %.200s", needle, body)
		}
	}
	return res.StatusCode, body
}

func q(path string) string { return "path=" + url.QueryEscape(path) }

func TestFilesList_OK(t *testing.T) {
	e := newFilesEnv(t)
	code, body := e.get(e.sid, "", "")
	if code != 200 {
		t.Fatalf("status=%d %s", code, body)
	}
	var got fsbrowse.Listing
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "" || got.Truncated || len(got.Entries) == 0 || got.Entries[0].Name != "sub" || !got.Entries[0].IsDir {
		t.Fatalf("got %+v", got)
	}
	protected := map[string]bool{}
	for _, en := range got.Entries {
		protected[en.Name] = en.Protected
	}
	if !protected["config.yaml"] || !protected[".env"] || protected["hello.txt"] {
		t.Fatalf("protected 標記不對: %v", protected)
	}
	if code, body := e.get(e.sid, "", q("sub")); code != 200 || !strings.Contains(body, `"a.txt"`) {
		t.Fatalf("sub: %d %s", code, body)
	}
}

func TestFilesContent_OK(t *testing.T) {
	e := newFilesEnv(t)
	code, body := e.get(e.sid, "/content", q("sub/a.txt"))
	if code != 200 {
		t.Fatalf("status=%d %s", code, body)
	}
	var got fsbrowse.Content
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Content != "in-sub" || got.Size != 6 || got.Path != "sub/a.txt" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestFilesContent_StatusCodes(t *testing.T) {
	e := newFilesEnv(t)
	tests := []struct {
		name string
		path string
		want int
	}{
		{"受保護 config.yaml", "config.yaml", 403},
		{"受保護 .env", ".env", 403},
		{"大小寫混用", "Config.YAML", 403},
		{"二進位", "image.bin", 415},
		{"過大", "big.txt", 413},
		{"目錄當檔案", "sub", 400},
		{"根目錄當檔案", "", 400},
		{"不存在", "nope.txt", 404},
		{"..", "..", 400},
		{"跳出讀外部檔", "../outside.txt", 400},
		{"絕對路徑", "/etc/passwd", 400},
		{"磁碟機代號", `C:\Windows\win.ini`, 400},
		{"UNC", `\\server\share\x`, 400},
		{"反斜線跳出", `..\outside.txt`, 400},
		{"備用資料流", "hello.txt:stream", 400},
	}
	for _, tc := range tests {
		code, body := e.get(e.sid, "/content", q(tc.path))
		if code != tc.want {
			t.Errorf("%s: path=%q status=%d want %d body=%.120s", tc.name, tc.path, code, tc.want, body)
		}
		for _, secret := range []string{"SECRET-TOKEN", "SECRET-KEY", "OUTSIDE-SECRET"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s: 回應含受保護或外部內容 %q", tc.name, secret)
			}
		}
	}
}

// 原始 query 層的編碼：Fiber 解一次碼後交給 fsbrowse；二次編碼的結果是字面檔名，不得再解。
func TestFilesContent_EncodedTraversal(t *testing.T) {
	e := newFilesEnv(t)
	cases := map[string]int{
		"path=..%2foutside.txt":            400, // 解一次碼 = ../outside.txt
		"path=%2e%2e%2foutside.txt":        400,
		"path=%2E%2E%5Coutside.txt":        400, // ..\outside.txt
		"path=%252e%252e%252foutside.txt":  404, // 二次編碼：字面檔名，不存在
		"path=%2e%2e":                      400,
		"path=sub%2f..%2f..%2foutside.txt": 400,
		"path=hello.txt%00.png":            400, // NUL
		"path=hello.txt%3Astream":          400, // 冒號
		"path=%2Fetc%2Fpasswd":             400,
		"path=C%3A%5CWindows":              400,
	}
	for raw, want := range cases {
		for _, endpoint := range []string{"/content", ""} {
			code, body := e.get(e.sid, endpoint, raw)
			if code != want {
				t.Errorf("%s%s: status=%d want %d body=%.100s", endpoint, raw, code, want, body)
			}
			if strings.Contains(body, "OUTSIDE-SECRET") {
				t.Errorf("%s: 洩漏外部內容", raw)
			}
		}
	}
}

func TestFiles_SessionProblems(t *testing.T) {
	e := newFilesEnv(t)
	if code, _ := e.get("no-such-session", "", ""); code != 404 {
		t.Errorf("session 不存在 status=%d", code)
	}
	database := testDB(t)
	sess, _ := database.CreateSession("nowd", "", "", "default", "claude", nil, "agent")
	h := NewFilesHandler(database)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/sessions/:id/files", h.List)
	res, err := app.Test(httptest.NewRequest("GET", "/sessions/"+sess.ID+"/files", nil))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("未設定 work_dir status=%d，預期 400", res.StatusCode)
	}
}

func TestFilesContent_TooLargeReportsSize(t *testing.T) {
	e := newFilesEnv(t)
	code, body := e.get(e.sid, "/content", q("big.txt"))
	var got struct {
		Error string `json:"error"`
		Size  int64  `json:"size"`
	}
	if code != 413 || json.Unmarshal([]byte(body), &got) != nil || got.Size != fsbrowse.MaxFileBytes+1 || got.Error == "" {
		t.Fatalf("status=%d body=%s", code, body)
	}
}

// 唯讀：非 GET 方法不存在於路由，不能用來寫入。
func TestFiles_NoWriteMethods(t *testing.T) {
	e := newFilesEnv(t)
	for _, m := range []string{"PUT", "POST", "DELETE", "PATCH"} {
		res, err := e.app.Test(httptest.NewRequest(m, "/sessions/"+e.sid+"/files/content?path=hello.txt", bytes.NewReader([]byte("x"))))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 405 && res.StatusCode != 404 {
			t.Errorf("%s status=%d", m, res.StatusCode)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "hello.txt")); string(b) != "hello" {
		t.Fatal("檔案被改動")
	}
}
