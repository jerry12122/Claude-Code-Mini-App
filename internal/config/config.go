package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// ErrConfigMissing 表示工作目錄沒有 config.yaml。
// 檔案在但 YAML 壞掉或欄位非法不是這個錯誤，避免首次設定精靈把壞檔當成首次使用而覆寫。
var ErrConfigMissing = errors.New("找不到 config.yaml")

// PasswordValid 檢查網頁密碼能否單獨當登入方式。設定載入與設定精靈共用這一份規則。
func PasswordValid(pw string) error {
	pw = strings.TrimSpace(pw)
	switch {
	case pw == "":
		return errors.New("web.password 為空")
	case pw == "change-me":
		return errors.New("web.password 仍是 change-me")
	case len([]rune(pw)) < 8:
		return errors.New("web.password 少於 8 個字元")
	default:
		return nil
	}
}

type Config struct {
	BotToken       string  `mapstructure:"bot_token"`
	WhitelistTgIDs []int64 `mapstructure:"whitelist_tg_ids"`
	Web            Web     `mapstructure:"web"`
	NoAuth         bool    `mapstructure:"no_auth"`
	Server         Server  `mapstructure:"server"`
	DB             DB      `mapstructure:"db"`
	Shell          Shell   `mapstructure:"shell"`
	Notify         Notify  `mapstructure:"notify"`
	// McpToken 供 MCP client（其他 agent）認證 /mcp endpoint 的固定 service token；空字串表示停用 /mcp。
	McpToken string `mapstructure:"mcp_token"`
	// McpMaxHops 為 session 互問（send_message / ask_session 帶 from_session_id）的跳數上限，預設 5。
	McpMaxHops int `mapstructure:"mcp_max_hops"`
	// TelegramClient MTProto 個人客戶端設定（api_id 與 api_hash）
	TelegramClient TelegramClient `mapstructure:"telegram_client"`
}

// Notify 任務結束時的 Telegram 推播設定。
type Notify struct {
	OnError          bool `mapstructure:"on_error"`
	OnCancel         bool `mapstructure:"on_cancel"`
	OnShellError     bool `mapstructure:"on_shell_error"`
	ErrorPreviewLen  int  `mapstructure:"error_preview_len"`
	IncludePrompt    bool `mapstructure:"include_prompt"`
	PromptPreviewLen int  `mapstructure:"prompt_preview_len"`
}

// Shell 直連 shell 執行（等同伺服器端任意指令，預設關閉）。
type Shell struct {
	Enabled         bool     `mapstructure:"enabled"`
	Timeout         string   `mapstructure:"timeout"`          // 例如 "60s"
	MaxOutputBytes  int      `mapstructure:"max_output_bytes"` // 單次輸出上限（位元組）
	AllowedCommands []string `mapstructure:"allowed_commands"` // 非空時僅允許第一個指令名稱在白名單內；空則不限制
}

type Web struct {
	Password          string   `mapstructure:"password"`
	AllowedCIDRs      []string `mapstructure:"allowed_cidrs"`
	TrustedProxies    []string `mapstructure:"trusted_proxies"`
	SessionTTL        string   `mapstructure:"session_ttl"`
	DefaultNotifyTgID int64    `mapstructure:"default_notify_tg_id"` // 網頁登入時預設綁定的 TG 通知對象（須在白名單）；0 表示未指定
}

type Server struct {
	Port            int `mapstructure:"port"`
	MaxMessageBytes int `mapstructure:"max_message_bytes"`
}

type DB struct {
	Path string `mapstructure:"path"`
}

// TelegramClient MTProto 個人帳號客戶端設定。
type TelegramClient struct {
	Enabled       bool   `mapstructure:"enabled"`
	APIID         int    `mapstructure:"api_id"`
	APIHash       string `mapstructure:"api_hash"`
	DefaultTarget string `mapstructure:"default_target"` // 預設聊天對象（@username 或 ID）
	SessionFile   string `mapstructure:"session_file"`   // session 儲存路徑，預設 ./telegram.session
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// 預設值
	viper.SetDefault("no_auth", false)
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.max_message_bytes", 16<<20)
	viper.SetDefault("db.path", "./claude-miniapp.db")
	viper.SetDefault("web.session_ttl", "168h")
	viper.SetDefault("web.trusted_proxies", []string{"127.0.0.1/32", "::1/128"})
	viper.SetDefault("web.allowed_cidrs", []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	})
	viper.SetDefault("shell.enabled", false)
	viper.SetDefault("shell.timeout", "60s")
	viper.SetDefault("shell.max_output_bytes", 524288)
	viper.SetDefault("notify.on_error", true)
	viper.SetDefault("notify.on_cancel", false)
	viper.SetDefault("notify.on_shell_error", true)
	viper.SetDefault("notify.error_preview_len", 800)
	viper.SetDefault("notify.include_prompt", true)
	viper.SetDefault("notify.prompt_preview_len", 120)
	viper.SetDefault("mcp_max_hops", 5)
	viper.SetDefault("telegram_client.enabled", false)
	viper.SetDefault("telegram_client.session_file", "./telegram.session")

	if err := viper.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("%w: %w", ErrConfigMissing, err)
		}
		return nil, fmt.Errorf("讀取 config.yaml 失敗: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析設定失敗: %w", err)
	}

	if cfg.BotToken == "" && !cfg.NoAuth {
		if err := PasswordValid(cfg.Web.Password); err != nil {
			return nil, fmt.Errorf("需要 bot_token 或 web.password: %w", err)
		}
	}

	if cfg.Server.MaxMessageBytes < 1 || cfg.Server.MaxMessageBytes > 256<<20 {
		return nil, errors.New("server.max_message_bytes 須介於 1 到 268435456")
	}
	return &cfg, nil
}
