package db

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode"
)

// 分享的角色與範圍。
const (
	ShareRoleViewer = "viewer"
	ShareRoleEditor = "editor"

	ShareModeSnapshot = "snapshot"
	ShareModeLive     = "live"

	// MaxPINAttempts：PIN 連續錯誤達此次數即鎖定該分享。
	MaxPINAttempts = 5

	// MaxNicknameRunes：暱稱長度上限（字元數）。
	MaxNicknameRunes = 20

	// shareTimeLayout：shares.expires_at / created_at 的儲存格式（固定寬度 UTC，可直接字串比較）。
	shareTimeLayout = "2006-01-02T15:04:05Z"
)

var (
	ErrShareNotFound    = errors.New("分享不存在")
	ErrShareEnded       = errors.New("分享已結束")
	ErrShareLocked      = errors.New("PIN 錯誤次數過多，此分享已鎖定")
	ErrInvalidShare     = errors.New("無效的分享設定")
	ErrInvalidNickname  = errors.New("請輸入暱稱")
	errShareSessionGone = errors.New("聊天室不存在")
)

// PINError：PIN 錯誤；Remaining 為剩餘可嘗試次數（0 代表此次錯誤後已鎖定）。
type PINError struct{ Remaining int }

func (e *PINError) Error() string {
	if e.Remaining <= 0 {
		return ErrShareLocked.Error()
	}
	return fmt.Sprintf("PIN 錯誤，剩餘 %d 次嘗試", e.Remaining)
}

// Share 是一個臨時分享。PIN 以明文存放（臨時碼，只有擁有者能從列表讀到）。
type Share struct {
	ID             int64     `json:"id"`
	SessionID      string    `json:"session_id"`
	Token          string    `json:"token"`
	PIN            string    `json:"pin"`
	Role           string    `json:"role"`
	Mode           string    `json:"mode"`
	SnapshotMsgID  *int64    `json:"snapshot_msg_id,omitempty"` // 僅 snapshot；live 為 nil
	ExpiresAt      time.Time `json:"expires_at"`
	Revoked        bool      `json:"revoked"`
	FailedAttempts int       `json:"failed_attempts"`
	CreatedAt      time.Time `json:"created_at"`
}

// Active：未撤銷且未到期。
func (s *Share) Active(now time.Time) bool {
	return !s.Revoked && now.Before(s.ExpiresAt)
}

// Locked：PIN 錯誤次數已達上限。
func (s *Share) Locked() bool { return s.FailedAttempts >= MaxPINAttempts }

// Status 回傳 active / expired / revoked，供列表顯示。
func (s *Share) Status(now time.Time) string {
	switch {
	case s.Revoked:
		return "revoked"
	case !now.Before(s.ExpiresAt):
		return "expired"
	default:
		return "active"
	}
}

// ShareGuest 是通過 PIN 加入分享的訪客。
type ShareGuest struct {
	ID        int64
	ShareID   int64
	Nickname  string
	Token     string
	CreatedAt string
}

// GuestAccess 是 guest token 解析結果（訪客＋其所屬分享）。
type GuestAccess struct {
	Guest ShareGuest
	Share Share
}

// shareMu 序列化 join（PIN 計數與暱稱去重需要原子性）。
var shareMu sync.Mutex

const shareCols = `id, session_id, token, pin, role, mode, snapshot_msg_id, expires_at, revoked, failed_attempts, created_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanShare(r rowScanner) (*Share, error) {
	var s Share
	var snap sql.NullInt64
	var exp, created string
	var revoked int
	if err := r.Scan(&s.ID, &s.SessionID, &s.Token, &s.PIN, &s.Role, &s.Mode, &snap, &exp, &revoked, &s.FailedAttempts, &created); err != nil {
		return nil, err
	}
	if snap.Valid {
		v := snap.Int64
		s.SnapshotMsgID = &v
	}
	s.Revoked = revoked != 0
	s.ExpiresAt, _ = time.Parse(shareTimeLayout, exp)
	s.CreatedAt, _ = time.Parse(shareTimeLayout, created)
	return &s, nil
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomPIN() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ValidateShareOptions 檢查角色／範圍組合；editor 必須是 live。
func ValidateShareOptions(role, mode string) error {
	if role != ShareRoleViewer && role != ShareRoleEditor {
		return fmt.Errorf("%w：role 須為 viewer 或 editor", ErrInvalidShare)
	}
	if mode != ShareModeSnapshot && mode != ShareModeLive {
		return fmt.Errorf("%w：mode 須為 snapshot 或 live", ErrInvalidShare)
	}
	if role == ShareRoleEditor && mode != ShareModeLive {
		return fmt.Errorf("%w：editor 必須搭配 live", ErrInvalidShare)
	}
	return nil
}

// CreateShare 為 session 建立分享。snapshot 會記下當下最大的 messages.id。
func (db *DB) CreateShare(sessionID, role, mode string, ttl time.Duration) (*Share, error) {
	if err := ValidateShareOptions(role, mode); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w：時長必須大於 0", ErrInvalidShare)
	}
	if _, err := db.GetSession(sessionID); err != nil {
		return nil, errShareSessionGone
	}
	var snap any
	if mode == ShareModeSnapshot {
		var maxID int64
		if err := db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM messages WHERE session_id = ?`, sessionID).Scan(&maxID); err != nil {
			return nil, err
		}
		snap = maxID
	}
	pin, err := randomPIN()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ { // token UNIQUE 衝突幾乎不可能，仍重試以防萬一
		token, err := randomToken(24)
		if err != nil {
			return nil, err
		}
		res, err := db.Exec(`INSERT INTO shares (session_id, token, pin, role, mode, snapshot_msg_id, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, token, pin, role, mode, snap, now.Add(ttl).Format(shareTimeLayout), now.Format(shareTimeLayout))
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				continue
			}
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		return db.GetShare(id)
	}
	return nil, errors.New("無法產生分享 token")
}

func (db *DB) GetShare(id int64) (*Share, error) {
	s, err := scanShare(db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return s, err
}

func (db *DB) GetShareByToken(token string) (*Share, error) {
	s, err := scanShare(db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE token = ?`, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return s, err
}

// ListShares 回傳分享（新到舊）。sessionID 為空字串時回全部（上限 500 筆）。
func (db *DB) ListShares(sessionID string) ([]*Share, error) {
	q := `SELECT ` + shareCols + ` FROM shares`
	var args []any
	if sessionID != "" {
		q += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	q += ` ORDER BY id DESC LIMIT 500`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		s, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RevokeShare 結束指定分享；分享不存在回 ErrShareNotFound。
func (db *DB) RevokeShare(id int64) error {
	res, err := db.Exec(`UPDATE shares SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrShareNotFound
	}
	return nil
}

// ClearEndedShares 清除已撤銷／到期的分享及其訪客；id=0 表示全部，不刪除聊天室或訊息。
func (db *DB) ClearEndedShares(id int64) (int64, error) {
	shareMu.Lock()
	defer shareMu.Unlock()
	where := `(revoked = 1 OR expires_at <= ?)`
	args := []any{time.Now().UTC().Format(shareTimeLayout)}
	if id != 0 {
		where += ` AND id = ?`
		args = append(args, id)
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM share_guests WHERE share_id IN (SELECT id FROM shares WHERE `+where+`)`, args...); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM shares WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// RevokeActiveShares 結束全部有效分享，回傳被結束的 id（供踢線）。sessionID 非空時只處理該 session。
func (db *DB) RevokeActiveShares(sessionID string) ([]int64, error) {
	q := `SELECT id FROM shares WHERE revoked = 0 AND expires_at > ?`
	args := []any{time.Now().UTC().Format(shareTimeLayout)}
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE shares SET revoked = 1 WHERE id = ?`, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// RevokeSharesBySession 結束某 session 底下所有未撤銷的分享（刪除 session 時用），回傳被結束的 id。
func (db *DB) RevokeSharesBySession(sessionID string) ([]int64, error) {
	rows, err := db.Query(`SELECT id FROM shares WHERE session_id = ? AND revoked = 0`, sessionID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`UPDATE shares SET revoked = 1 WHERE session_id = ?`, sessionID); err != nil {
		return nil, err
	}
	return ids, nil
}

// NormalizeNickname 清理暱稱：去控制字元與方括號（避免偽造 agent 看到的 [暱稱] 前綴）、折疊空白、截斷長度。
func NormalizeNickname(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) || r == '[' || r == ']' {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxNicknameRunes {
		s = strings.TrimSpace(string(r[:MaxNicknameRunes]))
	}
	return s
}

// reservedNicknames：保留給擁有者的顯示名稱，訪客取同名時一律加後綴。
var reservedNicknames = map[string]bool{"擁有者": true, "owner": true}

// TryJoin 驗證 PIN 並建立訪客。
//   - 分享不存在／已結束／已鎖定時回對應 error（不累計錯誤次數）。
//   - PIN 錯誤累計 failed_attempts，達 MaxPINAttempts 即鎖定；回 *PINError。
//   - 暱稱在同一分享內重複時自動加數字後綴（2、3…）。
func (db *DB) TryJoin(token, pin, nickname string) (*GuestAccess, error) {
	shareMu.Lock()
	defer shareMu.Unlock()

	sh, err := db.GetShareByToken(token)
	if err != nil {
		return nil, err
	}
	if !sh.Active(time.Now()) {
		return nil, ErrShareEnded
	}
	if sh.Locked() {
		return nil, ErrShareLocked
	}
	nick := NormalizeNickname(nickname)
	if nick == "" {
		return nil, ErrInvalidNickname
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(pin)), []byte(sh.PIN)) != 1 {
		if _, err := db.Exec(`UPDATE shares SET failed_attempts = failed_attempts + 1 WHERE id = ?`, sh.ID); err != nil {
			return nil, err
		}
		return nil, &PINError{Remaining: MaxPINAttempts - sh.FailedAttempts - 1}
	}

	taken := map[string]bool{}
	rows, err := db.Query(`SELECT nickname FROM share_guests WHERE share_id = ?`, sh.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		taken[strings.ToLower(n)] = true
	}
	rows.Close()
	unique := nick
	for i := 2; taken[strings.ToLower(unique)] || reservedNicknames[strings.ToLower(unique)]; i++ {
		unique = fmt.Sprintf("%s%d", nick, i)
	}

	gtoken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	res, err := db.Exec(`INSERT INTO share_guests (share_id, nickname, token) VALUES (?, ?, ?)`, sh.ID, unique, gtoken)
	if err != nil {
		return nil, err
	}
	gid, _ := res.LastInsertId()
	// 「連續」錯誤：成功加入即歸零。
	if sh.FailedAttempts != 0 {
		if _, err := db.Exec(`UPDATE shares SET failed_attempts = 0 WHERE id = ?`, sh.ID); err != nil {
			return nil, err
		}
		sh.FailedAttempts = 0
	}
	return &GuestAccess{
		Guest: ShareGuest{ID: gid, ShareID: sh.ID, Nickname: unique, Token: gtoken},
		Share: *sh,
	}, nil
}

// ResolveGuestToken 以 guest token 查出訪客與分享（含最新的 revoked／到期狀態，呼叫端自行判斷 Active）。
func (db *DB) ResolveGuestToken(token string) (*GuestAccess, error) {
	if token == "" {
		return nil, ErrShareNotFound
	}
	var ga GuestAccess
	err := db.QueryRow(`SELECT id, share_id, nickname, token, created_at FROM share_guests WHERE token = ?`, token).
		Scan(&ga.Guest.ID, &ga.Guest.ShareID, &ga.Guest.Nickname, &ga.Guest.Token, &ga.Guest.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	if err != nil {
		return nil, err
	}
	sh, err := db.GetShare(ga.Guest.ShareID)
	if err != nil {
		return nil, err
	}
	ga.Share = *sh
	return &ga, nil
}
