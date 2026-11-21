package db

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxSessionCategoryRunes = 80

var (
	// ErrSessionCategoryInvalid 是空白、超長、含 NUL、或目錄不是既有非空實體 work_dir。後續 API 對 HTTP 400。
	ErrSessionCategoryInvalid = errors.New("分類名稱或工作目錄無效")
	// ErrSessionCategoryNotFound 是 id 不存在。後續 API 對 HTTP 404。
	ErrSessionCategoryNotFound = errors.New("分類不存在")
	// ErrSessionCategoryDuplicate 是同一工作目錄 trim 後名稱精確重複。後續 API 對 HTTP 409。
	ErrSessionCategoryDuplicate = errors.New("同目錄已有相同分類名稱")
)

// SessionCategory 是某個實體工作目錄下的單層分類。id 建立後不隨改名改變。
type SessionCategory struct {
	ID      string `json:"id"`
	WorkDir string `json:"work_dir"`
	Name    string `json:"name"`
}

func migrateSessionCategories(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS session_categories (
		id TEXT PRIMARY KEY,
		work_dir TEXT NOT NULL,
		name TEXT NOT NULL,
		UNIQUE (work_dir, name)
	)`); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'category_id'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// 可空參照只寫進 schema。連線池沒開 foreign_keys，刪除解綁靠下面的交易，不靠這條自動生效。
	_, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN category_id TEXT REFERENCES session_categories(id) ON DELETE SET NULL`)
	return err
}

func normalizeCategoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsRune(name, 0) || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxSessionCategoryRunes {
		return "", ErrSessionCategoryInvalid
	}
	return name, nil
}

func normalizeCategoryWorkDir(workDir string) (string, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" || strings.ContainsRune(workDir, 0) || !utf8.ValidString(workDir) {
		return "", ErrSessionCategoryInvalid
	}
	return workDir, nil
}

func categoryWorkDirExists(q interface {
	QueryRow(string, ...any) *sql.Row
}, workDir string) (bool, error) {
	var ok bool
	err := q.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM work_dirs WHERE path = ?)
		    OR EXISTS(SELECT 1 FROM sessions WHERE trim(work_dir) = ?)`, workDir, workDir).Scan(&ok)
	return ok, err
}

func isSessionCategoryNameConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (db *DB) CreateSessionCategory(workDir, name string) (*SessionCategory, error) {
	workDir, err := normalizeCategoryWorkDir(workDir)
	if err != nil {
		return nil, err
	}
	name, err = normalizeCategoryName(name)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ok, err := categoryWorkDirExists(tx, workDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionCategoryInvalid
	}
	cat := &SessionCategory{ID: uuid.NewString(), WorkDir: workDir, Name: name}
	if _, err := tx.Exec(`INSERT INTO session_categories (id, work_dir, name) VALUES (?, ?, ?)`, cat.ID, cat.WorkDir, cat.Name); err != nil {
		if isSessionCategoryNameConflict(err) {
			return nil, ErrSessionCategoryDuplicate
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cat, nil
}

func (db *DB) ListSessionCategories(workDir string) ([]SessionCategory, error) {
	workDir, err := normalizeCategoryWorkDir(workDir)
	if err != nil {
		return nil, err
	}
	ok, err := categoryWorkDirExists(db, workDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionCategoryInvalid
	}
	rows, err := db.Query(`SELECT id, work_dir, name FROM session_categories WHERE work_dir = ? ORDER BY name, id`, workDir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionCategory{}
	for rows.Next() {
		var c SessionCategory
		if err := rows.Scan(&c.ID, &c.WorkDir, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) GetSessionCategory(id string) (*SessionCategory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrSessionCategoryNotFound
	}
	var c SessionCategory
	err := db.QueryRow(`SELECT id, work_dir, name FROM session_categories WHERE id = ?`, id).Scan(&c.ID, &c.WorkDir, &c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionCategoryNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) RenameSessionCategory(id, name string) (*SessionCategory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrSessionCategoryNotFound
	}
	name, err := normalizeCategoryName(name)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var workDir, current string
	err = tx.QueryRow(`SELECT work_dir, name FROM session_categories WHERE id = ?`, id).Scan(&workDir, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionCategoryNotFound
	}
	if err != nil {
		return nil, err
	}
	if current == name {
		return &SessionCategory{ID: id, WorkDir: workDir, Name: current}, nil
	}
	if _, err := tx.Exec(`UPDATE session_categories SET name = ? WHERE id = ?`, name, id); err != nil {
		if isSessionCategoryNameConflict(err) {
			return nil, ErrSessionCategoryDuplicate
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SessionCategory{ID: id, WorkDir: workDir, Name: name}, nil
}

func (db *DB) DeleteSessionCategory(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrSessionCategoryNotFound
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 只清歸屬欄。不改 last_active、已讀、status、work_dir，也不刪歷史或佇列。
	if _, err := tx.Exec(`UPDATE sessions SET category_id = NULL WHERE category_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM session_categories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionCategoryNotFound
	}
	return tx.Commit()
}

// SetSessionCategory 在同一個立即交易裡指派或解除。categoryID 為 nil 表示解除。
// 目錄比對用 trim 後的儲存字串，大小寫敏感。不改 last_active、已讀、status 或 work_dir。
func (db *DB) SetSessionCategory(sessionID string, categoryID *string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var workDir string
	err = tx.QueryRow(`SELECT trim(work_dir) FROM sessions WHERE id = ?`, sessionID).Scan(&workDir)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if categoryID == nil {
		if _, err := tx.Exec(`UPDATE sessions SET category_id = NULL WHERE id = ?`, sessionID); err != nil {
			return err
		}
		return tx.Commit()
	}
	id := strings.TrimSpace(*categoryID)
	if id == "" {
		return ErrSessionCategoryInvalid
	}
	res, err := tx.Exec(`UPDATE sessions SET category_id = ?
		WHERE id = ? AND EXISTS (
			SELECT 1 FROM session_categories c
			WHERE c.id = ? AND c.work_dir = trim(sessions.work_dir)
		)`, id, sessionID, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return tx.Commit()
	}
	var catDir string
	err = tx.QueryRow(`SELECT work_dir FROM session_categories WHERE id = ?`, id).Scan(&catDir)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionCategoryNotFound
	}
	if err != nil {
		return err
	}
	if catDir != workDir {
		return ErrSessionCategoryInvalid
	}
	return ErrSessionCategoryNotFound
}
