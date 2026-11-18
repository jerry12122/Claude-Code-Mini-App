package server

import "testing"

// 走完整的 authMiddleware：未登入不得取得整機資源資訊。
func TestSystemStatsRequiresAuth(t *testing.T) {
	base := startTestServer(t, "web:\n  password: correct-horse\n")
	if code := sessionsStatus(t, base+"/system/stats"); code != 401 {
		t.Fatalf("未登入 GET /system/stats 得到 %d，預期 401", code)
	}
}
