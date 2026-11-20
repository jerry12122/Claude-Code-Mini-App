package server

import (
	"os"
	"testing"
)

// 只比較 address，不呼叫 Start／Listen；RED 階段也不開 wildcard listener。
func TestServerListenAddress(t *testing.T) {
	for _, tc := range []struct {
		name, value     string
		unset, isolated bool
		want            string
	}{
		{name: "明確測試開關", value: "1", want: "127.0.0.1:12345"},
		{name: "未設定維持正式預設", unset: true, want: ":12345"},
		{name: "空值維持正式預設", value: "", want: ":12345"},
		{name: "零不是啟用", value: "0", want: ":12345"},
		{name: "true不是啟用", value: "true", want: ":12345"},
		{name: "空白不是啟用", value: " 1 ", want: ":12345"},
		{name: "external未設定仍限loopback", unset: true, isolated: true, want: "127.0.0.1:12345"},
		{name: "external空值仍限loopback", isolated: true, want: "127.0.0.1:12345"},
		{name: "external與開關同用", value: "1", isolated: true, want: "127.0.0.1:12345"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MINIAPP_TEST_LOOPBACK", tc.value)
			if tc.unset {
				if err := os.Unsetenv("MINIAPP_TEST_LOOPBACK"); err != nil {
					t.Fatal(err)
				}
			}
			if got := serverListenAddress(12345, tc.isolated); got != tc.want {
				t.Fatalf("address=%q，預期 %q", got, tc.want)
			}
		})
	}
}
