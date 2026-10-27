package version

import "runtime/debug"

// Version 應與 repo 根目錄 VERSION 同步。
const Version = "0.16.0"

// Revision 回傳執行檔建置時的 Git SHA；未包含 VCS 資訊時回傳 unknown。
func Revision() string {
	info, _ := debug.ReadBuildInfo()
	return revision(info)
}

func revision(info *debug.BuildInfo) string {
	if info != nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return "unknown"
}
