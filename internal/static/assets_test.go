package static

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestStaticAssetModes 同時驗證 production（-tags production）與開發模式的資源來源：
// 資源集合、production 不被磁碟副本遮蔽、開發檔案變動可立即讀取、以及路徑邊界與 MIME。
func TestStaticAssetModes(t *testing.T) {
	t.Run("資源集合只含公開前端檔", func(t *testing.T) {
		pkgDir := mustGetwd(t)
		enterFixtureRoot(t)
		if Production {
			// production 的資源是編譯期嵌入；對照套件目錄的真實檔案，嵌入集合必須只有公開前端。
			pkgFS := mustDirFS(t, pkgDir)
			embedded := walkFiles(t, Assets)
			for _, name := range embedded {
				if !IsPublic(name) {
					t.Errorf("非公開檔案被嵌入：%s", name)
				}
			}
			want := publicOnly(walkFiles(t, pkgFS))
			if got := publicOnly(embedded); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("嵌入的公開資源與套件目錄不一致\n got: %v\nwant: %v", got, want)
			}
		} else {
			// 開發模式讀磁碟：固定 fixture 內含 uploads、AGENT.md 等非公開檔，只有公開集合可被服務。
			got := publicOnly(walkFiles(t, Assets))
			want := []string{"favicon.ico", "index.html", "js/app.js", "js/core/core.js"}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("開發模式公開資源集合錯誤\n got: %v\nwant: %v", got, want)
			}
		}
		for _, must := range []string{"index.html", "favicon.ico", "js/app.js"} {
			if !IsPublic(must) {
				t.Errorf("IsPublic(%q) = false，應為公開資源", must)
			}
		}
		for _, notPublic := range []string{"AGENT.md", "config.yaml", "uploads/a.png", "static.go", "js/../config.yaml", "/index.html", ""} {
			if IsPublic(notPublic) {
				t.Errorf("IsPublic(%q) = true，應拒絕", notPublic)
			}
		}
	})

	t.Run("production 不被舊磁碟副本遮蔽；開發檔案變動即時可讀", func(t *testing.T) {
		pkgDir := mustGetwd(t)
		root := enterFixtureRoot(t)
		app := newAssetApp()

		if Production {
			want, err := os.ReadFile(filepath.Join(pkgDir, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			got := get(t, app, "/")
			// 首頁會被改寫（本地腳本合併為 bundle），因此與嵌入版 index.html 的改寫結果比對。
			want = rewriteIndex(want, true)
			if got.body != string(want) {
				t.Fatalf("production 首頁應為嵌入版本，卻被磁碟副本取代（body 長度 %d，嵌入版 %d）", len(got.body), len(want))
			}
			if strings.Contains(got.body, "STALE") {
				t.Fatal("production 讀到舊磁碟副本")
			}
			return
		}

		// 開發模式：改磁碟上的 JS，不重編譯就能在下一次請求看到。
		mustWrite(t, filepath.Join(root, "internal", "static", "js", "app.js"), "window.v = 'v2';")
		got := get(t, app, "/js/app.js")
		if got.status != fiber.StatusOK || !strings.Contains(got.body, "v2") {
			t.Fatalf("開發模式應立即讀到新的 app.js，得到 status=%d body=%q", got.status, got.body)
		}
	})

	t.Run("服務行為：公開檔、已知與未知 API、路徑逃逸、MIME", func(t *testing.T) {
		enterFixtureRoot(t)
		app := newAssetApp()

		cases := []struct {
			name        string
			path        string
			status      int
			ctypePrefix string
			bodyHas     string
			bodyNotHas  string
		}{
			{name: "首頁", path: "/", status: 200, ctypePrefix: "text/html", bodyHas: "<html"},
			{name: "index.html", path: "/index.html", status: 200, ctypePrefix: "text/html", bodyHas: "<html"},
			{name: "favicon", path: "/favicon.ico", status: 200, ctypePrefix: "image/x-icon"},
			{name: "JS MIME", path: "/js/app.js", status: 200, ctypePrefix: "text/javascript"},
			{name: "已知 API 不受影響", path: "/sessions", status: 200, ctypePrefix: "application/json", bodyHas: `"api":"sessions"`},
			{name: "未知 API 不得回 HTML 成功", path: "/api/unknown", status: 404, bodyNotHas: "<html"},
			{name: "非公開開發文件", path: "/AGENT.md", status: 404, bodyNotHas: "SECRET_AGENT"},
			{name: "上傳截圖（磁碟，/uploads）", path: "/uploads/a.png", status: 200, ctypePrefix: "image/png"},
			{name: "路徑逃逸 ..", path: "/js/../../config.yaml", status: 404, bodyNotHas: "SECRET_CONFIG"},
			{name: "路徑逃逸 百分比編碼", path: "/js/..%2F..%2Fconfig.yaml", status: 404, bodyNotHas: "SECRET_CONFIG"},
			{name: "路徑逃逸 根目錄", path: "/%2e%2e/config.yaml", status: 404, bodyNotHas: "SECRET_CONFIG"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := get(t, app, tc.path)
				if got.status != tc.status {
					t.Fatalf("GET %s status = %d，want %d（body=%q）", tc.path, got.status, tc.status, got.body)
				}
				if tc.ctypePrefix != "" && !strings.HasPrefix(got.ctype, tc.ctypePrefix) {
					t.Errorf("GET %s Content-Type = %q，want 前綴 %q", tc.path, got.ctype, tc.ctypePrefix)
				}
				if tc.bodyHas != "" && !strings.Contains(got.body, tc.bodyHas) {
					t.Errorf("GET %s body 缺少 %q：%q", tc.path, tc.bodyHas, got.body)
				}
				if tc.bodyNotHas != "" && strings.Contains(got.body, tc.bodyNotHas) {
					t.Errorf("GET %s 洩漏了不該回傳的內容 %q", tc.path, tc.bodyNotHas)
				}
				if tc.status == 404 && strings.HasPrefix(got.ctype, "text/html") {
					t.Errorf("GET %s 404 卻是 text/html", tc.path)
				}
			})
		}
	})
}

// enterFixtureRoot 在暫存目錄建立假的專案根（含 internal/static 與旁路的敏感檔），並切換工作目錄過去。
// production 模式不會讀磁碟，應忽略這些副本；開發模式則讀此處的檔案。
func enterFixtureRoot(t *testing.T) string {
	t.Helper()
	wd := mustGetwd(t)
	root := t.TempDir()
	static := filepath.Join(root, "internal", "static")
	mustWrite(t, filepath.Join(static, "index.html"), "<!doctype html><html><head></head><body>STALE disk index</body></html>")
	mustWrite(t, filepath.Join(static, "favicon.ico"), "ico")
	mustWrite(t, filepath.Join(static, "js", "app.js"), "window.v = 'v1';")
	mustWrite(t, filepath.Join(static, "js", "core", "core.js"), "window.core = 1;")
	mustWrite(t, filepath.Join(static, "AGENT.md"), "SECRET_AGENT")
	mustWrite(t, filepath.Join(static, "uploads", "a.png"), "png")
	mustWrite(t, filepath.Join(static, "config.yaml"), "SECRET_CONFIG")
	mustWrite(t, filepath.Join(root, "config.yaml"), "SECRET_CONFIG")
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return root
}

type response struct {
	status       int
	ctype        string
	etag         string
	cacheControl string
	body         string
}

// newAssetApp 與 server.go 的掛法一致：先公開白名單，再 /uploads 磁碟服務，最後才是 API。
func newAssetApp() *fiber.App {
	app := fiber.New()
	app.Use(Handler(Assets))
	app.Use("/uploads", UploadsHandler(Uploads))
	app.Get("/sessions", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"api": "sessions"})
	})
	return app
}

func get(t *testing.T, app *fiber.App, path string) response {
	t.Helper()
	return request(t, app, path, nil)
}

func request(t *testing.T, app *fiber.App, path string, headers map[string]string) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{
		status:       res.StatusCode,
		ctype:        res.Header.Get("Content-Type"),
		etag:         res.Header.Get("ETag"),
		cacheControl: res.Header.Get("Cache-Control"),
		body:         string(b),
	}
}

// TestConditionalGET 確認公開資源帶 ETag、If-None-Match 得 304，且內容變動後 ETag 會跟著變。
// 開發模式改檔後不得回舊的 304；production 則固定使用嵌入內容，磁碟改動不影響。
func TestConditionalGET(t *testing.T) {
	root := enterFixtureRoot(t)
	app := newAssetApp()

	first := get(t, app, "/js/app.js")
	if first.status != fiber.StatusOK {
		t.Fatalf("首次 GET status = %d，want 200", first.status)
	}
	if !strings.HasPrefix(first.etag, `"`) || !strings.HasSuffix(first.etag, `"`) || len(first.etag) < 4 {
		t.Fatalf("首次回應 ETag = %q，want 加引號的強 ETag", first.etag)
	}
	if first.cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %q，want no-cache", first.cacheControl)
	}

	t.Run("If-None-Match 命中回 304 且無內容", func(t *testing.T) {
		for _, inm := range []string{first.etag, "W/" + first.etag, "*", `"other", ` + first.etag} {
			got := request(t, app, "/js/app.js", map[string]string{"If-None-Match": inm})
			if got.status != fiber.StatusNotModified {
				t.Errorf("If-None-Match %q: status = %d，want 304", inm, got.status)
			}
			if got.body != "" {
				t.Errorf("If-None-Match %q: 304 不應帶內容，得到 %d bytes", inm, len(got.body))
			}
			if got.etag != first.etag {
				t.Errorf("If-None-Match %q: 304 的 ETag = %q，want %q", inm, got.etag, first.etag)
			}
		}
	})

	t.Run("不相符的 ETag 回 200", func(t *testing.T) {
		got := request(t, app, "/js/app.js", map[string]string{"If-None-Match": `"deadbeef"`})
		if got.status != fiber.StatusOK || got.body == "" {
			t.Fatalf("status = %d body 長度 %d，want 200 有內容", got.status, len(got.body))
		}
	})

	t.Run("內容改變後 ETag 變，舊 ETag 不再得 304", func(t *testing.T) {
		if Production {
			// 嵌入內容不隨磁碟變化：ETag 穩定，舊 ETag 仍得 304。
			again := get(t, app, "/js/app.js")
			if again.etag != first.etag {
				t.Fatalf("production ETag 不應變化：%q → %q", first.etag, again.etag)
			}
			if got := request(t, app, "/js/app.js", map[string]string{"If-None-Match": first.etag}); got.status != fiber.StatusNotModified {
				t.Fatalf("production 舊 ETag status = %d，want 304", got.status)
			}
			return
		}
		mustWrite(t, filepath.Join(root, "internal", "static", "js", "app.js"), "window.v = 'changed';")
		got := request(t, app, "/js/app.js", map[string]string{"If-None-Match": first.etag})
		if got.status != fiber.StatusOK {
			t.Fatalf("開發模式改檔後舊 ETag status = %d，want 200（不得回舊的 304）", got.status)
		}
		if !strings.Contains(got.body, "changed") {
			t.Fatalf("開發模式應回新內容，得到 %q", got.body)
		}
		if got.etag == first.etag || got.etag == "" {
			t.Fatalf("內容改變後 ETag 應改變：舊 %q 新 %q", first.etag, got.etag)
		}
	})
}

// TestUploadsDiskServing 確認 /uploads 由執行時 cwd 的 internal/static/uploads 提供（開發與 production 相同），
// 只服務該目錄內的檔案：不列目錄、不路徑逃逸、其他非白名單檔維持 404。
func TestUploadsDiskServing(t *testing.T) {
	root := enterFixtureRoot(t)
	pngBytes := "\x89PNG\r\n\x1a\nfixture-image-bytes"
	mustWrite(t, filepath.Join(root, "internal", "static", "uploads", "x.png"), pngBytes)
	app := newAssetApp()

	t.Run("上傳截圖 200 且 MIME 為 image/png、位元組一致", func(t *testing.T) {
		got := get(t, app, "/uploads/x.png")
		if got.status != fiber.StatusOK {
			t.Fatalf("status = %d，want 200", got.status)
		}
		if got.ctype != "image/png" {
			t.Errorf("Content-Type = %q，want image/png", got.ctype)
		}
		if got.body != pngBytes {
			t.Errorf("位元組不一致：got %q", got.body)
		}
		if got.etag == "" {
			t.Error("上傳截圖應帶 ETag")
		}
	})

	t.Run("列目錄與空路徑為 404", func(t *testing.T) {
		for _, p := range []string{"/uploads", "/uploads/", "/uploads/sub/"} {
			got := get(t, app, p)
			if got.status != fiber.StatusNotFound {
				t.Errorf("GET %s status = %d，want 404", p, got.status)
			}
		}
	})

	// 路徑逃逸：不得回傳 uploads 以外的非白名單檔；index.html 本身是公開檔，允許 200。
	escapes := []string{
		"/uploads/../AGENT.md",
		"/uploads/%2e%2e/AGENT.md",
		"/uploads/../../config.yaml",
		"/uploads/..%2f..%2fconfig.yaml",
		"/uploads/%2e%2e/%2e%2e/config.yaml",
		"/uploads/../index.html",
		"/uploads/%2e%2e/index.html",
	}
	for _, p := range escapes {
		t.Run("路徑逃逸 "+p, func(t *testing.T) {
			got := get(t, app, p)
			if strings.Contains(got.body, "SECRET_") {
				t.Fatalf("洩漏非白名單檔案內容：%q", got.body)
			}
			if got.status == fiber.StatusOK && !strings.HasPrefix(got.ctype, "text/html") {
				t.Fatalf("回傳了非公開頁面（status 200、Content-Type %q）", got.ctype)
			}
		})
	}

	t.Run("非白名單檔仍 404", func(t *testing.T) {
		for _, p := range []string{"/AGENT.md", "/config.yaml", "/uploads/../config.yaml"} {
			if got := get(t, app, p); got.status != fiber.StatusNotFound {
				t.Errorf("GET %s status = %d，want 404", p, got.status)
			}
		}
	})
}

// walkFiles 列出 fsys 內所有檔案（不含目錄），已排序。
func walkFiles(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func publicOnly(names []string) []string {
	var out []string
	for _, n := range names {
		if IsPublic(n) {
			out = append(out, n)
		}
	}
	return out
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func mustDirFS(t *testing.T, dir string) fs.FS {
	t.Helper()
	return os.DirFS(dir)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
