//go:build windows

package fsbrowse

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// mklinkJunction 建立目錄 junction（不需系統管理員權限）。Go 1.23 起 EvalSymlinks 不再展開 junction，
// 所以光靠 filepath.EvalSymlinks 比對前綴會被它繞過——這是本檔最重要的一個測試。
func mklinkJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("無法建立 junction: %v %s", err, out)
	}
}

func TestJunctionEscapeBlocked(t *testing.T) {
	root, outside := newRoot(t)
	mklinkJunction(t, filepath.Join(root, "junc"), outside)

	for _, p := range []string{"junc", "junc/secret.txt", "JUNC/secret.txt"} {
		if c, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期 ErrInvalidPath", p, err, c.Content)
		}
	}
	if l, err := List(root, "junc"); !errors.Is(err, ErrInvalidPath) || len(l.Entries) != 0 {
		t.Errorf("List(junc) err=%v entries=%v，預期 ErrInvalidPath 且無項目", err, l.Entries)
	}
	l, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range l.Entries {
		if strings.EqualFold(e.Name, "junc") {
			t.Errorf("指向外部的 junction 不該出現在清單")
		}
	}
}

// 穿過指向外部的 junction 時，「存在」與「不存在」的回應必須一致，否則能拿它探測外部檔案是否存在。
func TestJunctionEscapeNoExistenceOracle(t *testing.T) {
	root, outside := newRoot(t)
	mklinkJunction(t, filepath.Join(root, "junc"), outside)
	for _, p := range []string{"junc/secret.txt", "junc/nope.txt", "junc/no/such/dir/x.txt", "JUNC/nope.txt"} {
		if _, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ReadText(%q) err=%v，預期一律 ErrInvalidPath", p, err)
		}
		if _, err := List(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("List(%q) err=%v，預期一律 ErrInvalidPath", p, err)
		}
	}
	// 一般不存在的路徑仍是 ErrNotFound
	if _, err := ReadText(root, "sub/nope.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("work_dir 內不存在應 ErrNotFound，got %v", err)
	}
}

// junction 指到 work_dir 內部是合法的，不能被誤擋。
func TestJunctionInsideRootAllowed(t *testing.T) {
	root, _ := newRoot(t)
	mklinkJunction(t, filepath.Join(root, "alias"), filepath.Join(root, "sub"))
	if c, err := ReadText(root, "alias/a.txt"); err != nil || c.Content != "in-sub" {
		t.Fatalf("got %+v err=%v", c, err)
	}
}

func TestReservedDeviceNamesRejected(t *testing.T) {
	root, _ := newRoot(t)
	for _, p := range []string{"CON", "con", "NUL", "nul", "PRN", "AUX", "COM1", "com9", "LPT1", "LPT9", "NUL.txt", "sub/CON", "sub/aux.log", "con.", "COM1.tar.gz"} {
		if _, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ReadText(%q) err=%v，預期 ErrInvalidPath", p, err)
		}
		if _, err := List(root, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("List(%q) err=%v，預期 ErrInvalidPath", p, err)
		}
	}
}

// Windows 會悄悄去掉檔名尾端的點與空白，config.yaml. 實際開到 config.yaml，繞過以名稱比對的保護。
func TestTrailingDotsAndSpacesRejected(t *testing.T) {
	root, _ := newRoot(t)
	write(t, filepath.Join(root, "config.yaml"), "bot_token: x")
	for _, p := range []string{"config.yaml.", "config.yaml ", "config.yaml..", "config.yaml. ", "config.yaml .", "sub.", "sub /a.txt", "sub./a.txt", "..."} {
		c, err := ReadText(root, p)
		if err == nil || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期被拒且無內容", p, err, c.Content)
		}
		if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrProtected) {
			t.Errorf("ReadText(%q) err=%v，預期 ErrInvalidPath 或 ErrProtected", p, err)
		}
	}
}

// 8.3 短檔名（CONFIG~1.YAM）指向受保護檔：不論磁碟有沒有產生短名，都不能回內容。
func TestShortNameAliasOfProtectedFile(t *testing.T) {
	root, _ := newRoot(t)
	write(t, filepath.Join(root, "config.yaml"), "bot_token: x")
	for _, p := range []string{"CONFIG~1.YAM", "config~1.yam", "sub/CONFIG~1.YAM", "SECRET~1.KEY"} {
		c, err := ReadText(root, p)
		if err == nil || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期被拒且無內容", p, err, c.Content)
		}
	}
	// 若這個磁碟區有產生短名，用系統回報的真實短名再試一次
	long, _ := windows.UTF16PtrFromString(filepath.Join(root, "config.yaml"))
	buf := make([]uint16, 512)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Skipf("GetShortPathName: %v", err)
	}
	short := filepath.Base(windows.UTF16ToString(buf[:n]))
	if strings.EqualFold(short, "config.yaml") {
		t.Skip("此磁碟區未產生 8.3 短名")
	}
	c, err := ReadText(root, short)
	if err == nil || c.Content != "" {
		t.Fatalf("真實短名 %q 讀到了受保護檔: err=%v content=%q", short, err, c.Content)
	}
}

// 大小寫不敏感的檔案系統：Config.YAML 與 config.yaml 是同一個檔。
func TestMixedCaseProtectedOnDisk(t *testing.T) {
	root, _ := newRoot(t)
	write(t, filepath.Join(root, "config.yaml"), "bot_token: x")
	for _, p := range []string{"Config.YAML", "CONFIG.yaml", "cOnFiG.YaMl"} {
		if c, err := ReadText(root, p); !errors.Is(err, ErrProtected) || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q", p, err, c.Content)
		}
	}
}

// 以不同大小寫請求正常路徑仍應可用（前綴比對不能被大小寫誤判成「跳出」）。
func TestCaseInsensitiveNormalPathStillWorks(t *testing.T) {
	root, _ := newRoot(t)
	if c, err := ReadText(root, "SUB/A.TXT"); err != nil || c.Content != "in-sub" {
		t.Fatalf("got %+v err=%v", c, err)
	}
}

func TestAlternateDataStreamOnRealFile(t *testing.T) {
	root, _ := newRoot(t)
	if err := os.WriteFile(filepath.Join(root, "hello.txt:hidden"), []byte("ADS-SECRET"), 0o644); err != nil {
		t.Skipf("檔案系統不支援 ADS: %v", err)
	}
	for _, p := range []string{"hello.txt:hidden", "hello.txt:hidden:$DATA", "hello.txt::$DATA"} {
		if c, err := ReadText(root, p); !errors.Is(err, ErrInvalidPath) || c.Content != "" {
			t.Errorf("ReadText(%q) err=%v content=%q，預期 ErrInvalidPath", p, err, c.Content)
		}
	}
	l, _ := List(root, "")
	for _, e := range l.Entries {
		if strings.Contains(e.Name, ":") {
			t.Errorf("清單不該含冒號名稱: %s", e.Name)
		}
	}
}
