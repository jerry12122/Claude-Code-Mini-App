package fsbrowse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRoot 建出一個 work_dir，旁邊另有一個「外部」目錄放不該被讀到的 secret。
func newRoot(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "work")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{root, outside, filepath.Join(root, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(outside, "secret.txt"), "TOP-SECRET")
	write(t, filepath.Join(root, "hello.txt"), "hello")
	write(t, filepath.Join(root, "sub", "a.txt"), "in-sub")
	return root, outside
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(l Listing) []string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	return out
}

// 這些輸入在所有平台都必須被拒絕；解析前就擋，不依賴檔案是否存在。
var invalidPaths = []string{
	"..", "../x", "a/../../x", "a/..", "sub/../hello.txt",
	"/abs", "/etc/passwd", "//server/share",
	"C:", "C:/Windows", `C:\Windows`, `c:foo`,
	`..\x`, `a\b`, `\\server\share`, `\abs`,
	"a\x00b", "\x00",
	"hello.txt:stream", "hello.txt::$DATA", "sub:x/a.txt",
	"a\nb", "a\tb",
}

func TestInvalidPathsRejected(t *testing.T) {
	root, _ := newRoot(t)
	for _, p := range invalidPaths {
		if _, err := List(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("List(%q) err=%v，預期 ErrInvalidPath", p, err)
		}
		if _, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ReadText(%q) err=%v，預期 ErrInvalidPath", p, err)
		}
	}
	long := strings.Repeat("a/", 3000)
	if _, err := List(root, long); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("過長路徑 err=%v", err)
	}
}

// URL 編碼只在 HTTP 層解一次；這一層看到 %2e%2e 就是字面檔名，不得再解碼。
func TestEncodedDotsAreLiteralNotTraversal(t *testing.T) {
	root, _ := newRoot(t)
	for _, p := range []string{"%2e%2e", "%2e%2e%2fsecret.txt", "%252e%252e%252fsecret.txt", "..%2fsecret.txt"} {
		c, err := ReadText(root, p)
		if err == nil || strings.Contains(c.Content, "TOP-SECRET") {
			t.Errorf("ReadText(%q) 不該成功: %+v", p, c)
		}
	}
}

func TestListBasics(t *testing.T) {
	root, _ := newRoot(t)
	write(t, filepath.Join(root, "B.txt"), "b")
	write(t, filepath.Join(root, "a.txt"), "a")
	if err := os.Mkdir(filepath.Join(root, "Zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", ".", "./"} {
		l, err := List(root, p)
		if err != nil {
			t.Fatalf("List(%q): %v", p, err)
		}
		// 目錄在前，各組內依名稱不分大小寫排序
		if got := strings.Join(names(l), ","); got != "sub,Zdir,a.txt,B.txt,hello.txt" {
			t.Fatalf("順序不對: %s", got)
		}
		if l.Path != "" || l.Truncated {
			t.Fatalf("root listing: %+v", l)
		}
	}
	l, err := List(root, "sub/")
	if err != nil || len(l.Entries) != 1 || l.Entries[0].Name != "a.txt" || l.Entries[0].IsDir || l.Entries[0].Size != 6 || l.Entries[0].MTime.IsZero() {
		t.Fatalf("sub: %+v err=%v", l, err)
	}
	if l.Path != "sub" {
		t.Fatalf("Path 應回正規化的相對路徑，got %q", l.Path)
	}
	if _, err := List(root, "hello.txt"); !errors.Is(err, ErrNotDir) {
		t.Fatalf("列檔案應 ErrNotDir，got %v", err)
	}
	if _, err := List(root, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在應 ErrNotFound，got %v", err)
	}
}

func TestListTruncatesAt500(t *testing.T) {
	root := t.TempDir()
	for _, n := range []int{MaxEntries, MaxEntries + 1} {
		dir := filepath.Join(root, fmt.Sprint("d", n))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			write(t, filepath.Join(dir, fmt.Sprintf("f%04d.txt", i)), "x")
		}
		l, err := List(root, fmt.Sprint("d", n))
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Entries) != MaxEntries || l.Truncated != (n > MaxEntries) {
			t.Fatalf("n=%d got len=%d truncated=%v", n, len(l.Entries), l.Truncated)
		}
	}
}

func TestReadText(t *testing.T) {
	root, _ := newRoot(t)
	c, err := ReadText(root, "sub/a.txt")
	if err != nil || c.Content != "in-sub" || c.Size != 6 || c.Path != "sub/a.txt" {
		t.Fatalf("got %+v err=%v", c, err)
	}
	write(t, filepath.Join(root, "zh.txt"), "中文內容\n")
	if c, err := ReadText(root, "zh.txt"); err != nil || c.Content != "中文內容\n" {
		t.Fatalf("UTF-8 got %+v err=%v", c, err)
	}

	if _, err := ReadText(root, "sub"); !errors.Is(err, ErrIsDir) {
		t.Errorf("目錄當檔案讀應 ErrIsDir，got %v", err)
	}
	if _, err := ReadText(root, ""); !errors.Is(err, ErrIsDir) {
		t.Errorf("根目錄當檔案讀應 ErrIsDir，got %v", err)
	}
	if _, err := ReadText(root, "nope.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在應 ErrNotFound，got %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte("ab\x00cd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(root, "bin.dat"); !errors.Is(err, ErrBinary) {
		t.Errorf("含 NUL 應 ErrBinary，got %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad.txt"), []byte{0xff, 0xfe, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(root, "bad.txt"); !errors.Is(err, ErrBinary) {
		t.Errorf("非 UTF-8 應 ErrBinary，got %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "max.txt"), bytes.Repeat([]byte("a"), MaxFileBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := ReadText(root, "max.txt"); err != nil || len(c.Content) != MaxFileBytes {
		t.Errorf("剛好上限應可讀，err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), bytes.Repeat([]byte("a"), MaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadText(root, "big.txt")
	var tl *TooLargeError
	if !errors.As(err, &tl) || tl.Size != MaxFileBytes+1 {
		t.Errorf("過大應 *TooLargeError{Size:%d}，got %v", MaxFileBytes+1, err)
	}
}

func TestProtectedNames(t *testing.T) {
	root, _ := newRoot(t)
	protected := []string{".env", ".env.local", ".envrc", "server.pem", "SERVER.PEM", "tls.key", "id_rsa", "id_rsa.pub", "config.yaml", "CONFIG.YAML", "id_ed25519", "id_ed25519.pub", "id_ecdsa", "id_dsa", "client.pfx", "cert.P12", ".npmrc", ".netrc", ".pgpass", ".git-credentials"}
	if err := os.MkdirAll(filepath.Join(root, "mixed"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "mixed", "Config.YAML"), "bot_token: x") // 大小寫混用
	write(t, filepath.Join(root, "mixed", "ID_RSA"), "KEY")
	onDisk := []string{".env", ".env.local", ".envrc", "server.pem", "tls.key", "id_rsa", "id_rsa.pub", "config.yaml", "id_ed25519", "id_ecdsa", "id_dsa", "client.pfx", "cert.p12", ".npmrc", ".netrc", ".pgpass", ".git-credentials"}
	for _, n := range onDisk {
		write(t, filepath.Join(root, n), "SECRET")
	}
	// 檔案不存在時也不得洩漏存在與否：受保護名稱一律 ErrProtected
	for _, p := range append(append([]string{}, protected...), "mixed/Config.YAML", "mixed/ID_RSA", "sub/.env", "nonexistent.pem") {
		c, err := ReadText(root, p)
		if !errors.Is(err, ErrProtected) || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期 ErrProtected 且無內容", p, err, c.Content)
		}
	}
	// 清單：顯示名稱但標記受保護
	l, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string]bool{}
	for _, e := range l.Entries {
		flags[e.Name] = e.Protected
	}
	for _, n := range onDisk {
		if !flags[n] {
			t.Errorf("清單中 %q 應標記 protected（flags=%v）", n, flags[n])
		}
	}
	for _, n := range []string{"hello.txt", "sub"} {
		if flags[n] {
			t.Errorf("%q 不該被標記 protected", n)
		}
	}
	// 非受保護的相似名稱
	write(t, filepath.Join(root, "config.yaml.txt"), "ok")
	write(t, filepath.Join(root, "monkey"), "ok")
	write(t, filepath.Join(root, "keyboard.txt"), "ok")
	for _, p := range []string{"config.yaml.txt", "monkey", "keyboard.txt"} {
		if _, err := ReadText(root, p); err != nil {
			t.Errorf("%q 不該被擋: %v", p, err)
		}
	}
}

// 回應與錯誤字串都不得含 work_dir 的絕對路徑。
func TestNoAbsolutePathLeak(t *testing.T) {
	root, _ := newRoot(t)
	var blobs []string
	l, _ := List(root, "")
	b, _ := json.Marshal(l)
	blobs = append(blobs, string(b))
	c, _ := ReadText(root, "hello.txt")
	b, _ = json.Marshal(c)
	blobs = append(blobs, string(b))
	for _, p := range []string{"nope", "..", "hello.txt:x", "sub", ".env"} {
		_, e1 := List(root, p)
		_, e2 := ReadText(root, p)
		for _, e := range []error{e1, e2} {
			if e != nil {
				blobs = append(blobs, e.Error())
			}
		}
	}
	for _, s := range blobs {
		for _, needle := range []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`), filepath.Dir(root)} {
			if strings.Contains(s, needle) {
				t.Errorf("輸出洩漏路徑 %q: %s", needle, s)
			}
		}
	}
}

func TestSymlinkEscapeBlocked(t *testing.T) {
	root, outside := newRoot(t)
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Skipf("無法建立 symlink（Windows 需開發人員模式或系統管理員）: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "filelink.txt")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	// 指到內部的 symlink 與指到受保護檔的 symlink
	_ = os.Symlink(filepath.Join(root, "sub", "a.txt"), filepath.Join(root, "inlink.txt"))
	write(t, filepath.Join(root, ".env"), "SECRET")
	_ = os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "innocent.txt"))

	for _, p := range []string{"dirlink", "dirlink/secret.txt", "filelink.txt"} {
		if c, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期 ErrInvalidPath", p, err, c.Content)
		}
	}
	if _, err := List(root, "dirlink"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("List(dirlink) err=%v，預期 ErrInvalidPath", err)
	}
	// 不得成為外部檔案的存在探測器：穿過外部連結，存在與不存在都要是同一種錯誤
	for _, p := range []string{"dirlink/nope.txt", "dirlink/no/such/x.txt"} {
		if _, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ReadText(%q) err=%v，預期 ErrInvalidPath", p, err)
		}
	}
	l, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range l.Entries {
		if e.Name == "dirlink" || e.Name == "filelink.txt" {
			t.Errorf("指向外部的 symlink 不該出現在清單: %s", e.Name)
		}
	}
	if c, err := ReadText(root, "inlink.txt"); err == nil && c.Content != "in-sub" {
		t.Errorf("指向內部的 symlink 內容不對: %q", c.Content)
	}
	if c, err := ReadText(root, "innocent.txt"); !errors.Is(err, ErrProtected) || c.Content != "" {
		t.Errorf("指到 .env 的 symlink 應 ErrProtected，err=%v content=%q", err, c.Content)
	}
}

// work_dir 本身是 symlink 時仍可正常瀏覽，且不能因此放寬邊界。
func TestRootThatIsSymlink(t *testing.T) {
	root, _ := newRoot(t)
	link := filepath.Join(filepath.Dir(root), "rootlink")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if c, err := ReadText(link, "hello.txt"); err != nil || c.Content != "hello" {
		t.Fatalf("got %+v err=%v", c, err)
	}
	if _, err := ReadText(link, "../outside/secret.txt"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err=%v", err)
	}
}
