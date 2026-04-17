package db

func (db *DB) IsUserAllowed(tgID int64) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE tg_id = ?`, tgID).Scan(&count)
	return count > 0, err
}

func (db *DB) AddUser(tgID int64, username string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO users (tg_id, username) VALUES (?, ?)`,
		tgID, username,
	)
	return err
}

// DefaultNotifyTgIDIfSingle 若白名單僅有一位使用者，回傳其 tg_id；多人或無人則回傳 0。
func (db *DB) DefaultNotifyTgIDIfSingle() (int64, error) {
	ids, err := db.ListUsers()
	if err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, nil
	}
	return ids[0], nil
}

func (db *DB) ListUsers() ([]int64, error) {
	rows, err := db.Query(`SELECT tg_id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
