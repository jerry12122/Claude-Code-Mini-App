package desktopupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
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

	problems, _, err := linuxArchiveModeProblems(filepath.Join(dir, tgzName))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
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

type linuxArchiveMember struct {
	Name  string
	Mode  int64
	Size  int64
	Magic []byte
}

// linuxArchiveModeProblems 只讀 tar header。目錄維持 0755，頂層 claude-miniapp 必須 0755 且為 ELF；
// 其他一般檔不得帶執行位。不把成員解到磁碟。
func linuxArchiveModeProblems(tgzPath string) ([]string, linuxArchiveMember, error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		return nil, linuxArchiveMember{}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, linuxArchiveMember{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var problems []string
	var binary linuxArchiveMember
	found := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, linuxArchiveMember{}, err
		}
		if hdr.Typeflag == tar.TypeXHeader || hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeGNULongName || hdr.Typeflag == tar.TypeGNULongLink {
			continue
		}
		perm := hdr.Mode & 0o777
		name := path.Clean(hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if perm != 0o755 {
				problems = append(problems, fmt.Sprintf("目錄 %s mode %04o，要 0755", name, perm))
			}
		case tar.TypeReg, tar.TypeRegA:
			isBinary := strings.Count(name, "/") == 1 && path.Base(name) == "claude-miniapp"
			if isBinary {
				found++
				magic := make([]byte, 4)
				if _, err := io.ReadFull(tr, magic); err != nil {
					return nil, linuxArchiveMember{}, err
				}
				binary = linuxArchiveMember{Name: name, Mode: perm, Size: hdr.Size, Magic: magic}
				if perm != 0o755 {
					problems = append(problems, fmt.Sprintf("Linux binary %s mode %04o，要 0755", name, perm))
				}
				if !bytes.HasPrefix(magic, []byte{0x7f, 'E', 'L', 'F'}) {
					problems = append(problems, fmt.Sprintf("Linux binary %s 不是 ELF", name))
				}
				continue
			}
			if perm&0o111 != 0 {
				problems = append(problems, fmt.Sprintf("非執行檔 %s mode %04o 不該有執行位", name, perm))
			}
		}
	}
	if found != 1 {
		problems = append(problems, fmt.Sprintf("頂層 claude-miniapp 應恰有一個，實際 %d", found))
	}
	return problems, binary, nil
}

func TestLinuxArchiveHeaderModes(t *testing.T) {
	write := func(t *testing.T, binaryMode, docMode int64) string {
		t.Helper()
		target := filepath.Join(t.TempDir(), "sample.tar.gz")
		f, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		add := func(hdr *tar.Header, body []byte) {
			t.Helper()
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.WriteHeader(&tar.Header{Name: "pkg/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: "pkg/docs/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
		add(&tar.Header{Name: "pkg/claude-miniapp", Mode: binaryMode, Typeflag: tar.TypeReg, Size: 4}, []byte{0x7f, 'E', 'L', 'F'})
		add(&tar.Header{Name: "pkg/README.md", Mode: docMode, Typeflag: tar.TypeReg, Size: 4}, []byte("docs"))
		add(&tar.Header{Name: "pkg/config.example.yaml", Mode: 0o644, Typeflag: tar.TypeReg, Size: 3}, []byte("cfg"))
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		return target
	}
	t.Run("0755 binary keeps docs unexecutable", func(t *testing.T) {
		problems, binary, err := linuxArchiveModeProblems(write(t, 0o755, 0o644))
		if err != nil || len(problems) != 0 || binary.Mode != 0o755 || binary.Size != 4 {
			t.Fatalf("err=%v problems=%v binary=%+v", err, problems, binary)
		}
	})
	t.Run("0644 binary is rejected from the real header", func(t *testing.T) {
		problems, binary, err := linuxArchiveModeProblems(write(t, 0o644, 0o644))
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) == 0 || binary.Mode != 0o644 {
			t.Fatalf("缺執行位仍通過：%v %+v", problems, binary)
		}
	})
	t.Run("executable doc is rejected", func(t *testing.T) {
		problems, _, err := linuxArchiveModeProblems(write(t, 0o755, 0o755))
		if err != nil {
			t.Fatal(err)
		}
		if len(problems) != 1 || !strings.Contains(problems[0], "README.md") {
			t.Fatalf("文件被設成可執行時應拒絕：%v", problems)
		}
	})
}
