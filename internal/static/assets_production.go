//go:build production

package static

import (
	"embed"
	"io/fs"
)

// Production 表示資源是否為編譯期嵌入（-tags production）。
const Production = true

// embedded 只嵌入公開前端；不含 AGENT.md、uploads/、設定、DB、日誌或 Go 原始碼。
//
//go:embed index.html favicon.ico robots.txt manifest.webmanifest sw.js js
var embedded embed.FS

// Assets 是前端公開資源來源：production 只來自執行檔內嵌的資源，不讀磁碟，因此舊的 internal/static 目錄不會遮蔽它。
var Assets fs.FS = embedded
