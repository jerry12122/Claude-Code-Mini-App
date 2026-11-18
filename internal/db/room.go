package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// 成員仍是各自獨立的 session，role 同時決定工作室的派工方向。
const (
	RoomRolePM     = "pm"
	RoomRoleWorker = "worker"

	RoomTimelineDefaultLimit = 50
	RoomTimelineMaxLimit     = 200
)

var (
	ErrRoomNotFound = errors.New("工作室不存在")
	// ErrRoomSessionMissing 的訊息尾端帶有缺少的 session id（用 errors.Is 判斷、直接顯示）。
	ErrRoomSessionMissing = errors.New("成員 session 不存在")
)

// RoomMember 是寫入用的成員；Position 由寫入順序決定，傳入值會被忽略。
type RoomMember struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Position  int    `json:"position"`
}

// RoomMemberInfo 是讀出用的成員，帶所屬 session 的名稱、agent 類型與狀態。
type RoomMemberInfo struct {
	RoomMember
	SessionName string `json:"session_name"`
	AgentType   string `json:"agent_type"`
	Status      string `json:"status"`
}

type Room struct {
	ID        string
	Name      string
	CreatedAt string
	Members   []RoomMemberInfo
}

type RoomSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	MemberCount int    `json:"member_count"`
}

// migrateRooms 建立工作室兩張表。role 用 CHECK 把關，即使上層漏驗證也寫不進非法角色。
// 沒有外鍵：整個專案都不啟用 foreign_keys，session 刪除時由 DeleteSession 明確清掉成員身分。
func (db *DB) migrateRooms() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS rooms (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS room_members (
			room_id    TEXT    NOT NULL,
			session_id TEXT    NOT NULL,
			role       TEXT    NOT NULL CHECK (role IN ('pm', 'worker')),
			position   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (room_id, session_id)
		);
		CREATE INDEX IF NOT EXISTS idx_room_members_session ON room_members (session_id);
		-- Preserve legacy rooms and keep the first configured PM when migrating.
		UPDATE room_members AS member SET role = 'worker'
		WHERE role = 'pm' AND rowid != (SELECT first.rowid FROM room_members first
			WHERE first.room_id = member.room_id AND first.role = 'pm' ORDER BY first.position, first.rowid LIMIT 1);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_room_one_pm ON room_members(room_id) WHERE role = 'pm';
		CREATE TABLE IF NOT EXISTS room_memos (
			room_id TEXT PRIMARY KEY, text TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS room_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			room_id TEXT NOT NULL, pm_id TEXT NOT NULL, worker_id TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			assistant_id INTEGER NOT NULL DEFAULT 0,
			outcome TEXT NOT NULL DEFAULT '', reported INTEGER NOT NULL DEFAULT 0,
			attention TEXT NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_room_jobs_active_worker ON room_jobs(worker_id) WHERE outcome = '';
		CREATE INDEX IF NOT EXISTS idx_room_jobs_room ON room_jobs(room_id, id);
		CREATE INDEX IF NOT EXISTS idx_room_jobs_reply ON room_jobs(assistant_id) WHERE assistant_id > 0;
	`)
	return err
}

// CreateRoom 在同一個交易內建立工作室與成員；任何一個 session 不存在就整筆回滾。
func (db *DB) CreateRoom(name string, members []RoomMember) (*Room, error) {
	id := uuid.New().String()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO rooms (id, name) VALUES (?, ?)`, id, name); err != nil {
		return nil, err
	}
	if err := replaceRoomMembers(tx, id, members); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetRoom(id)
}

// UpdateRoom 改名與／或整批取代成員（nil＝不動）。成員以傳入順序重排 position。
func (db *DB) UpdateRoom(id string, name *string, members *[]RoomMember, expectedPM ...string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM rooms WHERE id = ?`, id).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return err
	}
	if name != nil {
		if _, err := tx.Exec(`UPDATE rooms SET name = ? WHERE id = ?`, *name, id); err != nil {
			return err
		}
	}
	if members != nil {
		if len(expectedPM) > 0 {
			previous, err := roomMemberPM(tx, id)
			if err != nil {
				return err
			}
			if previous != expectedPM[0] {
				return ErrRoomPMChanged
			}
		}
		if err := replaceRoomMembers(tx, id, *members); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func replaceRoomMembers(tx *sql.Tx, roomID string, members []RoomMember) error {
	oldPM, err := roomMemberPM(tx, roomID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM room_members WHERE room_id = ?`, roomID); err != nil {
		return err
	}
	var newPM string
	for i, m := range members {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, m.SessionID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrRoomSessionMissing, m.SessionID)
			}
			return err
		}
		if m.Role == RoomRolePM {
			if newPM != "" {
				return ErrRoomPM
			}
			newPM = m.SessionID
			var eligible bool
			if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sessions s WHERE s.id = ? AND s.agent_type != 'telegram'
				AND s.input_mode = 'agent' AND NOT EXISTS(SELECT 1 FROM room_members WHERE session_id = s.id AND role = 'pm')
				AND NOT EXISTS(SELECT 1 FROM room_jobs WHERE worker_id = s.id AND outcome = ''))`, newPM).Scan(&eligible); err != nil {
				return err
			}
			if !eligible {
				return ErrRoomPM
			}
		}
		if _, err := tx.Exec(`INSERT INTO room_members (room_id, session_id, role, position) VALUES (?, ?, ?, ?)`,
			roomID, m.SessionID, m.Role, i); err != nil {
			return err
		}
	}
	if oldPM != "" && oldPM != newPM {
		if newPM == "" {
			return ErrRoomPM
		}
		var busy bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sessions WHERE id = ? AND status != 'idle')
			OR EXISTS(SELECT 1 FROM queued_messages WHERE session_id = ? AND author = ?)`,
			oldPM, oldPM, RoomUserAuthorPrefix+roomID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrRoomHandoffBusy
		}
		if _, err := tx.Exec(`UPDATE queued_messages SET session_id = ? WHERE session_id = ? AND author = ?`,
			newPM, oldPM, "room-report:"+roomID); err != nil {
			return err
		}
	}
	return nil
}

func roomMemberPM(tx *sql.Tx, roomID string) (string, error) {
	var id string
	err := tx.QueryRow("SELECT session_id FROM room_members WHERE room_id = ? AND role = 'pm'", roomID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// DeleteRoom removes the room and its unstarted inputs while preserving sessions and their history.
func (db *DB) DeleteRoom(id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM rooms WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRoomNotFound
	}
	if _, err := tx.Exec(`DELETE FROM room_members WHERE room_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM room_memos WHERE room_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM queued_messages WHERE author IN (?, ?)
		OR EXISTS (SELECT 1 FROM room_jobs j WHERE j.room_id = ? AND j.worker_id = queued_messages.session_id
			AND queued_messages.author = 'room-job:' || j.id)`, RoomUserAuthorPrefix+id, "room-report:"+id, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM room_jobs WHERE room_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ListRooms 新到舊。member_count 只算 session 仍存在的成員。
func (db *DB) ListRooms() ([]*RoomSummary, error) {
	rows, err := db.Query(`SELECT r.id, r.name, r.created_at,
		(SELECT COUNT(*) FROM room_members rm JOIN sessions s ON s.id = rm.session_id WHERE rm.room_id = r.id)
		FROM rooms r ORDER BY r.created_at DESC, r.rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RoomSummary
	for rows.Next() {
		var r RoomSummary
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt, &r.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// GetRoom 回工作室與成員清單。以 JOIN sessions 讀取，所以就算有殘留的孤兒成員列也不會出現。
func (db *DB) GetRoom(id string) (*Room, error) {
	var r Room
	err := db.QueryRow(`SELECT id, name, created_at FROM rooms WHERE id = ?`, id).Scan(&r.ID, &r.Name, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRoomNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT rm.session_id, rm.role, rm.position, s.name, s.agent_type, s.status
		FROM room_members rm JOIN sessions s ON s.id = rm.session_id
		WHERE rm.room_id = ? ORDER BY rm.position, rm.session_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r.Members = []RoomMemberInfo{}
	for rows.Next() {
		var m RoomMemberInfo
		if err := rows.Scan(&m.SessionID, &m.Role, &m.Position, &m.SessionName, &m.AgentType, &m.Status); err != nil {
			return nil, err
		}
		r.Members = append(r.Members, m)
	}
	return &r, rows.Err()
}
