package static

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
)

const bundleTestIndex = `<!doctype html>
<html><head>
  <script src="./js/early.js"></script>
  <script src="https://unpkg.com/react@18/umd/react.development.js"></script>
  <script src="https://unpkg.com/react-dom@18/umd/react-dom.development.js"></script>
  <script src="https://unpkg.com/@babel/standalone/babel.min.js"></script>
  <script>Babel.registerPreset('react-classic', {});</script>
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
</head><body>
<script type="text/babel" data-presets="react-classic" src="./js/a.js"></script>
<script src="./js/plain.js"></script>
<script type="text/babel" data-presets="react-classic" src="./js/b.js"></script>
<div id="root"></div>
</body></html>
`

func bundleTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":  {Data: []byte(bundleTestIndex)},
		"js/a.js":     {Data: []byte("const A = () => <div className=\"a\">hi</div>;\n")},
		"js/plain.js": {Data: []byte("var PLAIN = 1;\n")},
		"js/b.js":     {Data: []byte("const B = () => <A />;\n")},
	}
}

func TestScriptFilesOrder(t *testing.T) {
	got := strings.Join(scriptFiles([]byte(bundleTestIndex)), ",")
	if want := "js/a.js,js/plain.js,js/b.js"; got != want {
		t.Fatalf("清單順序錯誤\n got: %s\nwant: %s", got, want)
	}
}

func TestRewriteIndex(t *testing.T) {
	dev := string(rewriteIndex([]byte(bundleTestIndex), false))
	if n := strings.Count(dev, `src="./js/app.bundle.js"`); n != 1 {
		t.Errorf("bundle 標籤數量 = %d，應為 1", n)
	}
	if strings.Index(dev, "./js/early.js") < 0 || strings.Index(dev, "./js/early.js") > strings.Index(dev, "react.development.js") {
		t.Error("head 內的本地腳本必須原樣保留在 React 之前")
	}
	for _, gone := range []string{"babel", "Babel", "./js/a.js", "./js/b.js", "./js/plain.js"} {
		if strings.Contains(dev, gone) {
			t.Errorf("改寫後仍含 %q", gone)
		}
	}
	for _, kept := range []string{"react.development.js", "react-dom.development.js", "marked.min.js", `<div id="root">`} {
		if !strings.Contains(dev, kept) {
			t.Errorf("dev 改寫後應保留 %q", kept)
		}
	}
	if !strings.Contains(dev, `<script defer src="./js/app.bundle.js">`) {
		t.Error("bundle 標籤必須是 defer，確保 #root 已解析")
	}
	if strings.Index(dev, "app.bundle.js") < strings.Index(dev, "marked.min.js") {
		t.Error("bundle 標籤必須在 CDN 腳本之後")
	}

	prod := string(rewriteIndex([]byte(bundleTestIndex), true))
	if !strings.Contains(prod, "/umd/react.production.min.js") || !strings.Contains(prod, "/umd/react-dom.production.min.js") {
		t.Errorf("production 應換成 React 壓縮版\n%s", prod)
	}
	if strings.Contains(prod, "development") {
		t.Error("production 不應再含 development 版本")
	}
}

func TestBundleCompile(t *testing.T) {
	t.Run("dev：JSX 轉 createElement、順序不變、sourcemap 為 sections 格式", func(t *testing.T) {
		res := newBundler(bundleTestFS(), false).result()
		if res.err != nil {
			t.Fatal(res.err)
		}
		code := string(res.bundle)
		if !strings.Contains(code, `React.createElement("div"`) || strings.Contains(code, "<div") {
			t.Errorf("JSX 未轉成 React.createElement\n%s", code)
		}
		if !(strings.Index(code, "const A") < strings.Index(code, "var PLAIN") && strings.Index(code, "var PLAIN") < strings.Index(code, "const B")) {
			t.Errorf("串接順序錯誤\n%s", code)
		}
		const prefix = "//# sourceMappingURL=data:application/json;base64,"
		i := strings.LastIndex(code, prefix)
		if i < 0 {
			t.Fatal("dev bundle 缺少 inline sourcemap")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(code[i+len(prefix):]))
		if err != nil {
			t.Fatal(err)
		}
		var idx sourceMapIndex
		if err := json.Unmarshal(raw, &idx); err != nil {
			t.Fatal(err)
		}
		if idx.Version != 3 || len(idx.Sections) != 3 {
			t.Fatalf("sections 數量 = %d，應為 3", len(idx.Sections))
		}
		wantLine := 0
		for k, f := range []string{"const A", "var PLAIN", "const B"} {
			lineOf := strings.Count(code[:strings.Index(code, f)], "\n")
			if idx.Sections[k].Offset.Line != lineOf {
				t.Errorf("section %d offset = %d，應為 %d", k, idx.Sections[k].Offset.Line, lineOf)
			}
			wantLine = lineOf
		}
		_ = wantLine
		if !strings.Contains(string(idx.Sections[0].Map), "js/a.js") {
			t.Error("section 0 的 sourcemap 應指向 js/a.js")
		}
	})

	t.Run("production：壓縮、無 sourcemap、頂層名稱不改", func(t *testing.T) {
		res := newBundler(bundleTestFS(), true).result()
		if res.err != nil {
			t.Fatal(res.err)
		}
		code := string(res.bundle)
		if strings.Contains(code, "sourceMappingURL") {
			t.Error("production 不應帶 sourcemap")
		}
		for _, name := range []string{"A=", "PLAIN", "B="} {
			if !strings.Contains(code, name) {
				t.Errorf("頂層名稱 %q 被改掉\n%s", name, code)
			}
		}
	})

	t.Run("語法錯誤帶檔名與行號", func(t *testing.T) {
		fsys := bundleTestFS()
		fsys["js/b.js"] = &fstest.MapFile{Data: []byte("const ok = 1;\nconst = ;\n")}
		res := newBundler(fsys, false).result()
		if res.err == nil || !strings.Contains(res.err.Error(), "js/b.js:2") {
			t.Fatalf("應回報 js/b.js:2，實際: %v", res.err)
		}
	})

	t.Run("清單中的檔案不存在", func(t *testing.T) {
		fsys := bundleTestFS()
		delete(fsys, "js/plain.js")
		res := newBundler(fsys, false).result()
		if res.err == nil || !strings.Contains(res.err.Error(), "js/plain.js") {
			t.Fatalf("應回報缺檔 js/plain.js，實際: %v", res.err)
		}
	})

	t.Run("沒有本地腳本時不產生 bundle", func(t *testing.T) {
		fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
		if res := newBundler(fsys, false).result(); len(res.files) != 0 {
			t.Errorf("files = %v，應為空", res.files)
		}
	})
}

func TestBundlerRebuildsOnChange(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", bundleTestIndex)
	write("js/a.js", "var A = 1;\n")
	write("js/plain.js", "var P = 1;\n")
	write("js/b.js", "var B = 1;\n")

	b := newBundler(os.DirFS(dir), false)
	first := b.result()
	if again := b.result(); again != first {
		t.Error("檔案沒變時應回傳快取結果")
	}
	write("js/a.js", "var A = 22222;\n")
	next := b.result()
	if next == first || !strings.Contains(string(next.bundle), "22222") {
		t.Error("檔案變動後應重編")
	}

	prod := newBundler(os.DirFS(dir), true)
	p1 := prod.result()
	write("js/a.js", "var A = 333333;\n")
	if prod.result() != p1 {
		t.Error("production 只編譯一次")
	}
}

func TestHandlerServesBundle(t *testing.T) {
	app := fiber.New()
	app.Use(Handler(bundleTestFS()))

	get := func(path string) (int, string, string) {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
	}

	status, ctype, body := get("/")
	if status != 200 || !strings.HasPrefix(ctype, "text/html") || !strings.Contains(body, "app.bundle.js") || strings.Contains(body, "babel") {
		t.Errorf("index 改寫錯誤 status=%d ctype=%s\n%s", status, ctype, body)
	}
	status, ctype, body = get("/js/app.bundle.js")
	if status != 200 || !strings.HasPrefix(ctype, "text/javascript") || !strings.Contains(body, "createElement") {
		t.Errorf("bundle 服務錯誤 status=%d ctype=%s\n%s", status, ctype, body)
	}
	// 原始檔案仍可單獨取得
	if status, _, body = get("/js/a.js"); status != 200 || !strings.Contains(body, "<div") {
		t.Errorf("原始檔 status=%d body=%s", status, body)
	}

	req := httptest.NewRequest("GET", "/js/app.bundle.js", nil)
	resp, _ := app.Test(req)
	req2 := httptest.NewRequest("GET", "/js/app.bundle.js", nil)
	req2.Header.Set("If-None-Match", resp.Header.Get("ETag"))
	if resp2, _ := app.Test(req2); resp2.StatusCode != 304 {
		t.Errorf("ETag 命中應回 304，實際 %d", resp2.StatusCode)
	}
}

func TestHandlerBundleErrorReturns500(t *testing.T) {
	fsys := bundleTestFS()
	fsys["js/b.js"] = &fstest.MapFile{Data: []byte("const = ;\n")}
	app := fiber.New()
	app.Use(Handler(fsys))
	resp, err := app.Test(httptest.NewRequest("GET", "/js/app.bundle.js", nil))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || !strings.Contains(string(b), "js/b.js:1") {
		t.Errorf("status=%d body=%s", resp.StatusCode, b)
	}
}

// 真實的 index.html 與 js/ 必須能編譯，且不得殘留 Babel 的 text/babel 標籤。
func TestRealFrontendBundles(t *testing.T) {
	res := newBundler(Assets, Production).result()
	if len(res.files) == 0 {
		t.Skip("Assets 內的 index.html 沒有本地腳本")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	if strings.Contains(string(res.index), "text/babel") || strings.Contains(string(res.index), "Babel") {
		t.Error("改寫後的 index.html 仍含 Babel")
	}
	if len(res.bundle) == 0 {
		t.Error("bundle 為空")
	}
}
