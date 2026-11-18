## 1. 安裝與相容

- [x] 1.1 實作安裝模式與固定資料目錄（Installed desktop data isolation）、Installed desktop update continuity、桌面端維護互斥與 Runtime 對應 profile；helper 最先處理，失敗停止或登錄錯誤只記錄；以 go test ./cmd/desktop 覆蓋固定路徑、失敗、mutex、registry 與便攜相容。
- [x] 1.2 實作 Inno Setup 包裝同一個 EXE、Safe installer maintenance 與 Verified WebView2 prerequisite：目前使用者、固定 AppId、捷徑、降版／鎖定拒絕、維護互斥與 Runtime、移除保留資料；加入 bootstrapper 簽章驗證與隔離安裝 smoke，驗證 ISCC 編譯與 smoke。
- [x] 1.3 更新中英 README／入門文件及 release header，清楚交付 Setup 推薦下載、便攜保留、手動遷移與保留資料的移除行為；以文件內容與相對連結檢查驗證。

## 2. 發布與整合驗證

- [x] 2.1 [after: 1.1, 1.2] 實作五資產草稿發布（Current-user Windows installer）：VERSION 產生 Windows 版本資源，固定版 Inno Setup 包裝同一 EXE，Setup 納入 checksum／發布精確清單；以 go test ./internal/desktopupdate 及 scripts/release/test/test-release.sh 驗證缺少／毀損 Setup 不公開與 updater 不選 Setup。
- [x] 2.2 [after: 1.3, 2.1] 執行桌面／production／發布契約及真實 Setup smoke，檢查 scenario 測試對應並記錄未實跑的實機矩陣；更新原規劃及 todo 的真實狀態，驗證 spectra validate windows-installer 與 git diff --check，不發布或提交。
