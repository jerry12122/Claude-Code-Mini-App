package config

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestTrustedProxiesConfig(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       []string
	}{
		{"舊設定保留同機代理", "no_auth: true\n", []string{"127.0.0.1/32", "::1/128"}},
		{"明確停用代理", "no_auth: true\nweb:\n  trusted_proxies: []\n", []string{}},
		{"設定其他代理", "no_auth: true\nweb:\n  trusted_proxies: [10.0.0.2/32]\n", []string{"10.0.0.2/32"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yaml", []byte(tt.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.Web.TrustedProxies, tt.want) {
				t.Fatalf("trusted_proxies = %v，預期 %v", cfg.Web.TrustedProxies, tt.want)
			}
		})
	}
}

func TestLoadMissingConfig(t *testing.T) {
	t.Run("空目錄", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Chdir(t.TempDir())
		_, err := Load()
		if !errors.Is(err, ErrConfigMissing) {
			t.Fatalf("err = %v，預期 ErrConfigMissing", err)
		}
	})
	t.Run("壞 YAML", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Chdir(t.TempDir())
		if err := os.WriteFile("config.yaml", []byte("bot_token: [\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load()
		if err == nil {
			t.Fatal("壞 YAML 應失敗")
		}
		if errors.Is(err, ErrConfigMissing) {
			t.Fatalf("壞 YAML 被當成缺檔: %v", err)
		}
	})
}

func TestLoadBotTokenOptional(t *testing.T) {
	tests := []struct {
		name, yaml string
		ok         bool
	}{
		{"僅密碼通過", "web:\n  password: correct-horse\n", true},
		{"兩者皆無", "{}\n", false},
		{"change-me", "web:\n  password: change-me\n", false},
		{"7 字元密碼", "web:\n  password: abcdefg\n", false},
		{"no_auth 且 token 為空", "no_auth: true\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yaml", []byte(tt.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load()
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "需要 bot_token 或 web.password") {
				t.Fatalf("err = %v，預期指出需要 bot_token 或 web.password", err)
			}
		})
	}
}

func TestQuotaGuardPercentConfig(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       float64
	}{
		{"未設定預設 90", "no_auth: true\n", 90},
		{"設 0 停用", "no_auth: true\nquota_guard_percent: 0\n", 0},
		{"自訂門檻", "no_auth: true\nquota_guard_percent: 75\n", 75},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yaml", []byte(tt.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.QuotaGuardPercent != tt.want {
				t.Fatalf("quota_guard_percent = %v，預期 %v", cfg.QuotaGuardPercent, tt.want)
			}
		})
	}
}
