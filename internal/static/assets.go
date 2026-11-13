// Package static 提供前端公開資源的單一 fs.FS 來源，與靜態檔服務。
// production 建置把 index.html、favicon.ico、js/ 嵌入執行檔；開發建置直接讀磁碟（見 assets_*.go）。
package static

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// IsPublic 回報 name（slash 相對路徑）是否為公開前端資源：index.html、favicon.ico 與 js/ 下的檔案。
// 這是唯一的公開白名單；AGENT.md、uploads/、設定、DB、日誌與 Go 原始碼都不在內。
func IsPublic(name string) bool {
	if !fs.ValidPath(name) || name == "." {
		return false
	}
	switch {
	case name == "index.html", name == "favicon.ico", name == "robots.txt", name == "manifest.webmanifest", name == "sw.js":
		return true
	case strings.HasPrefix(name, "js/"):
		return true
	}
	return false
}

// Handler 只服務 assets 內存在的公開資源；其他請求（含未知 API）交給後續路由，不做 SPA fallback。
// 路徑先經 path.Clean 正規化，再比對白名單，因此 ".." 無法跳出資源根。
func Handler(assets fs.FS) fiber.Handler {
	bundle := newBundler(assets, Production)
	return func(c *fiber.Ctx) error {
		if m := c.Method(); m != fiber.MethodGet && m != fiber.MethodHead {
			return c.Next()
		}
		name := publicName(c.Path())
		if name == "" {
			return c.Next()
		}
		if name == "index.html" || name == bundleName {
			if res := bundle.result(); len(res.files) > 0 {
				return serveBundled(c, res, name)
			}
		}
		found, err := serveFile(c, assets, name, contentType)
		if !found {
			return c.Next()
		}
		return err
	}
}

// serveBundled 服務改寫後的 index.html 與虛擬 bundle；編譯失敗時 bundle 請求回 500 並帶錯誤訊息。
func serveBundled(c *fiber.Ctx, res *bundleResult, name string) error {
	if name == "index.html" {
		return sendBody(c, res.index, name, contentType)
	}
	if res.err != nil {
		log.Printf("前端 bundle 編譯失敗：%v", res.err)
		c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
		return c.Status(fiber.StatusInternalServerError).SendString("前端 bundle 編譯失敗：" + res.err.Error())
	}
	return sendBody(c, res.bundle, name, contentType)
}

// Uploads 是聊天截圖的磁碟目錄（media.SaveBase64Image 寫入處），相對於執行時的工作目錄。
// 與嵌入的前端資源分開：不併入 IsPublic，也不嵌入 production 執行檔。
var Uploads fs.FS = os.DirFS("internal/static/uploads")

// UploadsHandler 服務 /uploads/<檔名> 的磁碟檔案；只接受 uploads 目錄內的單一檔名，不列目錄、不路徑逃逸。
// 不存在或非法的路徑回 404，不會交給其他路由（例如 index.html）。
func UploadsHandler(uploads fs.FS) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if m := c.Method(); m != fiber.MethodGet && m != fiber.MethodHead {
			return c.Next()
		}
		rel, ok := strings.CutPrefix(c.Path(), "/uploads/")
		if !ok || !fs.ValidPath(rel) || rel == "." {
			return fiber.ErrNotFound
		}
		found, err := serveFile(c, uploads, rel, uploadContentType)
		if !found {
			return fiber.ErrNotFound
		}
		return err
	}
}

// serveFile 以內容雜湊產生強 ETag；If-None-Match 命中時回 304 不帶內容。
// found 為 false 表示檔案不存在（或是目錄），由呼叫端決定 404 或交給後續路由。
// 每次請求都依當下內容計算，因此開發模式改檔後 ETag 立即變動，不會回舊的 304。
func serveFile(c *fiber.Ctx, fsys fs.FS, name string, ctype func(string) string) (bool, error) {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false, nil
	}
	return true, sendBody(c, body, name, ctype)
}

// sendBody 以內容雜湊產生強 ETag 並送出 body。
func sendBody(c *fiber.Ctx, body []byte, name string, ctype func(string) string) error {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderETag, etag)
	c.Set("X-Content-Type-Options", "nosniff")
	if etagMatches(c.Get(fiber.HeaderIfNoneMatch), etag) {
		return c.SendStatus(fiber.StatusNotModified)
	}
	c.Set(fiber.HeaderContentType, ctype(name))
	return c.Send(body)
}

// etagMatches 依 RFC 9110 做弱比較：支援 "*"、逗號分隔清單，忽略 W/ 前綴。
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	for _, part := range strings.Split(ifNoneMatch, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}

// uploadContentType 只對 media 會寫入的點陣圖格式指定 MIME；其他一律當作二進位下載，避免瀏覽器嗅探執行內容。
func uploadContentType(name string) string {
	switch path.Ext(name) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "application/octet-stream"
}

// publicName 把 URL 路徑轉成資源名；不是公開資源時回傳空字串。
func publicName(urlPath string) string {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" {
		name = "index.html"
	}
	if !IsPublic(name) {
		return ""
	}
	return name
}

// contentType 明確指定前端 MIME：不依賴作業系統登錄檔（Windows 上 .js 可能被登錄成 application/x-javascript）。
func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".ico":
		return "image/x-icon"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	}
	return "application/octet-stream"
}
