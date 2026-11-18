package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const (
	GlobalInstructionNative   = "native"
	GlobalInstructionFirst    = "first"
	GlobalInstructionEvery    = "every"
	MaxSystemInstructionBytes = 16384
	settingGlobalInstruction  = "global_instruction"
)

type GlobalInstruction struct {
	Text string `json:"text"`
	Mode string `json:"mode"`
}

func (g *GlobalInstruction) Normalize() error {
	switch g.Mode {
	case GlobalInstructionNative, GlobalInstructionFirst, GlobalInstructionEvery:
	default:
		return errors.New("無效的全域指令注入模式")
	}
	g.Text = strings.TrimSpace(g.Text)
	if len(g.Text) > MaxSystemInstructionBytes || strings.ContainsRune(g.Text, 0) {
		return errors.New("全域指令最多 16384 UTF-8 bytes，且不可包含 U+0000")
	}
	return nil
}

func (db *DB) GetGlobalInstruction() (GlobalInstruction, error) {
	g := GlobalInstruction{Mode: GlobalInstructionNative}
	var value string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", settingGlobalInstruction).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return g, nil
	}
	if err != nil {
		return g, err
	}
	if err := json.Unmarshal([]byte(value), &g); err != nil || g.Normalize() != nil {
		return GlobalInstruction{}, errors.New("無法讀取全域指令設定")
	}
	return g, nil
}

func (db *DB) PutGlobalInstruction(g GlobalInstruction) error {
	value, err := json.Marshal(g)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingGlobalInstruction, string(value))
	return err
}

// ponytail: store confirmation on the original message so clearing history re-arms the first prompt.
func (db *DB) GlobalInstructionSent(sessionID string) (bool, error) {
	var sent bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM messages
		WHERE session_id = ? AND global_instruction_sent = 1)`, sessionID).Scan(&sent)
	return sent, err
}

func (db *DB) MarkGlobalInstructionSent(sessionID string, messageID int64) error {
	_, err := db.Exec(`UPDATE messages SET global_instruction_sent = 1
		WHERE session_id = ? AND id = ? AND role = 'user'`, sessionID, messageID)
	return err
}
