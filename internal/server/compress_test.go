package server

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestStaticCompress(t *testing.T) {
	body := strings.Repeat("function demo() { return 1 }\n", 400)
	app := fiber.New()
	app.Use(staticCompress())
	app.Get("/*", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "application/octet-stream")
		return c.SendString(body)
	})

	get := func(path, enc string) (*httptest.ResponseRecorder, []byte) {
		req := httptest.NewRequest("GET", path, nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc)
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		rec := httptest.NewRecorder()
		for k, v := range res.Header {
			rec.Header()[k] = v
		}
		return rec, raw
	}

	t.Run("靜態資源在 gzip 下被壓縮且可還原", func(t *testing.T) {
		rec, raw := get("/js/app.bundle.js", "gzip")
		if rec.Header().Get("Content-Encoding") != "gzip" || len(raw) >= len(body)/2 {
			t.Fatalf("encoding=%q 壓縮後 %d / 原始 %d", rec.Header().Get("Content-Encoding"), len(raw), len(body))
		}
		zr, err := gzip.NewReader(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := io.ReadAll(zr)
		if string(plain) != body {
			t.Fatal("解壓後內容不一致")
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("缺少 Vary: Accept-Encoding：%q", rec.Header().Get("Vary"))
		}
	})
	t.Run("沒帶 Accept-Encoding 時原樣送出", func(t *testing.T) {
		rec, raw := get("/js/app.bundle.js", "")
		if rec.Header().Get("Content-Encoding") != "" || string(raw) != body {
			t.Fatalf("encoding=%q len=%d", rec.Header().Get("Content-Encoding"), len(raw))
		}
	})
	for _, p := range []string{"/uploads/a.js", "/uploads/photo.png", "/sessions/abc/ws", "/settings/general", "/logs/ws", "/media/x.jpg"} {
		t.Run("不壓縮 "+p, func(t *testing.T) {
			rec, raw := get(p, "gzip")
			if rec.Header().Get("Content-Encoding") != "" || string(raw) != body {
				t.Fatalf("%s 不該被壓縮：encoding=%q", p, rec.Header().Get("Content-Encoding"))
			}
		})
	}
	t.Run("首頁路徑壓縮", func(t *testing.T) {
		rec, _ := get("/", "gzip")
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("encoding=%q", rec.Header().Get("Content-Encoding"))
		}
	})
}
