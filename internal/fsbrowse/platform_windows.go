//go:build windows

package fsbrowse

import (
	"strings"

	"golang.org/x/sys/windows"
)

// platformNameOK 擋掉 Windows 會悄悄改寫檔名的寫法，否則以名稱比對的保護會被繞過：
// 尾端點與空白（config.yaml. 實際開到 config.yaml）、保留裝置名（CON、NUL.txt）、
// 不合法字元，以及 8.3 短檔名別名（CONFIG~1.YAM 指向 config.yaml）。
func platformNameOK(name string) bool {
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.ContainsAny(name, `<>"|?*`) {
		return false
	}
	base := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		base = name[:i]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	for i := 0; i+1 < len(name); i++ {
		if name[i] == '~' && name[i+1] >= '0' && name[i+1] <= '9' {
			return false
		}
	}
	return true
}

func pathEqual(a, b string) bool { return strings.EqualFold(a, b) }

// realPath 以 GetFinalPathNameByHandle 取得真實路徑。不用 filepath.EvalSymlinks：
// Go 1.23 起它不再展開 junction（mount point），junction 指到根目錄外時前綴比對會被繞過。
// 這個作法同時把 8.3 短名展開成長名、並還原實際大小寫。
func realPath(p string) (string, error) {
	pp, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(pp, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) <= len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]uint16, n)
	}
	s := windows.UTF16ToString(buf)
	switch {
	case strings.HasPrefix(s, `\\?\UNC\`):
		s = `\\` + s[len(`\\?\UNC\`):]
	case strings.HasPrefix(s, `\\?\`):
		s = s[len(`\\?\`):]
	}
	return s, nil
}
