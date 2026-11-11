package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestLoginFailureLimit(t *testing.T) {
	l := newIPRateLimiter(5, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	app := fiber.New(fiber.Config{EnableTrustedProxyCheck: true, TrustedProxies: []string{"0.0.0.0/0", "::/0"}})
	calls := 0
	app.Post("/login", loginFailureLimiter(l), func(c *fiber.Ctx) error {
		calls++
		if c.Get("Password") == "correct" {
			return c.SendStatus(200)
		}
		return c.SendStatus(401)
	})
	request := func(ip, password string, status int, retry string) {
		t.Helper()
		req := httptest.NewRequest("POST", "/login", nil)
		req.Header.Set("CF-Connecting-IP", ip)
		req.Header.Set("Password", password)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != status || resp.Header.Get("Retry-After") != retry {
			t.Fatalf("status=%d retry=%q, want %d %q", resp.StatusCode, resp.Header.Get("Retry-After"), status, retry)
		}
	}
	for i := 0; i < 4; i++ {
		request("192.0.2.1", "wrong", 401, "")
	}
	request("192.0.2.1", "correct", 200, "") // Success clears earlier failures.
	for i := 0; i < 5; i++ {
		request("192.0.2.1", "wrong", 401, "")
	}
	before := calls
	request("192.0.2.1", "correct", 429, "60")
	if calls != before {
		t.Fatal("blocked login reached password verification")
	}
	request("192.0.2.2", "correct", 200, "") // No global account lockout.
	now = now.Add(59*time.Second + 500*time.Millisecond)
	request("192.0.2.1", "wrong", 429, "1")
	now = now.Add(500 * time.Millisecond)
	request("192.0.2.1", "correct", 200, "") // Blocked retries do not prolong the window.
}
