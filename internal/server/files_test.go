package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// 走完整的 authMiddleware：未登入不得列目錄或讀檔。
func TestFilesRequireAuth(t *testing.T) {
	base := startTestServer(t, "web:\n  password: correct-horse\n")
	for _, p := range []string{"/sessions/x/files", "/sessions/x/files?path=sub", "/sessions/x/files/content?path=hello.txt", "/sessions/x/files/content?path=config.yaml"} {
		if code := sessionsStatus(t, base+p); code != 401 {
			t.Errorf("未登入 GET %s 得到 %d，預期 401", p, code)
		}
	}
}

// 分享聊天室訪客（viewer／editor、live／snapshot）一律 403，連自己綁定的 session 也一樣；
// 且回應不得帶出 work_dir 內容或路徑。
func TestGuestCannotBrowseFiles(t *testing.T) {
	e := newGuestEnv(t)
	a := e.session("A")
	dir := a.WorkDir
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("HELLO-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := e.session("B")

	type tc struct {
		role, mode string
	}
	for _, c := range []tc{
		{db.ShareRoleViewer, db.ShareModeLive}, {db.ShareRoleEditor, db.ShareModeLive},
		{db.ShareRoleViewer, db.ShareModeSnapshot},
	} {
		tok, _ := e.guest(a.ID, c.role, c.mode, "小明")
		for _, sid := range []string{a.ID, other.ID} {
			for _, p := range []string{"/files", "/files?path=sub", "/files/content?path=hello.txt", "/files/content?path=..%2f"} {
				code, body := e.do("GET", "/sessions/"+sid+p, tok, nil, nil)
				if code != 403 {
					t.Errorf("%s/%s 訪客 GET %s 得到 %d，預期 403", c.role, c.mode, p, code)
				}
				if strings.Contains(string(body), "HELLO-CONTENT") || strings.Contains(string(body), dir) {
					t.Errorf("訪客 403 回應不該帶 work_dir 資訊: %.100s", body)
				}
			}
		}
	}
}
