// 後端分層規範的自動檢查，規則定義見 docs/spec/backend-architecture.md
// 執行：go test ./internal/arch/
//
// 只檢查 internal/ 與 cmd/ 的非測試檔；poc/ 為拋棄式探針，不受規範約束。
package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath   = "github.com/jerry12122/Claude-Code-Mini-App/"
	maxFileLines = 600 // 單檔上限（非測試檔）
	maxFuncLines = 150 // 單一函式上限（非測試檔）
	needTestOver = 100 // 超過此行數的套件必須有測試檔
)

// layerOf 是每個套件的層號。依賴只能指向嚴格更低的層。
// 新增套件必須在此登記，否則 R1 失敗——這是刻意的，用來強迫決定新套件屬於哪一層。
var layerOf = map[string]int{
	// L0 基礎：不得 import 任何其他內部套件
	"internal/agent": 0, "internal/auth": 0, "internal/config": 0, "internal/db": 0,
	"internal/desktopupdate": 0, "internal/logging": 0, "internal/media": 0,
	"internal/proc": 0, "internal/static": 0, "internal/tg": 0, "internal/usage": 0,
	"internal/version": 0,

	// L1 領域工具：解析、計算、外部程序查詢
	"internal/gitinfo": 1, "internal/model": 1, "internal/opener": 1,
	"internal/quota": 1, "internal/shell": 1, "internal/sysmon": 1,

	// L2 Agent runner：實作 agent.Runner 並以 agent.Register 註冊
	"internal/antigravity": 2, "internal/claude": 2, "internal/codex": 2,
	"internal/cursor": 2, "internal/kiro": 2, "internal/kiroacp": 2,

	// L3 runner 探測
	"internal/clicheck": 3,

	// L4 回合編排與 WebSocket 傳輸
	"internal/ws": 4,

	// L5 其他對外介面（建立在 ws 之上）
	"internal/mcp": 5, "internal/tgclient": 5,

	// L6 HTTP handler
	"internal/api": 6,

	// L7 組裝與路由
	"internal/server": 7,

	// L8 進入點
	"cmd/desktop": 8, "cmd/server": 8,

	// 本檢查自身，無非測試原始碼
	"internal/arch": 0,
}

// execAllowed 是允許 spawn 子進程的套件。
var execAllowed = map[string]bool{
	"internal/proc": true, "internal/shell": true, "internal/gitinfo": true,
	"internal/clicheck": true, "internal/quota": true, "internal/desktopupdate": true,
	"internal/opener":      true,
	"internal/antigravity": true, "internal/claude": true, "internal/codex": true,
	"internal/cursor": true, "internal/kiro": true, "internal/kiroacp": true,
}

// knownViolations 是既存違規白名單。新增程式碼不得寫進來；處理掉一項就刪一項。
var knownViolations = map[string][]string{
	// runner 套件反向依賴傳輸層（tgclient 同時是 runner 與 Telegram 同步器）
	"R2_runner_ws": {"internal/tgclient"},
	// 直接開 SQLite 連線（讀 Cursor 自己的 state.vscdb 取 token）
	"R3_sql_open": {"internal/quota"},
	// 用 stdlib log 而非 slog
	"R5_stdlib_log": {},
	// 非測試碼 panic（logging 初始化失敗）
	"R6_panic": {"internal/logging"},
	// 超過單檔上限
	"R7_file": {"internal/ws/handler.go", "internal/mcp/tools.go", "internal/server/server.go"},
	// 超過單一函式上限
	"R8_func": {
		"internal/ws/handler.go:NewHandler",         // 1524 行，拆解為首要目標
		"internal/server/server.go:runNormal",       // 450 行，內嵌路由 handler 應移往 api
		"internal/kiroacp/runner.go:Runner.runOnce", // 313 行
		"cmd/desktop/main_windows.go:main",          // 185 行，視窗與托盤組裝
		"internal/db/db.go:DB.migrate",              // 164 行，schema 清單，會隨欄位增長
	},
}

type goFile struct {
	pkg     string // 相對模組根的套件路徑，例如 internal/ws
	rel     string // 相對模組根的檔案路徑，例如 internal/ws/handler.go
	lines   int
	imports []string
	ast     *ast.File
	fset    *token.FileSet
}

func TestBackendArchitecture(t *testing.T) {
	root := repoRoot(t)
	files, tested := collect(t, root)

	var fail []string
	check := func(ok bool, format string, args ...any) {
		if !ok {
			fail = append(fail, fmt.Sprintf(format, args...))
		}
	}
	allowed := func(rule, key string) bool {
		for _, v := range knownViolations[rule] {
			if v == key {
				return true
			}
		}
		return false
	}

	pkgLines := map[string]int{}
	for _, f := range files {
		pkgLines[f.pkg] += f.lines
	}

	// R1 分層：每個套件都要登記層號，且 import 只能指向嚴格更低的層
	for pkg := range pkgLines {
		if _, ok := layerOf[pkg]; !ok {
			check(false, "R1 套件 %s 未在 layerOf 登記層號", pkg)
		}
	}
	for _, f := range files {
		from, ok := layerOf[f.pkg]
		if !ok {
			continue
		}
		for _, imp := range f.imports {
			if strings.HasPrefix(imp, "ext:") {
				continue // 標準庫與第三方不參與分層
			}
			to, ok := layerOf[imp]
			if !ok {
				check(false, "R1 %s import 未登記層號的套件 %s", f.rel, imp)
				continue
			}
			check(to < from, "R1 %s（L%d）import %s（L%d）：依賴必須指向更低的層", f.rel, from, imp, to)
		}
	}

	for _, f := range files {
		// R2 runner 套件不得依賴傳輸層
		if f.registersRunner() {
			for _, imp := range f.imports {
				if imp == "internal/ws" {
					check(allowed("R2_runner_ws", f.pkg), "R2 %s 註冊 runner 卻 import internal/ws", f.rel)
				}
			}
		}
		// R3 只有 internal/db 能開 SQLite 連線
		if f.pkg != "internal/db" {
			if f.calls("sql", "Open") || f.importsAny("ext:modernc.org/sqlite") {
				check(allowed("R3_sql_open", f.pkg), "R3 %s 自行開 SQL 連線，資料存取應收在 internal/db", f.rel)
			}
		}
		// R4 只有允許清單的套件能 spawn 子進程
		if !execAllowed[f.pkg] && (f.calls("exec", "Command") || f.calls("exec", "CommandContext")) {
			check(allowed("R4_exec", f.pkg), "R4 %s spawn 子進程，但不在 execAllowed 清單", f.rel)
		}
		// R5 用 slog，不用 stdlib log
		if f.importsAny("ext:log") {
			check(allowed("R5_stdlib_log", f.pkg), "R5 %s import stdlib log，應改用 log/slog", f.rel)
		}
		// R6 非測試碼不得 panic
		if f.hasPanic() {
			check(allowed("R6_panic", f.pkg), "R6 %s 在非測試碼 panic，應回傳 error", f.rel)
		}
		// R7 單檔行數
		if f.lines > maxFileLines {
			check(allowed("R7_file", f.rel), "R7 %s %d 行，超過 %d 行上限", f.rel, f.lines, maxFileLines)
		}
		// R8 單一函式行數
		for name, n := range f.funcLines() {
			if n > maxFuncLines {
				key := f.rel + ":" + name
				check(allowed("R8_func", key), "R8 %s %d 行，超過 %d 行上限", key, n, maxFuncLines)
			}
		}
		// R10 檔名全小寫、不含連字號
		base := filepath.Base(f.rel)
		check(base == strings.ToLower(base) && !strings.Contains(base, "-"),
			"R10 %s 檔名需全小寫且不含連字號", f.rel)
	}

	// R9 有份量的套件必須有測試檔
	for pkg, n := range pkgLines {
		if n > needTestOver && !tested[pkg] {
			check(false, "R9 套件 %s 有 %d 行非測試碼卻沒有測試檔", pkg, n)
		}
	}

	// R11 白名單不得殘留死條目
	byRel := map[string]bool{}
	byPkg := map[string]bool{}
	funcKeys := map[string]bool{}
	for _, f := range files {
		byRel[f.rel] = true
		byPkg[f.pkg] = true
		for name := range f.funcLines() {
			funcKeys[f.rel+":"+name] = true
		}
	}
	for rule, list := range knownViolations {
		for _, v := range list {
			switch rule {
			case "R7_file":
				check(byRel[v], "%s 白名單中的 %s 不存在，請清除", rule, v)
			case "R8_func":
				check(funcKeys[v], "%s 白名單中的 %s 不存在，請清除", rule, v)
			default:
				check(byPkg[v], "%s 白名單中的 %s 不存在，請清除", rule, v)
			}
		}
	}

	sort.Strings(fail)
	if len(fail) > 0 {
		t.Fatalf("後端分層檢查失敗：\n  %s\n", strings.Join(fail, "\n  "))
	}
	t.Logf("後端分層檢查通過：%d 支非測試檔、%d 個套件，既存例外 %d 項",
		len(files), len(pkgLines), countViolations())
}

func countViolations() int {
	n := 0
	for _, list := range knownViolations {
		n += len(list)
	}
	return n
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("找不到模組根目錄：%v", err)
	}
	return root
}

func collect(t *testing.T, root string) ([]*goFile, map[string]bool) {
	t.Helper()
	var files []*goFile
	tested := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			pkg := filepath.ToSlash(filepath.Dir(rel))
			if strings.HasSuffix(p, "_test.go") {
				tested[pkg] = true
				return nil
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			fset := token.NewFileSet()
			parsed, perr := parser.ParseFile(fset, p, src, parser.ParseComments)
			if perr != nil {
				t.Fatalf("解析 %s 失敗：%v", rel, perr)
			}
			var imports []string
			for _, spec := range parsed.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if trimmed, ok := strings.CutPrefix(path, modulePath); ok {
					imports = append(imports, trimmed) // 模組內部依賴
				} else {
					imports = append(imports, "ext:"+path) // 標準庫與第三方
				}
			}
			files = append(files, &goFile{
				pkg: pkg, rel: rel, lines: strings.Count(string(src), "\n") + 1,
				imports: imports, ast: parsed, fset: fset,
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files, tested
}

func (f *goFile) importsAny(path string) bool {
	for _, imp := range f.imports {
		if imp == path {
			return true
		}
	}
	return false
}

// registersRunner 判斷套件是否以 agent.Register 註冊 runner。
func (f *goFile) registersRunner() bool { return f.calls("agent", "Register") }

// calls 比對 pkg.Fn(...) 形式的呼叫；走 AST，不會被註解或字串誤判。
func (f *goFile) calls(pkg, fn string) bool {
	found := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != fn {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == pkg {
			found = true
			return false
		}
		return true
	})
	return found
}

func (f *goFile) hasPanic() bool {
	found := false
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
			found = true
			return false
		}
		return true
	})
	return found
}

// funcLines 回傳每個頂層函式／方法的行數，key 為方法接收者前綴後的名稱。
func (f *goFile) funcLines() map[string]int {
	out := map[string]int{}
	for _, decl := range f.ast.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			name = recvName(fd.Recv.List[0].Type) + "." + name
		}
		out[name] = f.fset.Position(fd.Body.End()).Line - f.fset.Position(fd.Pos()).Line + 1
	}
	return out
}

func recvName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	}
	return "?"
}
