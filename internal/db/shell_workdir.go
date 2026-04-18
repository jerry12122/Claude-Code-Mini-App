package db

import (
	"strings"
)

// ListShellCommandsForWorkDirKey 回傳該工作目錄鍵下允許的指令名稱（已正規化小寫存於 DB）。
func (db *DB) ListShellCommandsForWorkDirKey(workDirKey string) ([]string, error) {
	workDirKey = strings.TrimSpace(workDirKey)
	if workDirKey == "" {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT command FROM shell_workdir_commands WHERE work_dir_key = ? ORDER BY command ASC`,
		workDirKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// AddShellWorkdirCommand 將指令加入某 work_dir_key 的白名單（INSERT OR IGNORE）。
func (db *DB) AddShellWorkdirCommand(workDirKey, command string) error {
	workDirKey = strings.TrimSpace(workDirKey)
	command = strings.TrimSpace(strings.ToLower(command))
	if workDirKey == "" || command == "" {
		return nil
	}
	_, err := db.Exec(
		`INSERT OR IGNORE INTO shell_workdir_commands (work_dir_key, command) VALUES (?, ?)`,
		workDirKey, command,
	)
	return err
}
