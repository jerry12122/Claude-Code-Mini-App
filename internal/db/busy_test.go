package db

import "testing"

// TestPendingWork 驗證桌面更新閘門用的全站工作摘要：每一種來源都要被計入，空閒時為零。
func TestPendingWork(t *testing.T) {
	database, err := Open(t.TempDir() + "/busy.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	s, err := database.CreateSession("busy", "", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}

	report, err := database.PendingWork()
	if err != nil {
		t.Fatal(err)
	}
	if report.Busy() {
		t.Fatalf("空閒時不應忙碌: %+v", report)
	}

	cases := []struct {
		name  string
		setup func() error
		check func(WorkReport) bool
	}{
		{"queued", func() error { _, err := database.EnqueueMessage(s.ID, "later", ""); return err }, func(r WorkReport) bool { return r.QueuedMessages == 1 }},
		{"pending_denials", func() error { return database.UpdatePendingDenials(s.ID, `[{"tool_name":"Write"}]`) }, func(r WorkReport) bool { return r.PendingDenials == 1 }},
		{"shell_pending", func() error { return database.UpdateShellPending(s.ID, `{"line":"ls"}`) }, func(r WorkReport) bool { return r.ShellPending == 1 }},
		{"status_running", func() error { return database.UpdateSessionStatus(s.ID, SessionStatusRunning) }, func(r WorkReport) bool { return r.ActiveStatus == 1 }},
	}
	for _, c := range cases {
		if err := c.setup(); err != nil {
			t.Fatalf("%s setup: %v", c.name, err)
		}
		r, err := database.PendingWork()
		if err != nil {
			t.Fatal(err)
		}
		if !r.Busy() || !c.check(r) {
			t.Fatalf("%s 應被計入忙碌: %+v", c.name, r)
		}
	}
	if _, err := database.Exec("UPDATE sessions SET agent_type = 'kiro' WHERE id = ?", s.ID); err != nil {
		t.Fatal(err)
	}
	if report, err := database.PendingWork(); err != nil || report.Busy() {
		t.Fatalf("唯讀 Kiro 不應阻擋更新: %+v, %v", report, err)
	}
	if details, err := database.PendingWorkDetails(); err != nil || len(details) != 0 {
		t.Fatalf("唯讀 Kiro 不應列為待處理工作: %+v, %v", details, err)
	}
}
