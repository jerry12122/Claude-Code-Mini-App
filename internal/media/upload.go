package media

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceDir 是 runtime 工作區，相對於工作目錄（桌面版為 exe 旁）。
// 上傳檔放這裡，不寫進使用者專案（work_dir）；也不放 internal/static/uploads——
// 那裡掛在 app.Static，未驗證就能讀。測試可覆寫。
var WorkspaceDir = "workspace"

// safeExt 取原檔名的副檔名（小寫、含點）。不限制類型，但只接受 1~16 個英數字元，
// 其餘（含空白、特殊字元、過長）一律視為無副檔名，避免污染儲存檔名。
func safeExt(origName string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(strings.ReplaceAll(origName, `\`, "/"))))
	if len(ext) < 2 || len(ext) > 17 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// SaveUpload 把使用者上傳的檔案存到 <WorkspaceDir>/uploads/<sessionID>/<隨機名>[.ext]，回傳絕對路徑。
// sessionID 必須來自資料庫（已驗證存在），不可直接使用請求參數。
// 檔名一律由伺服器產生（原檔名只取淨化後的副檔名），杜絕路徑穿越。
// 不限制類型與大小；呼叫端以串流和併發上限控制資源，空檔案仍拒絕。
func SaveUpload(sessionID, origName string, r io.Reader) (string, error) {
	ext := safeExt(origName)
	dir := filepath.Join(WorkspaceDir, "uploads", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("media: mkdir upload dir: %w", err)
	}
	name, err := randomFilename(ext)
	if err != nil {
		return "", fmt.Errorf("media: generate filename: %w", err)
	}
	dst := filepath.Join(dir, name)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("media: create upload file: %w", err)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr == nil && n == 0 {
		copyErr = fmt.Errorf("檔案是空的")
	}
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(dst)
		return "", copyErr
	}
	return filepath.Abs(dst)
}
