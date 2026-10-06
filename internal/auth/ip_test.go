package auth

import (
	"net"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func TestRealIP(t *testing.T) {
	loopback := []string{"127.0.0.1/32", "::1/128"}
	tests := []struct {
		name, remote, cf, xff, want string
		trusted                     []string
	}{
		{"直連不採信 CF", "203.0.113.10", "127.0.0.1", "", "203.0.113.10", loopback},
		{"直連不採信 XFF", "203.0.113.10", "", "192.168.1.1", "203.0.113.10", loopback},
		{"內網不是預設可信代理", "192.168.1.10", "127.0.0.1", "", "192.168.1.10", loopback},
		{"無標頭保留直連來源", "127.0.0.1", "", "", "127.0.0.1", loopback},
		{"同機 Tunnel 保留 CF", "127.0.0.1", " 203.0.113.10 ", "192.168.1.1", "203.0.113.10", loopback},
		{"IPv6 同機代理", "::1", "2001:db8::10", "", "2001:db8::10", loopback},
		{"同機代理 XFF", "127.0.0.1", "", "203.0.113.10", "203.0.113.10", loopback},
		{"忽略偽造 XFF 前綴", "127.0.0.1", "", "192.168.1.1, 203.0.113.10", "203.0.113.10", loopback},
		{"多層可信代理", "127.0.0.1", "", "192.168.1.1, 203.0.113.10, 10.0.0.2", "203.0.113.10", []string{"127.0.0.1/32", "10.0.0.2/32"}},
		{"明確設定其他代理", "10.0.0.2", "203.0.113.10", "", "203.0.113.10", []string{"10.0.0.2/32"}},
		{"空清單不採信標頭", "127.0.0.1", "203.0.113.10", "", "127.0.0.1", nil},
		{"無效 CF 不得回退成內網", "127.0.0.1", "invalid", "192.168.1.1", "", loopback},
		{"無效 XFF 不得回退成內網", "127.0.0.1", "", "192.168.1.1, invalid", "", loopback},
		{"全為可信代理取最左來源", "127.0.0.1", "", "::1, 127.0.0.1", "::1", loopback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{EnableTrustedProxyCheck: true, TrustedProxies: tt.trusted})
			var req fasthttp.Request
			req.Header.Set("CF-Connecting-IP", tt.cf)
			req.Header.Set("X-Forwarded-For", tt.xff)
			var ctx fasthttp.RequestCtx
			ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP(tt.remote), Port: 1234}, nil)
			c := app.AcquireCtx(&ctx)
			defer app.ReleaseCtx(c)
			if got := RealIP(c); got != tt.want {
				t.Fatalf("RealIP = %q，預期 %q", got, tt.want)
			}
		})
	}
}

// 未啟用代理檢查時也不得使用 c.IP() 的 ProxyHeader 解析結果。
func TestRealIPWithoutTrustedProxyCheck(t *testing.T) {
	app := fiber.New(fiber.Config{ProxyHeader: "X-Forwarded-For"})
	var req fasthttp.Request
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	var ctx fasthttp.RequestCtx
	ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP("203.0.113.10")}, nil)
	c := app.AcquireCtx(&ctx)
	defer app.ReleaseCtx(c)
	if got := RealIP(c); got != "203.0.113.10" {
		t.Fatalf("未啟用代理檢查時應使用直連來源，得到 %q", got)
	}
}
