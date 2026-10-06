package auth

import (
	"fmt"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// RealIP 只採信已設定的可信代理；代理須覆寫 CF-Connecting-IP 或附加直連來源至 X-Forwarded-For。
// X-Forwarded-For 由右往左跳過可信代理，避免使用者偽造最左側的 IP。
func RealIP(c *fiber.Ctx) string {
	remote := c.Context().RemoteIP().String()
	if !c.App().Config().EnableTrustedProxyCheck || !c.IsProxyTrusted() {
		return remote
	}
	if raw := c.Get("CF-Connecting-IP"); raw != "" {
		if ip := net.ParseIP(strings.TrimSpace(raw)); ip != nil {
			return ip.String()
		}
		return "" // 代理提供無效來源時拒絕 IP 白名單判斷，不回退成代理的內網 IP
	}
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		trusted, _ := ParseCIDRs(c.App().Config().TrustedProxies) // server 啟動時已驗證設定
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				return ""
			}
			if !IsAllowed(ip.String(), trusted) || i == 0 {
				return ip.String()
			}
		}
	}
	return remote
}

// ParseCIDRs 將字串陣列解析為 net.IPNet 清單。
func ParseCIDRs(strs []string) ([]net.IPNet, error) {
	nets := make([]net.IPNet, 0, len(strs))
	for _, s := range strs {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("無效 CIDR %q: %w", s, err)
		}
		nets = append(nets, *ipNet)
	}
	return nets, nil
}

// IsAllowed 檢查 IP 是否在允許的 CIDR 清單內。
func IsAllowed(ipStr string, allowed []net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
