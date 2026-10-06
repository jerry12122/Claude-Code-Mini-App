package config

import (
	"os"
	"reflect"
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
