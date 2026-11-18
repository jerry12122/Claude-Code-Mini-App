## Context

本變更依 docs/plan/todo/windows-installer.md。既有 go build 產生 production EXE，updater helper 在 main 最先處理，前端已嵌入。Wails WindowsOptions 的預設 WebviewUserDataPath 為 %APPDATA% 下的 binary 名稱。

## Goals / Non-Goals

**Goals:** Windows x64 目前使用者安裝、程式與資料分離、安全重裝／移除、沿用 EXE 自更新與草稿發布。

**Non-Goals:** MSI／MSIX、Store、全機安裝、CLI 自動安裝、自動資料匯入、重寫更新器、強制關閉工作、修改未相關的既有工作樹內容。

## Decisions

### 安裝模式與固定資料目錄

以 installed.flag 明確切換安裝模式；程式位於 %LOCALAPPDATA%\Programs\ClaudeMiniApp，資料位於 %LOCALAPPDATA%\ClaudeMiniApp\data。沿用相對路徑與既有便攜判定，避免重構全部儲存介面。WebView2 profile 固定為資料目錄的 webview2 子目錄。helper 仍先於所有副作用。

### Inno Setup 包裝同一個 EXE

新增單一 .iss；不遷移 Wails 建置。安裝器固定 AppId=ClaudeMiniApp、PrivilegesRequired=lowest、固定程式位置，開始功能表與選用桌面捷徑；不執行使用者資料刪除。Windows 版本資源由 VERSION 在發布建置時產生，以四段數字比較阻擋降版。解除安裝項目位於 HKCU Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeMiniApp_is1；安裝版正常啟動更新 DisplayVersion，登錄失敗只記錄。

### 維護互斥與 Runtime

桌面在 helper 處理後持有 Local\ClaudeMiniApp.Desktop；Setup／uninstaller 持有 Local\ClaudeMiniApp.Setup。兩邊先持有自身 mutex，再檢查對方；偵測維護就拒絕桌面啟動，偵測桌面或鎖定就拒絕維護。不得關閉應用或延後到重開機換檔。重用 Inno Setup 的檔案／程序檢查，無法判定時保守拒絕。

WebView2 依 Microsoft HKCU／HKLM pv registry 檢查，排除空或 0.0.0.0。缺少時使用建置時從微軟官方下載且 Authenticode 驗證為 Microsoft Corporation 的 Evergreen Bootstrapper，打入安裝包；執行失敗或安裝後仍缺少即報錯。驗證腳本失敗必須阻止打包。已存在 Runtime 時不執行 bootstrapper。

### 五資產草稿發布

新增 claude-miniapp-<tag>-windows-amd64-setup.exe，與三個既有程式資產共同納入 checksums.txt，合計五個發布資產。package.sh 只建置桌面 EXE 一次，Setup／ZIP／自更新 EXE 共用位元組。CI 準備固定版本 Inno Setup，先執行安裝 smoke 與產物契約才呼叫 publish.sh。保留草稿先行與公開版本不可覆寫的規則。本版維持現有未簽章應用產物，不新增金鑰／簽署服務；未來簽署須先簽共用 EXE 再打包、簽 Setup 後產生 checksums。

## Implementation Contract

- Task 1: cmd/desktop 在 installed.flag 存在時建立／切換固定資料位置；LOCALAPPDATA 缺少、標記讀取或 mkdir／chdir 失敗要顯示錯誤停止。無標記沿用便攜及開發規則。WindowsOptions 對安裝版設定固定 profile。helper 先行，正常啟動持有桌面 mutex，維護中拒絕；安裝版成功啟動同步既有 HKCU 登錄版本，失敗只 log。以 go test ./cmd/desktop 驗證路徑、失敗、mutex 與 registry 分支。
- Task 2: scripts/release/windows-installer.iss 接受 AppVersion、SourceDir、OutputDir、BootstrapperFile，SourceDir 含固定名稱桌面 EXE；產物命名使用 v<AppVersion>。固定安裝位置、捷徑與 AppId，Runtime 檢查／補裝、較新版或未知版本拒絕覆蓋、maintenance mutex、檔案鎖拒絕與資料保留。新增 powershell bootstrapper 下載驗證及安裝／移除 smoke；smoke 使用臨時隔離資料與測試標的，不動既有使用者安裝。編譯及 smoke 成功，尚未取得的標準帳戶／缺 Runtime／Win10 實機證據必須明列。
- Task 3: scripts/release/package.sh 在打包前依 VERSION 產生 EXE version resources，為同一 EXE 產生 Setup，四個程式資產 SHA256 正確；publish.sh 精確要求五個資產。更新 workflow 與 release 契約／假 gh 測試，涵蓋缺 Setup、毀損 Setup、updater 不選 Setup，驗證完才允許公開。驗證 go test ./internal/desktopupdate、scripts/release/test/test-release.sh，真實本機 Setup 編譯與 smoke。
- Task 4: README 與中英入門文件、release header 說明 Setup 推薦、ZIP 便攜／server、資料與 profile 位置、移除保留資料、停機備份與手動遷移、絕對路徑附件限制。文件不得宣稱 VM／標準帳戶已驗收。以連結與內容檢查驗證，單一文件呈現為測試範圍排除。
- Task 5: 整合命令涵蓋上述驗證及 production 標籤，記錄實跑／未實跑矩陣，更新原規劃與待辦的真實狀態，不發布 Release、不建立 tag、不提交。

## Risks / Trade-offs

- 無完整離線 Runtime → 缺少 Runtime 且無網路時明確失敗；保留微軟手動安裝指引。
- 無法在本機證明所有 Windows 環境 → CI smoke 可重跑，實機驗收獨立記錄，不虛報。
- 搬移舊資料存在絕對路徑 → 手動遷移並保留原檔，不重寫歷史；關閉 SQLite 後備份。
- 應用無 Authenticode 簽章 → 延續現狀並在下載說明揭露；Bootstrapper 仍須驗證微軟簽章。
