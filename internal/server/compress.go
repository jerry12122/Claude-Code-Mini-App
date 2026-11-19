package server

import (
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
)

// staticCompress 只壓縮文字類前端資源（首載約 1.4MB，壓縮後約三成）。
// 圖片、串流、WebSocket 與 API 回應都不經過它：這些已壓縮、或需要即時送出。
func staticCompress() fiber.Handler {
	return compress.New(compress.Config{
		Next: func(c *fiber.Ctx) bool { return !compressiblePath(c.Path()) },
	})
}

func compressiblePath(p string) bool {
	if strings.HasPrefix(p, "/uploads/") {
		return false
	}
	if p == "/" {
		return true
	}
	switch path.Ext(p) {
	case ".js", ".css", ".html", ".svg", ".webmanifest":
		return true
	}
	return false
}
