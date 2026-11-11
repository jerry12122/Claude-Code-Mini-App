package api

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
)

// ipRateLimiter 是固定視窗的記憶體限流（依來源 IP 計次）。
// 自製而不用 fiber limiter：後者會多帶 msgp 相依，這裡只需要最簡單的計次。
type ipRateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*ipWindow
	now    func() time.Time // 測試可替換
}

type ipWindow struct {
	start time.Time
	n     int
}

func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{max: max, window: window, hits: map[string]*ipWindow{}, now: time.Now}
}

// allow 記一次請求；超過上限回 false。
func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// 順手清掉過期項目，避免 map 無限成長。
	if len(l.hits) > 1024 {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.Sub(w.start) >= l.window {
		l.hits[key] = &ipWindow{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.max
}

// JoinLimiter 回傳 join 專用的限流中介層（每個來源 IP 每分鐘最多 joinRatePerMinute 次）。
func JoinLimiter() fiber.Handler {
	return limitByIP(newIPRateLimiter(joinRatePerMinute, time.Minute))
}

// LoginFailureLimiter counts only failed passwords. Blocking is per source IP,
// lasts at most a minute, and never revokes an already authenticated session.
// ponytail: counts are process-local; use shared storage for multiple instances.
func LoginFailureLimiter() fiber.Handler {
	return loginFailureLimiter(newIPRateLimiter(5, time.Minute))
}

func loginFailureLimiter(l *ipRateLimiter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ip := auth.RealIP(c)
		l.mu.Lock()
		var wait time.Duration
		if w := l.hits[ip]; w != nil && w.n >= l.max {
			wait = l.window - l.now().Sub(w.start)
		}
		l.mu.Unlock()
		if wait > 0 {
			c.Set("Retry-After", strconv.Itoa(int((wait+time.Second-1)/time.Second)))
			return jsonErr(c, 429, "登入失敗次數過多，請稍後再試")
		}
		err := c.Next()
		if c.Response().StatusCode() == 401 {
			l.allow(ip)
		} else if c.Response().StatusCode() >= 200 && c.Response().StatusCode() < 300 {
			l.mu.Lock()
			delete(l.hits, ip)
			l.mu.Unlock()
		}
		return err
	}
}

func limitByIP(l *ipRateLimiter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !l.allow(auth.RealIP(c)) {
			return jsonErr(c, 429, "嘗試次數過多，請稍後再試")
		}
		return c.Next()
	}
}
