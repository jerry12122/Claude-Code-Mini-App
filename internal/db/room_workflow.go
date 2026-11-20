package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

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
	ID              int64  `json:"id"`
	WorkerID        string `json:"worker_id"`
	WorkerName      string `json:"worker_name"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	Title           string `json:"title"`
	TaskText        string `json:"task_text"`
	ResultText      string `json:"result_text"`
	ResultSaved     bool   `json:"result_saved"`
	ResultMessageID int64  `json:"result_message_id"`
	WorkerExists    bool   `json:"worker_exists"`
}

type RoomBoard struct {
	Version string    `json:"version"`
	Room    *Room     `json:"-"`
	Memo    string    `json:"memo"`
	Jobs    []RoomJob `json:"jobs"`
}

func (db *DB) RoomBoard(roomID string) (RoomBoard, error) {
	board := RoomBoard{Jobs: []RoomJob{}}
	tx, err := db.Begin()
	if err != nil {
		return board, err
	}
	defer tx.Rollback()
	board.Version, err = workflowVersion(tx, roomID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRoomNotFound
	}
	if err != nil {
		return board, err
	}
	board.Room, err = getRoom(tx, roomID)
	if err != nil {
		return board, err
	}
	err = tx.QueryRow("SELECT COALESCE((SELECT text FROM room_memos WHERE room_id = r.id), '') FROM rooms r WHERE r.id = ?", roomID).Scan(&board.Memo)
	if errors.Is(err, sql.ErrNoRows) {
		return board, ErrRoomNotFound
	}
	if err != nil {
		return board, err
	}
	rows, err := tx.Query(`SELECT j.id, j.worker_id, COALESCE(NULLIF(j.worker_name_snapshot, ''), '派工時名稱未保存'), j.created_at,
		CASE WHEN j.outcome != '' THEN j.outcome WHEN j.attention != '' THEN j.attention
  WHEN j.assistant_id > 0 THEN 'running' ELSE 'queued' END,
 j.title,j.task_text,j.result_text,j.result_saved,j.assistant_id,(s.id IS NOT NULL)
 FROM room_jobs j LEFT JOIN sessions s ON s.id = j.worker_id
 WHERE j.room_id = ? ORDER BY (j.outcome = '') DESC, j.id DESC LIMIT 50`, roomID)
	if err != nil {
		return board, err
	}
	defer rows.Close()
	for rows.Next() {
		var job RoomJob
		if err := rows.Scan(&job.ID, &job.WorkerID, &job.WorkerName, &job.CreatedAt, &job.Status, &job.Title, &job.TaskText, &job.ResultText, &job.ResultSaved, &job.ResultMessageID, &job.WorkerExists); err != nil {
			return board, err
		}
		board.Jobs = append(board.Jobs, job)
	}
	return board, rows.Err()
}

func (db *DB) SaveRoomMemo(roomID, pmID, text string, expected ...string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(expected) > 0 {
		if err := checkRoomVersion(tx, roomID, pmID, expected[0]); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`INSERT INTO room_memos(room_id, text)
 SELECT room_id, ? FROM (`+roomPMQuery+`) WHERE room_id = ? AND session_id = ?
 ON CONFLICT(room_id) DO UPDATE SET text = excluded.text`, text, roomID, pmID)
	if err := roomChange(res, err, ErrRoomPM); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) EditRoomMemo(roomID, previous, text string) error {
	res, err := db.Exec(`INSERT INTO room_memos(room_id, text)
		SELECT id, ? FROM rooms WHERE id = ? AND COALESCE((SELECT text FROM room_memos WHERE room_id = rooms.id), '') = ?
		ON CONFLICT(room_id) DO UPDATE SET text = excluded.text`, text, roomID, previous)
	return roomChange(res, err, ErrRoomMemoChanged)
}

func (db *DB) RenameRoomSession(roomID, pmID, sessionID, name string, expected ...string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(expected) > 0 {
		if err := checkRoomVersion(tx, roomID, pmID, expected[0]); err != nil {
			return err
		}
	}
	res, err := tx.Exec(`UPDATE sessions SET name = ?, last_active = datetime('now')
		WHERE id = ? AND EXISTS(SELECT 1 FROM room_members WHERE room_id = ? AND session_id = sessions.id)
		AND EXISTS(SELECT 1 FROM (`+roomPMQuery+`) WHERE room_id = ? AND session_id = ?)`,
		name, sessionID, roomID, roomID, pmID)
	if err := roomChange(res, err, ErrRoomSessionMissing); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if db.OnSessionChange != nil {
		db.OnSessionChange()
	}
	return nil
}

// CreateRoomWorker atomically adds a normal session inheriting its PM's working directory.
func (db *DB) CreateRoomWorker(roomID, pmID, name, agentType string, expected ...string) (string, error) {
	id := uuid.NewString()
	global, err := db.GetGlobalInstruction()
	if err != nil {
		return "", err
	}
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if len(expected) > 0 {
		if err := checkRoomVersion(tx, roomID, pmID, expected[0]); err != nil {
			return "", err
		}
	}
	res, err := tx.Exec(`INSERT INTO sessions(id, name, agent_type, work_dir, global_instruction_text, global_instruction_mode, last_read_at)
 SELECT ?, ?, ?, s.work_dir, ?, ?, datetime('now') FROM sessions s
 JOIN (`+roomPMQuery+`) pm ON pm.session_id = s.id WHERE s.id = ? AND pm.room_id = ?`, id, name, agentType, global.Text, global.Mode, pmID, roomID)
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

// ponytail: 舊 Go 呼叫保留明示的缺標題 fallback；MCP 派工一律走有明確標題的入口。
func (db *DB) DispatchRoomJob(roomID, pmID, workerID, text string, expected ...string) (int64, error) {
	return db.DispatchRoomJobWithTitle(roomID, pmID, workerID, "未提供工作標題（舊呼叫）", text, expected...)
}

// DispatchRoomJobWithTitle 在同一交易保存當次工作快照與排隊項目。
func (db *DB) DispatchRoomJobWithTitle(roomID, pmID, workerID, title, text string, expected ...string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 120 || strings.ContainsAny(title, "\r\n") {
		return 0, errors.New("工作標題須為 1 到 120 字的單行文字")
	}
	if strings.TrimSpace(text) == "" || len(text) > 64000 {
		return 0, errors.New("交辦內容須為 1 到 64000 bytes")
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if len(expected) > 0 {
		if err := checkRoomVersion(tx, roomID, pmID, expected[0]); err != nil {
			return 0, err
		}
	}
	res, err := tx.Exec(`INSERT INTO room_jobs(room_id, pm_id, worker_id,title,task_text,worker_name_snapshot)
 SELECT wm.room_id, pm.session_id, s.id,?,?,COALESCE(NULLIF(s.name,''),'未命名') FROM room_members wm
 JOIN sessions s ON s.id = wm.session_id
 JOIN (`+roomPMQuery+`) pm ON pm.room_id = wm.room_id
 WHERE wm.room_id = ? AND pm.session_id = ? AND wm.role = 'worker' AND s.id = ?
 AND s.agent_type != 'telegram' AND s.input_mode = 'agent' AND s.status = 'idle'
 AND s.shell_pending = '' AND s.pending_denials = ''
	AND NOT EXISTS (SELECT 1 FROM room_members WHERE session_id = s.id AND role = 'pm')
 AND NOT EXISTS (SELECT 1 FROM queued_messages WHERE session_id = s.id) -- 暫停只擋已排隊的訊息；空佇列的暫停旗標不該擋派工
 ON CONFLICT(worker_id) WHERE outcome = '' DO NOTHING`, title, text, roomID, pmID, workerID)
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
	_, err := db.Exec(`UPDATE room_jobs AS j SET outcome = ?,`+roomJobResultSnapshot+` WHERE worker_id = ? AND assistant_id = ? AND outcome = ''`, outcome, sessionID, assistantID)
	return err
}

// 完工只有 FinishRoomJob → DeliverRoomReports 一個入口；手動通知不產生另一筆 runnable。
// 不解析本文；不同 job 的回報仍各自保留，新版交付應建立新 job。
func (db *DB) RoomCompletionNotification(jobID int64, workerID, pmID string) error {
	return db.CheckRoomAgentMessage(pmID, workerID, AgentMessage{Intent: "completion", JobID: jobID})
}

// AgentMessage 的意圖是協定宣告，不是來源的 OS 身分證明；不解析自然語言。
type AgentMessage struct {
	Intent string
	JobID  int64
	Ask    bool
}

var ErrRoomAgentIntent = errors.New("送往現任 PM 的 send_message 必須帶 from_session_id、intent=question 與正式 worker 的 job_id；完工請用 intent=completion＋job_id（或 completion_job_id），只走自動回報；明確同步詢問可用 ask_session")
var ErrRoomAgentJob = errors.New("job_id／來源不屬於這個現任 PM 的正式 worker；請核對 job_id、from_session_id 與目前 PM，未入列")

func (db *DB) CheckRoomAgentMessage(target, source string, message AgentMessage) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkRoomAgentMessage(tx, target, source, message)
}

func checkRoomAgentMessage(reader roomReader, target, source string, message AgentMessage) error {
	if message.Intent != "" && message.Intent != "question" && message.Intent != "completion" {
		return ErrRoomAgentIntent
	}
	if source != "" {
		var exists bool
		if err := reader.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)", source).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("from_session_id 來源已不存在，未入列")
		}
	}
	// ask_session 本身就是明確詢問，保留其不依賴正式派工的既有語意。
	if message.Ask && message.Intent == "question" {
		return nil
	}
	var roomID string
	err := reader.QueryRow("SELECT room_id FROM ("+roomPMQuery+") WHERE session_id=?", target).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		if message.Intent == "completion" || message.JobID != 0 {
			return ErrRoomAgentJob
		}
		return nil
	}
	if err != nil {
		return err
	}
	if source == "" || message.Intent == "" {
		return ErrRoomAgentIntent
	}
	var worker bool
	if err := reader.QueryRow("SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=? AND session_id=? AND role='worker')", roomID, source).Scan(&worker); err != nil {
		return err
	}
	if !worker {
		if message.Intent == "completion" || message.JobID != 0 {
			return ErrRoomAgentJob
		}
		return nil
	}
	if message.JobID <= 0 {
		return ErrRoomAgentIntent
	}
	var valid bool
	if err := reader.QueryRow("SELECT EXISTS(SELECT 1 FROM room_jobs WHERE id=? AND room_id=? AND worker_id=?)", message.JobID, roomID, source).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrRoomAgentJob
	}
	return nil
}

func (db *DB) RecoverRoomJobs() error {
	_, err := db.Exec(`UPDATE room_jobs AS j SET outcome = 'interrupted',` + roomJobResultSnapshot + ` WHERE assistant_id > 0 AND outcome = ''`)
	return err
}

// DeliverRoomReports 修復失去佇列或回覆的派工，並把每份回報原子送入目前 PM 的佇列。
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
	// 排隊很久的派工一樣需要留時間讓出列的訊息綁上回覆。
	if _, err := tx.Exec(`UPDATE room_jobs AS j SET outcome = 'interrupted'
 WHERE j.outcome = '' AND (NOT EXISTS (SELECT 1 FROM sessions WHERE id = j.worker_id)
  OR (j.assistant_id > 0 AND NOT EXISTS (
   SELECT 1 FROM messages WHERE id = j.assistant_id AND session_id = j.worker_id))
  OR (j.assistant_id = 0 AND j.created_at < datetime('now', '-10 seconds')
   AND NOT EXISTS (SELECT 1 FROM queued_messages WHERE session_id = j.worker_id AND author = 'room-job:' || j.id)
   AND NOT EXISTS (SELECT 1 FROM messages WHERE session_id = j.worker_id AND role = 'user'
    AND author = 'room-job:' || j.id AND created_at >= datetime('now', '-10 seconds'))))`); err != nil {
		return err
	}
	// source_session_id 記下執行者，讓成員篩選能結構化歸屬回報；作者與內容不變。
	if err := snapshotRoomJobResults(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO queued_messages(session_id, content, author, source_session_id)
 SELECT pm.session_id, printf('派工 #%d｜%s｜%s｜執行者：%s%s', j.id,j.title,
  CASE j.outcome WHEN 'done' THEN '已完成' WHEN 'failed' THEN '執行失敗' ELSE '已中斷' END,
  COALESCE(NULLIF(j.worker_name_snapshot,''),'派工時名稱未保存'),
  CASE WHEN trim(j.result_text) = '' THEN '' ELSE char(10) || char(10) || j.result_text END),
  'room-report:' || j.room_id, j.worker_id
 FROM room_jobs j JOIN (` + roomPMQuery + `) pm ON pm.room_id = j.room_id
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

// RoomAttention 只依 session 狀態與未解授權欄位判斷，不讀錯誤文字。
func RoomAttention(status, shellPending, pendingDenials string) string {
	switch status {
	case SessionStatusAwaitingConfirm:
		return "confirm"
	case SessionStatusAwaitingInput:
		return "input"
	case SessionStatusAwaitingShellConfirm:
		return "shell"
	}
	if strings.TrimSpace(pendingDenials) != "" {
		return "confirm"
	}
	if strings.TrimSpace(shellPending) != "" {
		return "shell"
	}
	return ""
}

func RoomMessageAuthor(author string) string {
	switch {
	case strings.HasPrefix(author, "room-job:"):
		return "PM 派工 #" + strings.TrimPrefix(author, "room-job:")
	case strings.HasPrefix(author, "room-report:"):
		return "執行者回報"
	case strings.HasPrefix(author, RoomUserAuthorPrefix):
		return "使用者 → PM"
	case strings.HasPrefix(author, "room-handoff:"):
		return "交接"
	default:
		return author
	}
}
