package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	BotToken       string  `mapstructure:"bot_token"`
	WhitelistTgIDs []int64 `mapstructure:"whitelist_tg_ids"`
	Web            Web     `mapstructure:"web"`
	NoAuth         bool    `mapstructure:"no_auth"`
	Server         Server  `mapstructure:"server"`
	DB             DB      `mapstructure:"db"`
}

type Web struct {
	Password          string   `mapstructure:"password"`
	AllowedCIDRs      []string `mapstructure:"allowed_cidrs"`
	SessionTTL        string   `mapstructure:"session_ttl"`
	DefaultNotifyTgID int64    `mapstructure:"default_notify_tg_id"` // 網頁登入時預設綁定的 TG 通知對象（須在白名單）；0 表示未指定
}

type Server struct {
	Port int `mapstructure:"port"`
}

type DB struct {
	Path string `mapstructure:"path"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// 預設值
	viper.SetDefault("no_auth", false)
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("db.path", "./claude-miniapp.db")
	viper.SetDefault("web.session_ttl", "24h")
	viper.SetDefault("web.allowed_cidrs", []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	})

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("讀取 config.yaml 失敗: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析設定失敗: %w", err)
	}

	if cfg.BotToken == "" && !cfg.NoAuth {
		return nil, fmt.Errorf("config.yaml 缺少 bot_token（或設定 no_auth: true 跳過驗證）")
	}

	return &cfg, nil
}
