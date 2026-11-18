package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// 走完整的 authMiddleware：未登入不得列出、建立、修改、刪除工作室，也不得讀時間軸。
func TestRoomsRequireAuth(t *testing.T) {
	base := startTestServer(t, "web:\n  password: correct-horse\n")
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/rooms", ""},
		{"POST", "/rooms", `{"name":"x","members":[]}`},
		{"GET", "/rooms/x", ""},
		{"PUT", "/rooms/x", `{"name":"y"}`},
		{"DELETE", "/rooms/x", ""},
		{"GET", "/rooms/x/timeline", ""},
		{"GET", "/rooms/x/timeline?before=abc&limit=5", ""},
		{"GET", "/rooms/x/board", ""},
		{"PUT", "/rooms/x/board", `{"text":"memo","previous":""}`},
		{"POST", "/rooms/x/messages", `{"text":"hi","request_id":"test","pm_id":"x"}`},
		{"POST", "/mcp/room", "{}"},
	} {
		req, err := http.NewRequest(tc.method, base+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Errorf("未登入 %s %s 得到 %d，預期 401", tc.method, tc.path, res.StatusCode)
		}
	}
}
