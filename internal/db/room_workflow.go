package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrRoomPM          = errors.New("請指定一個未擔任其他工作室 PM 的 agent 會話")
	ErrRoomPMChanged   = errors.New("PM 已變更，請重新開啟工作室設定")
	ErrRoomHandoffBusy = errors.New("目前的 PM 還有處理中的群聊訊息，完成後即可交接")
	ErrRoomMemoChanged = errors.New("備忘已更新，請比對最新內容再儲存")
	ErrRoomWorker      = errors.New("執行者尚未閒置、已有派工，或已離開工作室")
	ErrRoomFull        = errors.New("工作室成員已達上限")
)

const RoomUserAuthorPrefix = "room-user:"

// A PM must have a single room so the tools have one unambiguous scope.
const roomPMQuery = `SELECT pm.room_id, pm.session_id
 FROM room_members pm JOIN sessions s ON s.id = pm.session_id
 WHERE pm.role = 'pm' AND s.agent_type != 'telegram' AND s.input_mode = 'agent'
 AND (SELECT COUNT(*) FROM room_members WHERE room_id = pm.room_id AND role = 'pm') = 1
 AND (SELECT COUNT(*) FROM room_members WHERE session_id = pm.session_id AND role = 'pm') = 1`

func (db *DB) RoomPM(roomID string) (string, error) {
	return roomPM(db.QueryRow("SELECT session_id FROM ("+roomPMQuery+") WHERE room_id = ?", roomID))
}

func (db *DB) RoomForPM(pmID string) (string, error) {
	return roomPM(db.QueryRow("SELECT room_id FROM ("+roomPMQuery+") WHERE session_id = ?", pmID))
}

func roomPM(row *sql.Row) (string, error) {
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRoomPM
	}
	return id, err
}

type RoomJob struct {
	ID         int64  `json:"id"`
	WorkerID   string `json:"worker_id"`
	WorkerName string `json:"worker_name"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

type RoomBoard struct {
	Memo string    `json:"memo"`
	Jobs []RoomJob `json:"jobs"`
}

func (db *DB) RoomBoard(roomID string) (RoomBoard, error) {
	board := RoomBoard{Jobs: []RoomJob{}}
	err := db.QueryRow("SELECT COALESCE((SELECT text FROM room_memos WHERE room_id = r.id), '') FROM rooms r WHERE r.id = ?", roomID).Scan(&board.Memo)
	if errors.Is(err, sql.ErrNoRows) {
		return board, ErrRoomNotFound
	}
	if err != nil {
		return board, err
	}
	rows, err := db.Query(`SELECT j.id, j.worker_id, COALESCE(NULLIF(s.name, ''), '執行者'), j.created_at,
		CASE WHEN j.outcome != '' THEN j.outcome WHEN j.attention != '' THEN j.attention
  WHEN j.assistant_id > 0 THEN 'running' ELSE 'queued' END
 FROM room_jobs j LEFT JOIN sessions s ON s.id = j.worker_id
 WHERE j.room_id = ? ORDER BY (j.outcome = '') DESC, j.id DESC LIMIT 50`, roomID)
	if err != nil {
		return board, err
	}
	defer rows.Close()
	for rows.Next() {
		var job RoomJob
		if err := rows.Scan(&job.ID, &job.WorkerID, &job.WorkerName, &job.CreatedAt, &job.Status); err != nil {
			return board, err
		}
		board.Jobs = append(board.Jobs, job)
	}
	return board, rows.Err()
}

func (db *DB) SaveRoomMemo(roomID, pmID, text string) error {
	res, err := db.Exec(`INSERT INTO room_memos(room_id, text)
 SELECT room_id, ? FROM (`+roomPMQuery+`) WHERE room_id = ? AND session_id = ?
 ON CONFLICT(room_id) DO UPDATE SET text = excluded.text`, text, roomID, pmID)
	return roomChange(res, err, ErrRoomPM)
}

func (db *DB) EditRoomMemo(roomID, previous, text string) error {
	res, err := db.Exec(`INSERT INTO room_memos(room_id, text)
		SELECT id, ? FROM rooms WHERE id = ? AND COALESCE((SELECT text FROM room_memos WHERE room_id = rooms.id), '') = ?
		ON CONFLICT(room_id) DO UPDATE SET text = excluded.text`, text, roomID, previous)
	return roomChange(res, err, ErrRoomMemoChanged)
}

func (db *DB) RenameRoomSession(roomID, pmID, sessionID, name string) error {
	res, err := db.Exec(`UPDATE sessions SET name = ?, last_active = datetime('now')
		WHERE id = ? AND EXISTS(SELECT 1 FROM room_members WHERE room_id = ? AND session_id = sessions.id)
		AND EXISTS(SELECT 1 FROM (`+roomPMQuery+`) WHERE room_id = ? AND session_id = ?)`,
		name, sessionID, roomID, roomID, pmID)
	if err := roomChange(res, err, ErrRoomSessionMissing); err != nil {
		return err
	}
	if db.OnSessionChange != nil {
		db.OnSessionChange()
	}
	return nil
}

// CreateRoomWorker atomically adds a normal session inheriting its PM's working directory.
func (db *DB) CreateRoomWorker(roomID, pmID, name, agentType string) (string, error) {
	id := uuid.NewString()
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO sessions(id, name, agent_type, work_dir, last_read_at)
 SELECT ?, ?, ?, s.work_dir, datetime('now') FROM sessions s
 JOIN (`+roomPMQuery+`) pm ON pm.session_id = s.id WHERE s.id = ? AND pm.room_id = ?`, id, name, agentType, pmID, roomID)
	if err := roomChange(res, err, ErrRoomPM); err != nil {
		return "", err
	}
	res, err = tx.Exec(`INSERT INTO room_members(room_id, session_id, role, position)
 SELECT ?, ?, 'worker', COALESCE(MAX(position) + 1, 0) FROM room_members WHERE room_id = ? HAVING COUNT(*) < 50`, roomID, id, roomID)
	if err := roomChange(res, err, ErrRoomFull); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if db.OnSessionChange != nil {
		db.OnSessionChange()
	}
	return id, nil
}

// DispatchRoomJob records the assignment and its queue entry in the same transaction.
func (db *DB) DispatchRoomJob(roomID, pmID, workerID, text string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO room_jobs(room_id, pm_id, worker_id)
 SELECT wm.room_id, pm.session_id, s.id FROM room_members wm
 JOIN sessions s ON s.id = wm.session_id
 JOIN (`+roomPMQuery+`) pm ON pm.room_id = wm.room_id
 WHERE wm.room_id = ? AND pm.session_id = ? AND wm.role = 'worker' AND s.id = ?
 AND s.agent_type != 'telegram' AND s.input_mode = 'agent' AND s.status = 'idle'
 AND s.shell_pending = '' AND s.pending_denials = '' AND s.queue_paused = 0
	AND NOT EXISTS (SELECT 1 FROM room_members WHERE session_id = s.id AND role = 'pm')
 AND NOT EXISTS (SELECT 1 FROM queued_messages WHERE session_id = s.id)
 ON CONFLICT(worker_id) WHERE outcome = '' DO NOTHING`, roomID, pmID, workerID)
	if err := roomChange(res, err, ErrRoomWorker); err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("INSERT INTO queued_messages(session_id, content, author) VALUES (?, ?, ?)", workerID, text, fmt.Sprintf("room-job:%d", id)); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func roomChange(res sql.Result, err, missing error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		err = missing
	}
	return err
}

// BindRoomJob associates the exact turn with its assignment, including a permission retry.
func (db *DB) BindRoomJob(sessionID string, inputID, assistantID int64) error {
	_, err := db.Exec(`UPDATE room_jobs SET assistant_id = ?
 WHERE worker_id = ? AND outcome = '' AND (? = 0 OR assistant_id = 0)
 AND ('room-job:' || id) = (SELECT author FROM messages WHERE session_id = ? AND role = 'user'
  AND (? = 0 OR id = ?) ORDER BY id DESC LIMIT 1)`, assistantID, sessionID, inputID, sessionID, inputID, inputID)
	return err
}

func (db *DB) FinishRoomJob(sessionID string, assistantID int64, outcome string) error {
	_, err := db.Exec("UPDATE room_jobs SET outcome = ? WHERE worker_id = ? AND assistant_id = ? AND outcome = ''", outcome, sessionID, assistantID)
	return err
}

func (db *DB) RecoverRoomJobs() error {
	_, err := db.Exec("UPDATE room_jobs SET outcome = 'interrupted' WHERE assistant_id > 0 AND outcome = ''")
	return err
}

// DeliverRoomReports atomically queues each report once for the room's current PM.
func (db *DB) DeliverRoomReports() error {
	var pending bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM room_jobs WHERE outcome = '' OR reported = 0)").Scan(&pending); err != nil || !pending {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE room_jobs AS j SET attention = COALESCE((
		SELECT CASE WHEN s.status IN ('awaiting_confirm', 'awaiting_input') OR s.shell_pending != '' OR s.queue_paused = 1 THEN 'blocked'
			WHEN j.created_at <= datetime('now', '-3 minutes') THEN 'slow' ELSE '' END
		FROM sessions s WHERE s.id = j.worker_id), '') WHERE j.outcome = ''`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE room_jobs SET outcome = 'interrupted'
 WHERE outcome = '' AND (NOT EXISTS (SELECT 1 FROM sessions WHERE id = worker_id)
  OR (assistant_id = 0 AND created_at < datetime('now', '-10 seconds')
  AND NOT EXISTS (SELECT 1 FROM queued_messages WHERE session_id = worker_id AND author = 'room-job:' || room_jobs.id)))`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO queued_messages(session_id, content, author)
 SELECT pm.session_id, printf('派工 #%d｜%s｜%s%s', j.id, COALESCE(NULLIF(s.name, ''), '執行者'),
  CASE j.outcome WHEN 'done' THEN '已完成' WHEN 'failed' THEN '執行失敗' ELSE '已中斷' END,
  CASE WHEN trim(COALESCE(m.content, '')) = '' THEN '' ELSE char(10) || char(10) || substr(m.content, 1, 16000) END),
  'room-report:' || j.room_id
 FROM room_jobs j JOIN (` + roomPMQuery + `) pm ON pm.room_id = j.room_id
 LEFT JOIN sessions s ON s.id = j.worker_id
 LEFT JOIN messages m ON m.id = j.assistant_id AND m.session_id = j.worker_id
 WHERE j.outcome != '' AND j.reported = 0 ORDER BY j.id`); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE room_jobs SET reported = 1 WHERE outcome != '' AND reported = 0 AND room_id IN (SELECT room_id FROM (" + roomPMQuery + "))"); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) RoomSessionsToWake() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT s.id FROM sessions s JOIN queued_messages q ON q.session_id = s.id
 WHERE s.status = 'idle' AND s.queue_paused = 0 AND s.shell_pending = '' AND s.pending_denials = ''
 AND (q.author LIKE 'room-job:%' OR q.author LIKE 'room-user:%' OR q.author LIKE 'room-report:%')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func RoomMessageAuthor(author string) string {
	switch {
	case strings.HasPrefix(author, "room-job:"):
		return "PM 派工 #" + strings.TrimPrefix(author, "room-job:")
	case strings.HasPrefix(author, "room-report:"):
		return "執行者回報"
	case strings.HasPrefix(author, RoomUserAuthorPrefix):
		return "使用者 → PM"
	default:
		return author
	}
}
