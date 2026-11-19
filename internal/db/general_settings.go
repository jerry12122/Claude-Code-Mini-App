package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const SettingKeyGeneral = "general"

// GeneralSettings 「一般」設定分類。缺欄位即零值，不需遷移。
type GeneralSettings struct {
	// VscodeNoAdmin: 開啟 VSCode 時以一般使用者權限啟動（透過 runas /trustlevel:0x20000），
	// 避免伺服器以系統管理員身分執行本程式時，跳出「Another instance of Code is already
	// running as administrator」錯誤。僅 Windows 有效，其他平台忽略。
	VscodeNoAdmin bool `json:"vscodeNoAdmin"`
	// ClaudeAutoAllowAsk：bypassPermissions 的 Claude run 命中 ask 規則時，由後端直接允許。預設關。
	ClaudeAutoAllowAsk bool `json:"claudeAutoAllowAsk"`
	// AutoResumeOnQuota：5 小時額度用盡後自動繼續。預設關；缺欄位即 false。
	AutoResumeOnQuota bool `json:"autoResumeOnQuota"`
	// QuotaGuardEnabled／QuotaGuardPercent：額度守門。尚未儲存過時由 EffectiveGeneralSettings 依 config 百分比決定。
	QuotaGuardEnabled bool    `json:"quotaGuardEnabled"`
	QuotaGuardPercent float64 `json:"quotaGuardPercent"`
}

// AutoResumeOnQuota 讀取「額度用盡後自動繼續」開關；沒有設定列時為 false。
func (db *DB) AutoResumeOnQuota() (bool, error) {
	s, err := db.GetGeneralSettings()
	return s.AutoResumeOnQuota, err
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
// autoResumeOnQuota 由開變關時，在同一筆 BEGIN IMMEDIATE 把每個 session 清成 none、
// 次數歸零、時間清空，並把世代加 1。包含當時已是 none 的普通執行。
// 不刪訊息、不改暫停。寫入失敗則狀態、世代與設定一起回滾。false→false 不推進世代。
func (db *DB) PutGeneralSettings(raw []byte) (GeneralSettings, error) {
	var s GeneralSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return GeneralSettings{}, err
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return GeneralSettings{}, err
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return GeneralSettings{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return GeneralSettings{}, err
	}
	done := false
	defer func() {
		if !done {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	current, err := readGeneralSettings(ctx, conn)
	if err != nil {
		return GeneralSettings{}, err
	}
	if current.AutoResumeOnQuota && !s.AutoResumeOnQuota {
		if _, err := conn.ExecContext(ctx,
			`UPDATE sessions SET
				quota_resume_state = ?,
				quota_resume_attempts = 0,
				quota_resume_at = '',
				quota_resume_generation = quota_resume_generation + 1`,
			QuotaResumeNone); err != nil {
			return GeneralSettings{}, err
		}
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		SettingKeyGeneral, string(encoded)); err != nil {
		return GeneralSettings{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return GeneralSettings{}, err
	}
	done = true
	return s, nil
}

func readGeneralSettings(ctx context.Context, conn *sql.Conn) (GeneralSettings, error) {
	var value string
	err := conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, SettingKeyGeneral).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return GeneralSettings{}, nil
	}
	if err != nil {
		return GeneralSettings{}, err
	}
	var s GeneralSettings
	if err := json.Unmarshal([]byte(value), &s); err != nil {
		return GeneralSettings{}, nil
	}
	return s, nil
}

// EffectiveGeneralSettings 回傳一般設定的有效值。
// 沒有設定列，或 JSON 沒有 quotaGuardPercent 鍵時，守門改由 configGuardPercent 決定：
// 大於 0 則啟用並採用該百分比；否則停用、百分比為 90。其餘欄位仍用已儲存的值（無列則為零值）。
func (db *DB) EffectiveGeneralSettings(configGuardPercent float64) (GeneralSettings, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingKeyGeneral).Scan(&value)
	if err == sql.ErrNoRows {
		return generalSettingsFromConfig(configGuardPercent), nil
	}
	if err != nil {
		return GeneralSettings{}, err
	}
	var s GeneralSettings
	if err := json.Unmarshal([]byte(value), &s); err != nil {
		return generalSettingsFromConfig(configGuardPercent), nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		return generalSettingsFromConfig(configGuardPercent), nil
	}
	if _, ok := keys["quotaGuardPercent"]; !ok {
		s.QuotaGuardEnabled, s.QuotaGuardPercent = guardFromConfig(configGuardPercent)
	}
	return s, nil
}

func generalSettingsFromConfig(configGuardPercent float64) GeneralSettings {
	enabled, percent := guardFromConfig(configGuardPercent)
	return GeneralSettings{QuotaGuardEnabled: enabled, QuotaGuardPercent: percent}
}

func guardFromConfig(configGuardPercent float64) (enabled bool, percent float64) {
	if configGuardPercent > 0 {
		return true, configGuardPercent
	}
	return false, 90
}
