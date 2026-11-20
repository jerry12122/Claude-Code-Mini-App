package db

import (
	"database/sql"
	"fmt"
)

// 只保存這份派工及其綁定回覆，不複製會話裡的其他對話。
const roomJobResultSnapshot = `result_text=COALESCE((SELECT substr(m.content,1,16000) FROM messages m WHERE m.id=j.assistant_id AND m.session_id=j.worker_id),''),
 result_saved=EXISTS(SELECT 1 FROM messages m WHERE m.id=j.assistant_id AND m.session_id=j.worker_id)`

func snapshotRoomJobResults(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE room_jobs AS j SET ` + roomJobResultSnapshot + ` WHERE j.outcome!='' AND j.result_saved=0
 AND EXISTS(SELECT 1 FROM messages m WHERE m.id=j.assistant_id AND m.session_id=j.worker_id)`)
	return err
}

func migrateRoomJobRecords(tx *sql.Tx) error {
	for _, column := range []struct{ name, definition string }{
		{"title", `TEXT NOT NULL DEFAULT '舊任務（標題未保存）'`},
		{"task_text", `TEXT NOT NULL DEFAULT ''`},
		{"worker_name_snapshot", `TEXT NOT NULL DEFAULT ''`},
		{"result_text", `TEXT NOT NULL DEFAULT ''`},
		{"result_saved", `INTEGER NOT NULL DEFAULT 0`},
	} {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('room_jobs') WHERE name=?`, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE room_jobs ADD COLUMN %s %s", column.name, column.definition)); err != nil {
				return err
			}
		}
	}
	// 舊守門只監看原欄位；新增快照的寫入也必須使 board／批准版本失效。
	if _, err := tx.Exec(`CREATE TRIGGER IF NOT EXISTS room_job_records_revision AFTER UPDATE OF title,task_text,worker_name_snapshot,result_text,result_saved ON room_jobs
 WHEN NEW.title IS NOT OLD.title OR NEW.task_text IS NOT OLD.task_text OR NEW.worker_name_snapshot IS NOT OLD.worker_name_snapshot
 OR NEW.result_text IS NOT OLD.result_text OR NEW.result_saved IS NOT OLD.result_saved
 BEGIN
 INSERT INTO workflow_revisions(scope,revision) VALUES('room:'||NEW.room_id,1) ON CONFLICT(scope) DO UPDATE SET revision=revision+1;
 INSERT INTO workflow_revisions(scope,revision) VALUES('session:'||NEW.worker_id,1) ON CONFLICT(scope) DO UPDATE SET revision=revision+1;
 END`); err != nil {
		return err
	}
	// 原標題與當時名稱無法追溯，保留明確 fallback；只回填有結構化 room-job 作者的原任務。
	if _, err := tx.Exec(`UPDATE room_jobs AS j SET task_text=COALESCE(
 (SELECT content FROM messages WHERE session_id=j.worker_id AND role='user' AND author='room-job:'||j.id ORDER BY id LIMIT 1),
 (SELECT content FROM queued_messages WHERE session_id=j.worker_id AND author='room-job:'||j.id ORDER BY id LIMIT 1),'') WHERE task_text=''`); err != nil {
		return err
	}
	return snapshotRoomJobResults(tx)
}
