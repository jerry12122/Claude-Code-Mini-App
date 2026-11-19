package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	QuotaResumeNone      = "none"
	QuotaResumeWaiting   = "waiting"
	QuotaResumeResuming  = "resuming"
	QuotaResumeExhausted = "exhausted"
)

// DueQuotaResume 是已到預定時間、狀態仍為 waiting 的 session。
// Generation 與 At 是列出當下的身份，輪詢必須拿這組去比對，不能刷新後重讀。
type DueQuotaResume struct {
	SessionID  string
	At         string
	Generation int64
}

type quotaResumeEdge struct{ from, to string }

// 允許的來源→目標。表外轉移在進資料庫前直接拒絕。
var quotaResumeAllowed = map[quotaResumeEdge]struct{}{
	{QuotaResumeNone, QuotaResumeWaiting}:       {},
	{QuotaResumeNone, QuotaResumeExhausted}:     {},
	{QuotaResumeWaiting, QuotaResumeWaiting}:    {},
	{QuotaResumeWaiting, QuotaResumeResuming}:   {},
	{QuotaResumeResuming, QuotaResumeWaiting}:   {},
	{QuotaResumeResuming, QuotaResumeExhausted}: {},
	{QuotaResumeResuming, QuotaResumeNone}:      {},
	{QuotaResumeWaiting, QuotaResumeNone}:       {},
	{QuotaResumeExhausted, QuotaResumeNone}:     {},
}

// quotaResumeTime 轉成 UTC RFC3339、秒精度、不帶小數。字串可直接比較先後。
func quotaResumeTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// QuotaResumeAt 是額度重置後實際再試的時間：重置時間加 60 秒緩衝（吸收時鐘差）。
func QuotaResumeAt(reset time.Time) string {
	return quotaResumeTime(reset.Add(time.Minute))
}

// quotaSQL 讓同一段 SQL 能在 *DB 與交易（*sql.Tx）上執行。
type quotaSQL interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// TransitionQuotaResume 以單一條件式 UPDATE 轉移狀態。
// from 必須等於目前 quota_resume_state 才會成功。表外轉移回傳 false, nil，不寫入。
// attempts 只在 waiting→resuming 加 1；轉入 none 時歸零且 quota_resume_at 清空。
// 同一道 waiting→resuming 也把 quota_resume_generation 加 1。取消與關閉開關同樣加 1。這個數字不歸零、不重用。
// resuming→waiting 且 restoreAttempts 為真時減 1（不低於 0），為假時保留。
// at 非空且目標不是 none 時寫入 quota_resume_at。
// generation 大於 0 時，WHERE 同時要求 quota_resume_generation 等於它。
func (db *DB) TransitionQuotaResume(sessionID, from, to, at string, restoreAttempts bool) (bool, error) {
	return execQuotaResumeTransition(context.Background(), db, sessionID, from, to, at, restoreAttempts, 0, false)
}

func execQuotaResumeTransition(ctx context.Context, ex quotaSQL, sessionID, from, to, at string, restoreAttempts bool, generation int64, matchGeneration bool) (bool, error) {
	if _, ok := quotaResumeAllowed[quotaResumeEdge{from, to}]; !ok {
		return false, nil
	}
	inc, zero, restore := 0, 0, 0
	if from == QuotaResumeWaiting && to == QuotaResumeResuming {
		inc = 1
	}
	if to == QuotaResumeNone {
		zero = 1
	}
	if from == QuotaResumeResuming && to == QuotaResumeWaiting && restoreAttempts {
		restore = 1
	}
	query := `
		UPDATE sessions SET
			quota_resume_state = ?,
			quota_resume_attempts = CASE
				WHEN ? = 1 THEN quota_resume_attempts + 1
				WHEN ? = 1 THEN 0
				WHEN ? = 1 THEN MAX(quota_resume_attempts - 1, 0)
				ELSE quota_resume_attempts
			END,
			quota_resume_at = CASE
				WHEN ? = 1 THEN ''
				WHEN ? != '' THEN ?
				ELSE quota_resume_at
			END,
			quota_resume_generation = CASE
				WHEN ? = 1 THEN quota_resume_generation + 1
				ELSE quota_resume_generation
			END
		WHERE id = ? AND quota_resume_state = ?`
	args := []any{to, inc, zero, restore, zero, at, at, inc, sessionID, from}
	if matchGeneration {
		query += ` AND quota_resume_generation = ?`
		args = append(args, generation)
	}
	res, err := ex.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func quotaArmEdge(from, to string) bool {
	switch [2]string{from, to} {
	case [2]string{QuotaResumeNone, QuotaResumeWaiting},
		[2]string{QuotaResumeNone, QuotaResumeExhausted},
		[2]string{QuotaResumeResuming, QuotaResumeWaiting},
		[2]string{QuotaResumeResuming, QuotaResumeExhausted}:
		return true
	default:
		return false
	}
}

// ArmQuotaResume 在同一筆交易裡確認開關仍開、使用者訊息、武裝轉移、把該訊息插到佇列最前並暫停。
// 只接受 none／resuming → waiting／exhausted，且轉移的 WHERE 要求世代等於 generation（世代 0 也要比對）。
// 取消或接受新訊息會把世代加 1，晚到的失敗收尾就對不上，不會再用舊訊息重新武裝。
// 轉移沒搶到、世代已換或開關已關時回 false, nil，不入列。
// 訊息不存在、附件不合法或寫入失敗會回滾，不會留下可自動啟動但沒有原訊息的狀態。
// 這條路徑不增加也不歸零 attempts。
func (db *DB) ArmQuotaResume(sessionID string, userMsgID int64, from, to, at string, generation int64) (bool, error) {
	if !quotaArmEdge(from, to) {
		return false, nil
	}
	var content, author, rawIDs string
	err := db.QueryRow(
		`SELECT content, author, attachment_ids FROM messages WHERE id = ? AND session_id = ? AND role = 'user'`,
		userMsgID, sessionID,
	).Scan(&content, &author, &rawIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("找不到本次使用者訊息")
	}
	if err != nil {
		return false, err
	}
	ids, err := decodeAttachmentIDs(rawIDs)
	if err != nil {
		return false, err
	}
	if _, err := db.ResolveAttachments(sessionID, ids); err != nil {
		return false, err
	}
	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var rawSettings string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, SettingKeyGeneral).Scan(&rawSettings)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var settings GeneralSettings
	if json.Unmarshal([]byte(rawSettings), &settings) != nil || !settings.AutoResumeOnQuota {
		return false, nil
	}
	ok, err := execQuotaResumeTransition(ctx, tx, sessionID, from, to, at, false, generation, true)
	if err != nil || !ok {
		return false, err
	}
	var minID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(id) FROM queued_messages`).Scan(&minID); err != nil {
		return false, err
	}
	base := int64(0)
	if minID.Valid && minID.Int64 < 0 {
		base = minID.Int64
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO queued_messages (id, session_id, content, attachment_ids, author) VALUES (?, ?, ?, ?, ?)`,
		base-1, sessionID, content, attachmentIDsJSON(ids), author); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET queue_paused = 1 WHERE id = ?`, sessionID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SnapshotQuotaResume 一次讀出這次是不是自動重跑，以及當時的世代。
// 不是 resuming 時 held 為 0，observed 仍是資料列上的世代，供一般失敗收尾比對。
func (db *DB) SnapshotQuotaResume(sessionID string) (owned bool, held, observed int64, err error) {
	var state string
	err = db.QueryRow(
		`SELECT quota_resume_state, quota_resume_generation FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&state, &observed)
	if err != nil {
		return false, 0, 0, err
	}
	if state == QuotaResumeResuming && observed > 0 {
		owned = true
		held = observed
	}
	return owned, held, observed, nil
}

// ClaimQuotaResume 一次讀出目前是否為 resuming，以及那一列的世代。
func (db *DB) ClaimQuotaResume(sessionID string) (owned bool, generation int64, err error) {
	owned, generation, _, err = db.SnapshotQuotaResume(sessionID)
	return
}

// ReleaseQuotaResumeGeneration 只把仍是這個世代的 resuming 轉成 none。
// 世代比對與狀態轉移是同一道 UPDATE。沒有列符合時回 false, nil。
func (db *DB) ReleaseQuotaResumeGeneration(sessionID string, generation int64) (bool, error) {
	if generation <= 0 {
		return false, nil
	}
	res, err := db.Exec(
		`UPDATE sessions SET
			quota_resume_state = ?,
			quota_resume_attempts = 0,
			quota_resume_at = ''
		 WHERE id = ? AND quota_resume_state = ? AND quota_resume_generation = ?`,
		QuotaResumeNone, sessionID, QuotaResumeResuming, generation,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetQuotaResume 讀取單一 session 的自動復原狀態、預定時間與次數。
func (db *DB) GetQuotaResume(sessionID string) (state, at string, attempts int, err error) {
	err = db.QueryRow(
		`SELECT quota_resume_state, quota_resume_at, quota_resume_attempts FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&state, &at, &attempts)
	return
}

// ListDueQuotaResume 列出 state 為 waiting 且 quota_resume_at 小於等於 now 的 session。
// 同時帶回列出當下的世代，讓之後的啟動或延後能對上同一筆。
func (db *DB) ListDueQuotaResume(now time.Time) ([]DueQuotaResume, error) {
	rows, err := db.Query(
		`SELECT id, quota_resume_at, quota_resume_generation FROM sessions
		 WHERE quota_resume_state = ? AND quota_resume_at <= ?
		 ORDER BY quota_resume_at, id`,
		QuotaResumeWaiting, quotaResumeTime(now),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DueQuotaResume{}
	for rows.Next() {
		var item DueQuotaResume
		if err := rows.Scan(&item.SessionID, &item.At, &item.Generation); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CancelQuotaResume 把這個 session 清成 none、次數歸零、時間清空，並把世代加 1。
// 已經是 none 也推進世代。世代不歸零、不重用。
func (db *DB) CancelQuotaResume(sessionID string) error {
	return execCancelQuotaResume(db, sessionID)
}

// StartQuotaResume 以同一道 UPDATE 把仍是觀察到的 waiting、世代與預定時間轉成 resuming。
// 次數與世代各加 1，並回傳新世代。身份已變、或預定時間已被延後時回 false, nil，不啟動。
func (db *DB) StartQuotaResume(sessionID string, generation int64, at string) (int64, bool, error) {
	if at == "" {
		return 0, false, nil
	}
	var next int64
	err := db.QueryRow(`
		UPDATE sessions SET
			quota_resume_state = ?,
			quota_resume_attempts = quota_resume_attempts + 1,
			quota_resume_generation = quota_resume_generation + 1
		WHERE id = ? AND quota_resume_state = ? AND quota_resume_generation = ? AND quota_resume_at = ?
		RETURNING quota_resume_generation`,
		QuotaResumeResuming, sessionID, QuotaResumeWaiting, generation, at,
	).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return next, true, nil
}

// DelayQuotaResume 只改仍是觀察到的 waiting、世代與預定時間的 quota_resume_at。
// 沒有對上時回 false，不改次數、不推進世代、也不表示延後成功。
func (db *DB) DelayQuotaResume(sessionID string, generation int64, observedAt, at string) (bool, error) {
	if observedAt == "" || at == "" {
		return false, nil
	}
	res, err := db.Exec(
		`UPDATE sessions SET quota_resume_at = ?
		 WHERE id = ? AND quota_resume_state = ? AND quota_resume_generation = ? AND quota_resume_at = ?`,
		at, sessionID, QuotaResumeWaiting, generation, observedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// RestoreQuotaResumeWaiting 只把仍是這個世代的 resuming 退回 waiting，並把次數減 1。
func (db *DB) RestoreQuotaResumeWaiting(sessionID string, generation int64) (bool, error) {
	if generation <= 0 {
		return false, nil
	}
	return execQuotaResumeTransition(context.Background(), db, sessionID, QuotaResumeResuming, QuotaResumeWaiting, "", true, generation, true)
}

// execCancelQuotaResume 清成 none、次數歸零、時間清空，並把世代加 1。
// 已經是 none 也推進世代，讓執行開始時記下的舊世代不能在取消之後再武裝。世代不歸零、不重用。
func execCancelQuotaResume(ex quotaSQL, sessionID string) error {
	_, err := ex.ExecContext(context.Background(),
		`UPDATE sessions SET
			quota_resume_state = ?,
			quota_resume_attempts = 0,
			quota_resume_at = '',
			quota_resume_generation = quota_resume_generation + 1
		WHERE id = ?`,
		QuotaResumeNone, sessionID,
	)
	return err
}

// RecoverQuotaResume 只把 resuming 轉回 waiting，時間改為 now，次數保留。
func (db *DB) RecoverQuotaResume(now time.Time) error {
	_, err := db.Exec(
		`UPDATE sessions SET quota_resume_state = ?, quota_resume_at = ? WHERE quota_resume_state = ?`,
		QuotaResumeWaiting, quotaResumeTime(now), QuotaResumeResuming,
	)
	return err
}
