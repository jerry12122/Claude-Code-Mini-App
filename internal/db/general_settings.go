package db

import (
	"database/sql"
	"encoding/json"
)

const SettingKeyGeneral = "general"

// GeneralSettings 「一般」設定分類：目前僅一項開關。
type GeneralSettings struct {
	// VscodeNoAdmin: 開啟 VSCode 時以一般使用者權限啟動（透過 runas /trustlevel:0x20000），
	// 避免伺服器以系統管理員身分執行本程式時，跳出「Another instance of Code is already
	// running as administrator」錯誤。僅 Windows 有效，其他平台忽略。
	VscodeNoAdmin bool `json:"vscodeNoAdmin"`
}

// GetGeneralSettings 讀取一般設定；無列時回預設值（全 false）。
func (db *DB) GetGeneralSettings() (GeneralSettings, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingKeyGeneral).Scan(&value)
	if err == sql.ErrNoRows {
		return GeneralSettings{}, nil
	}
	if err != nil {
		return GeneralSettings{}, err
	}
	var s GeneralSettings
	if err := json.Unmarshal([]byte(value), &s); err != nil {
		// DB 損壞時回預設，不視為錯誤（與 GetAppearance 一致的容錯策略）。
		return GeneralSettings{}, nil
	}
	return s, nil
}

// PutGeneralSettings 整包覆寫一般設定。
func (db *DB) PutGeneralSettings(raw []byte) (GeneralSettings, error) {
	var s GeneralSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return GeneralSettings{}, err
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return GeneralSettings{}, err
	}
	_, err = db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		SettingKeyGeneral, string(encoded),
	)
	if err != nil {
		return GeneralSettings{}, err
	}
	return s, nil
}
