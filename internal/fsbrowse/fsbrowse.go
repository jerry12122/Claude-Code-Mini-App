// Package fsbrowse 唯讀瀏覽某個根目錄（session 的 work_dir）底下的檔案：列目錄、讀文字檔。
//
// 這是對外暴露檔案系統的唯一入口，安全邊界都在這裡，呼叫端不必（也不該）再自己處理路徑：
//   - 相對路徑在解析前就先擋掉 `..`、絕對路徑、磁碟機代號、UNC、NUL／控制字元、反斜線、冒號（備用資料流）；
//     Windows 另擋保留裝置名、尾端點與空白、8.3 短檔名別名。
//   - 解析後取「真實路徑」再比對是否仍在根目錄內，擋 symlink／junction 跳出。
//   - 受保護檔名永不回傳內容；指向外部的 symlink 不出現在清單，也不洩漏其目標。
//   - 所有錯誤與輸出只含相對路徑，不含根目錄的絕對路徑。
package fsbrowse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxEntries   = 500        // 單次列目錄上限
	MaxFileBytes = 512 * 1024 // 可預覽的文字檔上限
	maxPathLen   = 4096
)

var (
	ErrInvalidPath = errors.New("路徑無效")
	ErrProtected   = errors.New("受保護的檔案，不提供內容")
	ErrNotFound    = errors.New("找不到")
	ErrNotDir      = errors.New("不是目錄")
	ErrIsDir       = errors.New("是目錄，不是檔案")
	ErrBinary      = errors.New("不是文字檔")
)

// TooLargeError 帶實際大小，讓呼叫端能告訴使用者。
type TooLargeError struct{ Size int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("檔案過大（%d bytes，上限 %d）", e.Size, MaxFileBytes)
}

type Entry struct {
	Name      string    `json:"name"`
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	MTime     time.Time `json:"mtime"`
	Protected bool      `json:"protected,omitempty"`
}

type Listing struct {
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

type Content struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

// isProtected 依檔名（不分大小寫）判斷是否為永不回傳內容的檔案：.env*、*.pem、*.key、*.pfx、*.p12、
// SSH 私鑰（id_rsa／id_ed25519／id_ecdsa／id_dsa 開頭）、常見憑證檔與 config.yaml（本專案設定檔含 bot_token、密碼）。
func isProtected(name string) bool {
	n := strings.ToLower(name)
	for _, pre := range []string{".env", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
		if strings.HasPrefix(n, pre) {
			return true
		}
	}
	for _, suf := range []string{".pem", ".key", ".pfx", ".p12"} {
		if strings.HasSuffix(n, suf) {
			return true
		}
	}
	switch n {
	case "config.yaml", ".npmrc", ".netrc", ".pgpass", ".git-credentials":
		return true
	}
	return false
}

// cleanRel 驗證使用者給的相對路徑並拆成元件。這裡不碰檔案系統，也不做任何解碼（URL 解碼是 HTTP 層的事，只解一次）。
func cleanRel(rel string) ([]string, error) {
	if len(rel) > maxPathLen || !utf8.ValidString(rel) || strings.HasPrefix(rel, "/") {
		return nil, ErrInvalidPath
	}
	var parts []string
	for _, p := range strings.Split(rel, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			return nil, ErrInvalidPath
		}
		if !nameOK(p) {
			return nil, ErrInvalidPath
		}
		parts = append(parts, p)
	}
	return parts, nil
}

// nameOK 檢查單一檔名元件。冒號（磁碟機代號、備用資料流）與反斜線在所有平台一律拒絕，
// 讓行為與平台無關；平台特有的規則在 platformNameOK。
func nameOK(name string) bool {
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '\\' || r == ':' {
			return false
		}
	}
	return platformNameOK(name)
}

// resolve 回傳根目錄真實路徑與目標真實路徑，並保證目標在根目錄內。
func resolve(root string, parts []string) (rootReal, real string, err error) {
	rootReal, err = realPath(root)
	if err != nil {
		return "", "", mapErr(err)
	}
	real, err = realPath(filepath.Join(append([]string{rootReal}, parts...)...))
	if errors.Is(err, fs.ErrNotExist) {
		// 目標不存在時，改驗證最深的既存祖先是否仍在根目錄內：穿過指向外部的連結時，
		// 「存在」與「不存在」必須回同一種錯誤，否則能拿來探測外部檔案是否存在。
		for i := len(parts) - 1; i >= 0; i-- {
			anc, aerr := realPath(filepath.Join(append([]string{rootReal}, parts[:i]...)...))
			if aerr == nil {
				if !within(rootReal, anc) {
					return "", "", ErrInvalidPath
				}
				break
			}
		}
	}
	if err != nil {
		return "", "", mapErr(err)
	}
	if !within(rootReal, real) {
		return "", "", ErrInvalidPath
	}
	return rootReal, real, nil
}

// mapErr 把檔案系統錯誤換成不含路徑的哨兵錯誤（os.PathError 會帶完整絕對路徑）。
func mapErr(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return ErrInvalidPath
}

func within(root, p string) bool {
	if len(p) < len(root) || !pathEqual(p[:len(root)], root) {
		return false
	}
	return len(p) == len(root) || p[len(root)] == filepath.Separator || strings.HasSuffix(root, string(filepath.Separator))
}

// List 列出根目錄底下 rel 這個目錄的項目：目錄在前、名稱不分大小寫排序，最多 MaxEntries 項。
func List(root, rel string) (Listing, error) {
	parts, err := cleanRel(rel)
	if err != nil {
		return Listing{}, err
	}
	rootReal, real, err := resolve(root, parts)
	if err != nil {
		return Listing{}, err
	}
	if st, err := os.Stat(real); err != nil {
		return Listing{}, mapErr(err)
	} else if !st.IsDir() {
		return Listing{}, ErrNotDir
	}
	des, err := os.ReadDir(real)
	if err != nil {
		return Listing{}, mapErr(err)
	}

	type item struct {
		name  string
		isDir bool
		info  fs.FileInfo
		prot  string // 受保護比對用的真實檔名（symlink 指到 .env 時要擋）
	}
	items := make([]item, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if !nameOK(name) {
			continue // 開不了的名稱不放進清單
		}
		it := item{name: name, isDir: de.IsDir(), prot: name}
		if de.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			target, err := realPath(filepath.Join(real, name))
			if err != nil || !within(rootReal, target) {
				continue // 指向外部或失效的連結：不顯示，也不提目標
			}
			st, err := os.Stat(target)
			if err != nil {
				continue
			}
			it.isDir, it.info, it.prot = st.IsDir(), st, filepath.Base(target)
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].isDir != items[j].isDir {
			return items[i].isDir
		}
		a, b := strings.ToLower(items[i].name), strings.ToLower(items[j].name)
		if a != b {
			return a < b
		}
		return items[i].name < items[j].name
	})

	out := Listing{Path: strings.Join(parts, "/"), Entries: []Entry{}}
	if len(items) > MaxEntries {
		items, out.Truncated = items[:MaxEntries], true
	}
	for _, it := range items {
		info := it.info
		if info == nil {
			var err error
			if info, err = os.Lstat(filepath.Join(real, it.name)); err != nil {
				continue
			}
		}
		e := Entry{Name: it.name, IsDir: it.isDir, MTime: info.ModTime().UTC(), Protected: !it.isDir && (isProtected(it.name) || isProtected(it.prot))}
		if !it.isDir {
			e.Size = info.Size()
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

// ReadText 讀取根目錄底下的單一文字檔（UTF-8、不含 NUL、≤ MaxFileBytes）。
func ReadText(root, rel string) (Content, error) {
	parts, err := cleanRel(rel)
	if err != nil {
		return Content{}, err
	}
	if len(parts) == 0 {
		return Content{}, ErrIsDir
	}
	// 先看名稱：受保護檔名不論存不存在都回 ErrProtected，不碰檔案系統，也不透露存在與否。
	if isProtected(parts[len(parts)-1]) {
		return Content{}, ErrProtected
	}
	_, real, err := resolve(root, parts)
	if err != nil {
		return Content{}, err
	}
	// 再看真實名稱：擋住 innocent.txt -> .env 這類換名字的連結，以及 8.3 短名解開後的長名。
	if isProtected(filepath.Base(real)) {
		return Content{}, ErrProtected
	}
	st, err := os.Stat(real)
	if err != nil {
		return Content{}, mapErr(err)
	}
	if st.IsDir() {
		return Content{}, ErrIsDir
	}
	if !st.Mode().IsRegular() {
		return Content{}, ErrInvalidPath
	}
	if st.Size() > MaxFileBytes {
		return Content{}, &TooLargeError{Size: st.Size()}
	}
	f, err := os.Open(real)
	if err != nil {
		return Content{}, mapErr(err)
	}
	defer f.Close()
	// 多讀一個 byte：檔案在 Stat 之後變大也守得住上限。
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return Content{}, mapErr(err)
	}
	if len(data) > MaxFileBytes {
		return Content{}, &TooLargeError{Size: int64(len(data))}
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return Content{}, ErrBinary
	}
	return Content{Path: strings.Join(parts, "/"), Size: int64(len(data)), Content: string(data)}, nil
}
