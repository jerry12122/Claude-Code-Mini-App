package db

import (
	"fmt"
	"strings"
)

// WorkReport 是「全站」（所有 session）仍在進行或待處理的工作摘要，供桌面更新閘門判斷是否忙碌。
// 只計 DB 可見的來源；記憶體中的執行緒、授權等待由 ws 套件自行檢查。
type WorkReport struct {
	QueuedMessages int // queued_messages 筆數（包含暫停中的佇列）
	PendingDenials int // pending_denials 非空的 session 數
	ShellPending   int // shell_pending 非空的 session 數
	ActiveStatus   int // status 為 running / awaiting_input / awaiting_shell_confirm 的 session 數
}

// Busy 任一來源非零即為忙碌。
func (r WorkReport) Busy() bool {
	return r.QueuedMessages > 0 || r.PendingDenials > 0 || r.ShellPending > 0 || r.ActiveStatus > 0
}

// PendingWork 讀取全站工作摘要。任何錯誤都回傳給呼叫端，不以「空閒」掩蓋。
func (db *DB) PendingWork() (WorkReport, error) {
	var r WorkReport
	// 舊 Kiro 已唯讀；保留的佇列／待確認資料不再是可執行工作，不應阻擋更新。
	err := db.QueryRow(
		`WITH writable_sessions AS (SELECT * FROM sessions WHERE agent_type != 'kiro')
		SELECT
			(SELECT COUNT(*) FROM queued_messages q JOIN writable_sessions s ON s.id = q.session_id),
			(SELECT COUNT(*) FROM writable_sessions WHERE TRIM(pending_denials) != ''),
			(SELECT COUNT(*) FROM writable_sessions WHERE TRIM(shell_pending) != ''),
			(SELECT COUNT(*) FROM writable_sessions WHERE status IN (?, ?, ?))`,
		SessionStatusRunning, SessionStatusAwaitingInput, SessionStatusAwaitingShellConfirm,
	).Scan(&r.QueuedMessages, &r.PendingDenials, &r.ShellPending, &r.ActiveStatus)
	return r, err
}

// PendingWorkDetails contains labels only, never prompts or command contents.
func (db *DB) PendingWorkDetails() ([]string, error) {
	rows, err := db.Query(`SELECT s.id, s.name, s.status, s.queue_paused,
		TRIM(s.pending_denials) != '', TRIM(s.shell_pending) != '', COALESCE(q.n, 0)
		FROM sessions s LEFT JOIN (SELECT session_id, COUNT(*) n FROM queued_messages GROUP BY session_id) q ON q.session_id=s.id
		WHERE s.agent_type != 'kiro' AND (q.n > 0 OR TRIM(s.pending_denials) != '' OR TRIM(s.shell_pending) != ''
		OR s.status IN ('running','awaiting_input','awaiting_shell_confirm')) ORDER BY s.id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, name, status string
		var paused, permission, shell bool
		var count int
		if err := rows.Scan(&id, &name, &status, &paused, &permission, &shell, &count); err != nil {
			return nil, err
		}
		var reasons []string
		if count > 0 {
			label := "佇列"
			if paused {
				label = "暫停佇列"
			}
			reasons = append(reasons, fmt.Sprintf("%s %d 筆；請至該會話處理或移除排隊訊息", label, count))
		}
		if permission || shell || status == SessionStatusAwaitingShellConfirm {
			reasons = append(reasons, "等待授權／確認")
		}
		if status == SessionStatusAwaitingInput {
			reasons = append(reasons, "等待回答")
		}
		if status == SessionStatusRunning {
			reasons = append(reasons, "狀態記為執行中；請至該會話確認")
		}
		details = append(details, fmt.Sprintf("%s [%s]：%s", name, id, strings.Join(reasons, "；")))
	}
	return details, rows.Err()
}
