package static

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// bundleName 是虛擬 bundle 的資源名；內容由 index.html 的本地 <script> 清單在記憶體編譯，磁碟與嵌入集合中沒有這個檔案。
const bundleName = "js/app.bundle.js"

var (
	// 本地腳本標籤：src 為 ./js/ 下的相對路徑。CDN 腳本與內嵌腳本不符合。
	localScriptRe = regexp.MustCompile(`(?m)^[ \t]*<script\b[^>]*\bsrc="\./(js/[^"]+)"[^>]*></script>[ \t]*\r?\n?`)
	babelCDNRe    = regexp.MustCompile(`(?m)^[ \t]*<script src="https://unpkg\.com/@babel/standalone/[^"]*"></script>[ \t]*\r?\n?`)
	babelPresetRe = regexp.MustCompile(`(?m)^[ \t]*<script>Babel\.registerPreset\([^\n]*</script>[ \t]*\r?\n?`)
)

// bundler 把 index.html 的 body 內列出的本地腳本依序編成單一全域 script。
// 每個檔案各自編譯後串接（不包 module），頂層宣告與載入順序維持原本多個 <script> 的語意。
// production 建置只編譯一次；開發建置每次請求比對 index.html 與各檔案的 mtime／大小，有變才重編。
type bundler struct {
	fsys       fs.FS
	production bool

	mu    sync.Mutex
	sig   string
	built *bundleResult
}

// bundleResult 是一次編譯的完整輸出。files 為空表示 index.html 沒有本地腳本，不需要 bundle。
type bundleResult struct {
	files  []string
	index  []byte
	bundle []byte
	err    error
}

func newBundler(fsys fs.FS, production bool) *bundler {
	return &bundler{fsys: fsys, production: production}
}

// IndexHTML 讓分享頁與主頁使用相同的 bundle，避免重新引入瀏覽器 Babel。
func IndexHTML(fsys fs.FS) ([]byte, error) {
	html, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, err
	}
	if len(scriptFiles(html)) > 0 {
		html = rewriteIndex(html, Production)
	}
	return html, nil
}

// result 回傳目前有效的編譯結果；沒有本地腳本時 files 為空。
func (b *bundler) result() *bundleResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.production && b.built != nil {
		return b.built
	}
	html, err := fs.ReadFile(b.fsys, "index.html")
	if err != nil {
		return &bundleResult{}
	}
	files := scriptFiles(html)
	if len(files) == 0 {
		return &bundleResult{}
	}
	sig := b.signature(html, files)
	if b.built != nil && b.sig == sig {
		return b.built
	}
	b.built = b.build(html, files)
	b.sig = sig
	return b.built
}

// splitBody 把 index.html 切成 <body 之前（head）與其後兩段。只有 body 內的本地腳本會被合併；
// head 內的本地腳本維持原樣，會在 React 載入前同步執行。找不到 <body 時 body 為空。
func splitBody(html []byte) (head, body []byte) {
	i := bytes.Index(html, []byte("<body"))
	if i < 0 {
		return html, nil
	}
	return html[:i], html[i:]
}

// scriptFiles 依出現順序回傳 index.html 的 body 內本地腳本的資源名。
func scriptFiles(html []byte) []string {
	var files []string
	_, body := splitBody(html)
	for _, m := range localScriptRe.FindAllSubmatch(body, -1) {
		files = append(files, string(m[1]))
	}
	return files
}

func (b *bundler) signature(html []byte, files []string) string {
	h := sha256.New()
	h.Write(html)
	for _, f := range files {
		if info, err := fs.Stat(b.fsys, f); err == nil {
			fmt.Fprintf(h, "|%s:%d:%d", f, info.Size(), info.ModTime().UnixNano())
		} else {
			fmt.Fprintf(h, "|%s:missing", f)
		}
	}
	return string(h.Sum(nil))
}

func (b *bundler) build(html []byte, files []string) *bundleResult {
	res := &bundleResult{files: files, index: rewriteIndex(html, b.production)}
	res.bundle, res.err = b.compile(files)
	return res
}

// rewriteIndex 把 body 內的本地腳本標籤合併成一個 defer 的 bundle 標籤（維持 Babel 時代「頁面解析完才執行」的時序）並移除 Babel；production 另把 React 換成壓縮版。
func rewriteIndex(html []byte, production bool) []byte {
	head, body := splitBody(html)
	replaced := false
	body = localScriptRe.ReplaceAllFunc(body, func(m []byte) []byte {
		if replaced {
			return nil
		}
		replaced = true
		indent := m[:len(m)-len(bytes.TrimLeft(m, " \t"))]
		return append(append(append([]byte{}, indent...), []byte(`<script defer src="./`+bundleName+`"></script>`)...), '\n')
	})
	out := append(append([]byte{}, head...), body...)
	out = babelCDNRe.ReplaceAll(out, nil)
	out = babelPresetRe.ReplaceAll(out, nil)
	if production {
		out = bytes.ReplaceAll(out, []byte("/umd/react.development.js"), []byte("/umd/react.production.min.js"))
		out = bytes.ReplaceAll(out, []byte("/umd/react-dom.development.js"), []byte("/umd/react-dom.production.min.js"))
	}
	return out
}

// compile 逐檔把 JSX 轉成 React.createElement 呼叫（等同 Babel react-classic），再依序串接。
// 開發建置附 sections 格式的 inline sourcemap，production 壓縮且不附 sourcemap。
func (b *bundler) compile(files []string) ([]byte, error) {
	var out bytes.Buffer
	var sections []sourceMapSection
	line := 0
	for _, name := range files {
		src, err := fs.ReadFile(b.fsys, name)
		if err != nil {
			return nil, fmt.Errorf("讀取 %s 失敗: %w", name, err)
		}
		opts := api.TransformOptions{
			Loader:     api.LoaderJSX,
			Sourcefile: name,
			Target:     api.ESNext,
			LogLevel:   api.LogLevelSilent,
		}
		if b.production {
			opts.MinifyWhitespace = true
			opts.MinifySyntax = true
			opts.LegalComments = api.LegalCommentsNone
		} else {
			opts.Sourcemap = api.SourceMapExternal
			opts.SourcesContent = api.SourcesContentInclude
		}
		r := api.Transform(string(src), opts)
		if len(r.Errors) > 0 {
			return nil, errors.New(formatMessages(r.Errors))
		}
		code := r.Code
		if !bytes.HasSuffix(code, []byte("\n")) {
			code = append(code, '\n')
		}
		if !b.production {
			sections = append(sections, sourceMapSection{Offset: sectionOffset{Line: line}, Map: json.RawMessage(r.Map)})
		}
		out.Write(code)
		line += bytes.Count(code, []byte("\n"))
	}
	if !b.production {
		mapJSON, err := json.Marshal(sourceMapIndex{Version: 3, Sections: sections})
		if err != nil {
			return nil, err
		}
		out.WriteString("//# sourceMappingURL=data:application/json;base64,")
		out.WriteString(base64.StdEncoding.EncodeToString(mapJSON))
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

type sourceMapIndex struct {
	Version  int                `json:"version"`
	Sections []sourceMapSection `json:"sections"`
}

type sourceMapSection struct {
	Offset sectionOffset   `json:"offset"`
	Map    json.RawMessage `json:"map"`
}

type sectionOffset struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func formatMessages(msgs []api.Message) string {
	lines := api.FormatMessages(msgs, api.FormatMessagesOptions{Kind: api.ErrorMessage})
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
