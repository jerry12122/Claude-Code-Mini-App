package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

type operationApprovalKey struct{}

func WithOperationApproval(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, operationApprovalKey{}, id)
}
func OperationApprovalFromContext(ctx context.Context) string {
	id, _ := ctx.Value(operationApprovalKey{}).(string)
	return id
}

func idleCleanupTarget(tx *sql.Tx, id string) error {
	var safe bool
	if err := tx.QueryRow(`SELECT status='idle' AND pending_denials='' AND shell_pending=''
 AND NOT EXISTS(SELECT 1 FROM queued_messages WHERE session_id=s.id)
 AND NOT EXISTS(SELECT 1 FROM room_jobs WHERE worker_id=s.id AND outcome='')
 AND NOT EXISTS(SELECT 1 FROM shares WHERE session_id=s.id AND revoked=0 AND expires_at>strftime('%Y-%m-%dT%H:%M:%SZ','now'))
 FROM sessions s WHERE id=?`, id).Scan(&safe); err != nil {
		return err
	}
	if !safe {
		return errors.New("目標忙碌、有佇列／派工／待確認或有效分享，未清理")
	}
	return nil
}

func idleCleanupRoom(tx *sql.Tx, roomID, pmID string) error {
	var unsafe bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM room_jobs WHERE room_id=? AND outcome='') OR EXISTS(
 SELECT 1 FROM room_members rm JOIN sessions s ON s.id=rm.session_id WHERE rm.room_id=? AND
 ((s.id!=? AND s.status!='idle') OR s.pending_denials!='' OR s.shell_pending!='' OR EXISTS(SELECT 1 FROM queued_messages WHERE session_id=s.id)))`, roomID, roomID, pmID).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe {
		return errors.New("工作室仍有忙碌成員、待確認、佇列或派工，未刪除")
	}
	return nil
}

// 版本只綁定資料庫工作流，沒有讀取或封存工作目錄中的檔案。
func migrateWorkflowGuards(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS workflow_revisions(scope TEXT PRIMARY KEY, revision INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS operation_approvals(id TEXT PRIMARY KEY, action TEXT NOT NULL, room_id TEXT NOT NULL, pm_id TEXT NOT NULL, target TEXT NOT NULL, state TEXT NOT NULL, approved INTEGER NOT NULL DEFAULT 0, consumed INTEGER NOT NULL DEFAULT 0, expires_at TEXT NOT NULL);`); err != nil {
		return err
	}
	// ponytail: 以既有資料列的 scope 累計世代；高頻寫入若成瓶頸再縮小觸發欄位。
	scopes := map[string][]string{
		"rooms": {"'room:' || %s.id"}, "room_members": {"'room:' || %s.room_id", "'session:' || %s.session_id"},
		"room_memos": {"'room:' || %s.room_id"}, "room_jobs": {"'room:' || %s.room_id", "'session:' || %s.worker_id"},
		"sessions": {"'session:' || %s.id", "SELECT 'room:' || room_id FROM room_members WHERE session_id = %s.id"},
		"messages": {"'session:' || %s.session_id"}, "queued_messages": {"'session:' || %s.session_id", "SELECT 'room:' || id FROM rooms WHERE %s.author IN ('room-user:' || id, 'room-report:' || id) OR EXISTS(SELECT 1 FROM room_jobs WHERE room_id=rooms.id AND 'room-job:' || room_jobs.id = %s.author)"},
		"attachments": {"'session:' || %s.session_id"}, "attachment_access": {"'session:' || %s.session_id", "SELECT 'session:' || session_id FROM attachments WHERE id=%s.attachment_id"},
		"input_receipts": {"'session:' || %s.session_id"}, "queue_batches": {"'session:' || %s.session_id", "'room:' || %s.room_id"},
		"shares": {"'session:' || %s.session_id"},
	}
	for table, keys := range scopes {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			aliases := []string{"NEW"}
			if event == "DELETE" {
				aliases = []string{"OLD"}
			}
			if event == "UPDATE" {
				aliases = []string{"OLD", "NEW"}
			}
			// SQLite 的 INSERT SELECT UPSERT 需要 WHERE 消除 JOIN 語法歧義。
			sqlBody := ""
			for _, alias := range aliases {
				for _, key := range keys {
					key = strings.ReplaceAll(key, "%s", alias)
					if !strings.HasPrefix(key, "SELECT ") {
						key = "SELECT " + key
					}
					sqlBody += "INSERT INTO workflow_revisions(scope,revision) SELECT *,1 FROM (" + key + ") WHERE true ON CONFLICT(scope) DO UPDATE SET revision=revision+1;"
				}
			}
			when := ""
			if table == "room_jobs" && event == "UPDATE" {
				when = " WHEN NEW.room_id IS NOT OLD.room_id OR NEW.pm_id IS NOT OLD.pm_id OR NEW.worker_id IS NOT OLD.worker_id OR NEW.assistant_id IS NOT OLD.assistant_id OR NEW.outcome IS NOT OLD.outcome OR NEW.reported IS NOT OLD.reported OR NEW.attention IS NOT OLD.attention OR NEW.created_at IS NOT OLD.created_at"
			}
			if _, err := tx.Exec("CREATE TRIGGER IF NOT EXISTS workflow_" + table + "_" + event + " AFTER " + event + " ON " + table + when + " BEGIN " + sqlBody + " END"); err != nil {
				return err
			}
		}
	}
	return nil
}

func workflowVersion(tx *sql.Tx, roomID string) (string, error) {
	var rev int64
	if err := tx.QueryRow("SELECT COALESCE((SELECT revision FROM workflow_revisions WHERE scope = 'room:' || id),0) FROM rooms WHERE id=?", roomID).Scan(&rev); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", roomID, rev), nil
}

func checkRoomVersion(tx *sql.Tx, roomID, pmID, expected string) error {
	var pm string
	if err := tx.QueryRow("SELECT session_id FROM ("+roomPMQuery+") WHERE room_id=?", roomID).Scan(&pm); err != nil || pm != pmID {
		return ErrRoomPMChanged
	}
	current, err := workflowVersion(tx, roomID)
	if err != nil {
		return err
	}
	if expected == "" {
		return fmt.Errorf("缺少 expected_version；先 get_board，使用 version（目前 %s）", current)
	}
	if expected != current {
		return fmt.Errorf("expected_version 過期：收到 %s，目前 %s；請重新 get_board 比對後重試", expected, current)
	}
	return nil
}

var ErrOperationApproval = errors.New("缺少有效的使用者批准；請開啟 PM 會話的確認面板，明確批准這次操作後重試，MCP 不可代答")

// operationState 綁定 exact action/room/PM/target 與清理範圍的世代，狀態改回原值也不能復用。
func operationState(tx *sql.Tx, action, roomID, pmID, target string) (string, error) {
	if action != "room_delete" && action != "reset_session" && action != "delete_session" {
		return "", ErrOperationApproval
	}
	if err := checkOperationScope(tx, action, roomID, pmID, target); err != nil {
		return "", err
	}
	rows, err := tx.Query(`SELECT scope,revision FROM workflow_revisions WHERE scope='room:' || ? OR scope='session:' || ?
 OR scope IN (SELECT 'session:' || session_id FROM room_members WHERE room_id=?) ORDER BY scope`, roomID, target, roomID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	values := []any{action, roomID, pmID, target}
	for rows.Next() {
		var key string
		var rev int64
		if err := rows.Scan(&key, &rev); err != nil {
			return "", err
		}
		values = append(values, key, rev)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func checkOperationScope(tx *sql.Tx, action, roomID, pmID, target string) error {
	var pm string
	if err := tx.QueryRow("SELECT session_id FROM ("+roomPMQuery+") WHERE room_id=?", roomID).Scan(&pm); err != nil || pm != pmID {
		return ErrRoomPMChanged
	}
	if action == "room_delete" {
		if target != roomID {
			return ErrOperationApproval
		}
		return nil
	}
	var count, belongs int
	if err := tx.QueryRow("SELECT COUNT(*),COALESCE(SUM(room_id=?),0) FROM room_members WHERE session_id=?", roomID, target).Scan(&count, &belongs); err != nil {
		return err
	}
	if !(action == "delete_session" && count == 0) && (count != 1 || belongs != 1) {
		return errors.New("目標必須只屬於這個工作室；共用或其他工作室未清理")
	}
	return nil
}

func (db *DB) RequestOperationApproval(action, roomID, pmID, target string, expected ...string) (string, string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if len(expected) > 0 {
		if err := checkRoomVersion(tx, roomID, pmID, expected[0]); err != nil {
			return "", "", err
		}
	}
	state, err := operationState(tx, action, roomID, pmID, target)
	if err != nil {
		return "", "", err
	}
	id := uuid.NewString()
	_, err = tx.Exec(`INSERT INTO operation_approvals(id,action,room_id,pm_id,target,state,expires_at) VALUES(?,?,?,?,?,?,datetime('now','+5 minutes'))`, id, action, roomID, pmID, target, state)
	if err != nil {
		return "", "", err
	}
	return id, state, tx.Commit()
}

// 此方法只由收到 user-only WS 回答的 server 呼叫，不提供 MCP/API 核准入口。
func (db *DB) ApproveOperation(id string) error {
	res, err := db.Exec("UPDATE operation_approvals SET approved=1 WHERE id=? AND approved=0 AND consumed=0 AND expires_at>datetime('now')", id)
	return roomChange(res, err, ErrOperationApproval)
}

func consumeOperation(tx *sql.Tx, id, action, roomID, pmID, target string) error {
	state, err := operationState(tx, action, roomID, pmID, target)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE operation_approvals SET consumed=1 WHERE id=? AND action=? AND room_id=? AND pm_id=? AND target=? AND state=? AND approved=1 AND consumed=0 AND expires_at>datetime('now')`, id, action, roomID, pmID, target, state)
	return roomChange(res, err, ErrOperationApproval)
}
