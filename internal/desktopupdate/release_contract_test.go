package desktopupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

// TestReleaseDistContract 以 scripts/release/package.sh 產出的真實 dist 目錄驗證發佈契約：
//   - 桌面 EXE 檔名 == AssetName(version)，且符合 desktopAssetPattern；其餘資產不符合
//   - checksums.txt 涵蓋 ZIP、tar.gz、桌面 EXE，且雜湊與實際檔案相符
//   - provider 能從真實 checksums.txt 解析出桌面 EXE 的 sha256，並下載到與 dist 相同的位元組
//   - ZIP 內的 claude-miniapp-desktop.exe 與版本化 EXE 位元組相同
//   - dist 版本與 internal/version.Version 一致
//
// 需要 RELEASE_DIST=<dist 目錄>；未設定時略過。整個流程只走本機 httptest，不連線 GitHub。
func TestReleaseDistContract(t *testing.T) {
	dir := os.Getenv("RELEASE_DIST")
	if dir == "" {
		t.Skip("RELEASE_DIST 未設定，略過發佈產物契約測試")
	}

	exes, err := filepath.Glob(filepath.Join(dir, "claude-miniapp-desktop-v*-windows-amd64.exe"))
	if err != nil || len(exes) != 1 {
		t.Fatalf("dist 中應恰有一個桌面 EXE，實際 %v (err %v)", exes, err)
	}
	exeName := filepath.Base(exes[0])
	ver := strings.TrimSuffix(strings.TrimPrefix(exeName, "claude-miniapp-desktop-v"), "-windows-amd64.exe")
	tag := "v" + ver

	if ver != version.Version {
		t.Fatalf("dist 版本 %q 與 internal/version.Version %q 不符", ver, version.Version)
	}
	if got := AssetName(ver); got != exeName {
		t.Fatalf("AssetName(%q) = %q，dist 檔名 %q", ver, got, exeName)
	}
	if !desktopAssetPattern.MatchString(exeName) {
		t.Fatalf("desktopAssetPattern 不接受 dist 檔名 %q", exeName)
	}

	zipName := "claude-miniapp-" + tag + "-windows-amd64.zip"
	tgzName := "claude-miniapp-" + tag + "-linux-amd64.tar.gz"
	setupName := "claude-miniapp-" + tag + "-windows-amd64-setup.exe"
	for _, n := range []string{zipName, tgzName, setupName} {
		if desktopAssetPattern.MatchString(n) {
			t.Errorf("desktopAssetPattern 不應接受程式資產 %q", n)
		}
	}

	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	exeBytes := read(exeName)
	sums := read("checksums.txt")

	// checksums.txt：三個程式資產都要有一行，且雜湊與實際檔案相符（sha256sum 格式）
	want := map[string]string{
		zipName:   sha256Hex(read(zipName)),
		tgzName:   sha256Hex(read(tgzName)),
		exeName:   sha256Hex(exeBytes),
		setupName: sha256Hex(read(setupName)),
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(sums), "\n"), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			t.Fatalf("checksums.txt 行格式不符 sha256sum：%q", line)
		}
		got[parts[1]] = parts[0]
	}
	if len(got) != len(want) {
		t.Errorf("checksums.txt 涵蓋 %d 個檔案，預期 %d 個（%v）", len(got), len(want), want)
	}
	for name, h := range want {
		if got[name] != h {
			t.Errorf("checksums.txt 的 %s = %q，實際 %q", name, got[name], h)
		}
	}

	// ZIP 內的桌面 EXE 與版本化 EXE 位元組相同
	zr, err := zip.OpenReader(filepath.Join(dir, zipName))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	inner := false
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/claude-miniapp-desktop.exe") {
			continue
		}
		inner = true
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, exeBytes) {
			t.Errorf("ZIP 內 %s 與 %s 位元組不同", f.Name, exeName)
		}
	}
	if !inner {
		t.Error("ZIP 內沒有 claude-miniapp-desktop.exe")
	}

	// provider 端：用真實 checksums.txt 走 Check → Download
	f := newFixture(t)
	f.setLatest(stableRelease(ver,
		fakeAsset{Name: setupName, Body: read(setupName)},
		fakeAsset{Name: zipName, Body: read(zipName)},
		fakeAsset{Name: tgzName, Body: read(tgzName)},
		fakeAsset{Name: exeName, Body: exeBytes},
		fakeAsset{Name: checksumAsset, Body: sums},
	))
	p := newTestProvider(t, f)
	rel, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.0.0", Platform: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatalf("Check error = %v", err)
	}
	if rel == nil {
		t.Fatal("Check 沒有回傳候選")
	}
	if rel.Artifact.Filename != exeName {
		t.Errorf("Artifact.Filename = %q, want %q", rel.Artifact.Filename, exeName)
	}
	if rel.Verification == nil || hex.EncodeToString(rel.Verification.Digest) != want[exeName] {
		t.Errorf("provider 解析出的 sha256 與 checksums.txt 不符")
	}

	var buf bytes.Buffer
	if err := p.Download(context.Background(), rel, &buf, func(int64, int64) {}); err != nil {
		t.Fatalf("Download error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), exeBytes) {
		t.Errorf("下載位元組與 dist 的 %s 不同", exeName)
	}
}
