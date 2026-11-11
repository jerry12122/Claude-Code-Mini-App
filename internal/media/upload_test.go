package media

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveUpload(t *testing.T) {
	ws := t.TempDir()
	old := WorkspaceDir
	WorkspaceDir = ws
	t.Cleanup(func() { WorkspaceDir = old })

	// 路徑穿越的原檔名只取副檔名，檔案一定落在該 session 的 uploads 目錄內。
	p, err := SaveUpload("sess1", `..\..\evil/../x.PNG`, strings.NewReader("img"))
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(ws, "uploads", "sess1")
	if filepath.Dir(p) != wantDir || filepath.Ext(p) != ".png" {
		t.Fatalf("path=%s", p)
	}
	if b, _ := os.ReadFile(p); string(b) != "img" {
		t.Fatalf("content=%q", b)
	}

	// 不限類型：任意副檔名、無副檔名都可上傳；怪異副檔名會被淨化掉。
	for name, wantExt := range map[string]string{
		"x.exe":                     ".exe",
		"noext":                     "",
		"a.tar.gz":                  ".gz",
		"bad.e x":                   "",
		"long.aaaaaaaaaaaaaaaaaaaa": "",
	} {
		p, err := SaveUpload("sess1", name, strings.NewReader("data"))
		if err != nil {
			t.Errorf("%s 應可上傳: %v", name, err)
			continue
		}
		if filepath.Ext(p) != wantExt || filepath.Dir(p) != wantDir {
			t.Errorf("%s → %s，副檔名應為 %q", name, p, wantExt)
		}
	}

	// 不限大小：超過舊上限 8 MB 的檔案也可上傳。
	if _, err := SaveUpload("sess1", "big.bin", bytes.NewReader(make([]byte, 9*1024*1024))); err != nil {
		t.Errorf("大檔應可上傳: %v", err)
	}

	// 空檔仍拒絕且不殘留。
	before, _ := os.ReadDir(wantDir)
	if _, err := SaveUpload("sess1", "e.png", bytes.NewReader(nil)); err == nil {
		t.Error("空檔應被拒絕")
	}
	after, _ := os.ReadDir(wantDir)
	if len(after) != len(before) {
		t.Fatalf("被拒絕的檔案不應殘留，before=%d after=%d", len(before), len(after))
	}
}

type failedUploadReader struct{}

func (failedUploadReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestInterruptedUploadIsRemoved(t *testing.T) {
	old := WorkspaceDir
	WorkspaceDir = t.TempDir()
	defer func() { WorkspaceDir = old }()
	_, err := SaveUpload("session", "file.bin", io.MultiReader(strings.NewReader("partial"), failedUploadReader{}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v", err)
	}
	files, err := os.ReadDir(filepath.Join(WorkspaceDir, "uploads", "session"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("partial upload retained: %v", files)
	}
}
