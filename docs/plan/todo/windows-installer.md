# Windows 安裝包發布規劃

狀態：實作完成，本機安裝／發布檢查通過；普通全套 Cursor 逾時與發版前實機矩陣待驗收。日期：2026-11-18。詳見 [驗證紀錄](../../reviews/2026-11-18-windows-installer.md)。

目標：新增 Windows x64 桌面安裝包，作為一般使用者的推薦下載；保留既有便攜 ZIP、Linux server 與桌面自更新 EXE。

## 現況與方案

- 桌面使用 Wails v3 beta.26，但透過 `go build` 建置，沒有 Wails Taskfile／安裝器骨架；production 前端已嵌入 EXE。
- `scripts/release/package.sh` 建置桌面 EXE 一次，ZIP 與版本化更新 EXE 共用相同位元組。
- `cmd/desktop/workdir_windows.go` 依 EXE 旁的設定檔／範例檔決定工作目錄；設定、預設 DB、日誌、附件與 Telegram session 依賴該目錄。
- 既有 updater 會原地替換桌面 EXE，因此安裝位置必須允許目前使用者寫入。

建議新增一份 Inno Setup 腳本，包裝現有已建置的桌面 EXE。沿用既有 Go 建置與草稿發布流程，不為安裝包升級 Wails 或導入另一套建置流程。
Inno Setup 支援以 `PrivilegesRequired=lowest` 執行目前使用者安裝，符合這次需求（[官方說明](https://jrsoftware.org/inno-setup/documentation/topic_setup_privilegesrequired.htm)）。
Wails 官方提供 NSIS 路線，但目前專案沒有其建置骨架，且現行文件不等同於本專案 beta.26 的能力；本輪不遷移（[官方文件](https://v3.wails.io/guides/build/windows/)）。

## 首版行為

| 項目 | 決定 |
|---|---|
| 平台 | Windows x64；實測 Windows 10／11，不增加 ARM64 或其他桌面平台 |
| 產物 | `claude-miniapp-<tag>-windows-amd64-setup.exe`，例如 `claude-miniapp-vX.Y.Z-windows-amd64-setup.exe` |
| 安裝權限 | 目前使用者，無管理員提權；不提供全機安裝選項 |
| 程式位置 | `%LOCALAPPDATA%\Programs\ClaudeMiniApp`，首版固定位置 |
| 資料位置 | `%LOCALAPPDATA%\ClaudeMiniApp\data`，與程式目錄分開 |
| 入口 | 固定名稱 `claude-miniapp-desktop.exe`；開始功能表捷徑、可選桌面捷徑、Windows 已安裝應用程式項目 |
| 安裝內容 | 桌面 EXE、安裝模式標記、授權及必要安裝資產；不包含純 server、原始碼或開發文件 |
| 首次使用 | 由現有設定精靈建立設定；安裝器不寫入密碼、Token 或使用者 DB |
| 更新 | 保留現有程式內 EXE 更新；Setup 用於首次安裝、重裝與手動升級 |
| 移除 | 移除程式、捷徑與解除安裝登錄；保留資料，不提供一鍵刪除資料選項 |
| 便攜版 | 原有 ZIP 及啟動規則保留；不自動轉成安裝模式 |

## 工作目錄與資料安全

1. 安裝器建立固定的安裝模式標記（如 EXE 旁的 `installed.flag`）；桌面入口只有在標記存在時才切到上述資料目錄，不靠路徑猜測。
2. 順序維持 updater helper 最先處理，再建立／切換資料目錄，最後初始化 logging、設定與 DB。建立或切換失敗必須顯示錯誤並停止，不回退到任意目錄寫資料。
3. 安裝模式優先於現有 EXE 旁設定判定；不放 `config.example.yaml` 觸發便攜規則。無標記時沿用便攜與開發行為。
4. 維持相對路徑設計，讓 `config.yaml`、預設 `claude-miniapp.db`、`logs/`、`workspace/`、`internal/static/uploads/`、預設 `telegram.session` 落在資料目錄。使用者指定的絕對路徑不改寫。
5. 確認 Wails WebView2 profile 的實際預設位置；安裝模式採固定、可寫入且不隨版本變更的位置，並驗證更新後登入與本機偏好仍保留。
6. 安裝／升級／解除安裝前，要求從系統匣「結束」程式並停止工作；有執行中程序或檔案鎖時中止操作，不強制結束 agent，也不排程重開機後偷偷替換。
7. 安裝器只管理明列的程式檔，不遞迴清除資料目錄或未知檔案；重裝沿用固定 AppId／安裝位置。禁止直接覆蓋較新版 EXE，尤其須辨識程式已經自行更新的情況。

## 既有便攜使用者

首版不掃描磁碟、不自動搬移或合併資料；便攜版可繼續使用。另提供手動遷移文件，步驟如下：

1. 停止桌面／server 與相關工作，完整備份原資料夾，確保 SQLite 已關閉；不得只複製仍在寫入的 DB 而漏掉 WAL。
2. 安裝後、第一次啟動前，複製設定、DB、`workspace/`、`internal/static/uploads/` 與使用中的 Telegram session 至資料目錄；自訂 DB／session 路徑逐項確認。
3. 檢查設定中的相對路徑與歷史附件／訊息中的絕對路徑。首版不批次改寫聊天歷史，若舊路徑仍被引用，文件須說明保留原檔的限制，不宣稱搬完即可刪除便攜資料夾。
4. 驗證歷史、附件、工作目錄與 Telegram 登入後才完成遷移；失敗則關閉新版，從原便攜資料夾恢復使用。已被新版開啟的 DB 不直接交給舊版程式。

安裝版與便攜版沿用目前單一實例／port 限制，不承諾同時執行；遷移流程也必須避免兩者同時存取同一 DB。

## WebView2 與版本資訊

- 安裝前檢查 WebView2 Runtime；缺少時執行微軟 Evergreen Bootstrapper，驗證來源簽章與執行結果，失敗或離線時給出明確提示，不宣稱環境已就緒。
- 首版不內含完整離線 Runtime；已裝 Runtime 的環境不需要為此下載。檢查與安裝方式依 [Microsoft 官方文件](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)。
- Setup 版本取自 tag／VERSION；EXE 的 Windows 版本資源須與之相符，讓安裝器能攔截降版。
- 程式內更新成功啟動後，同步目前使用者解除安裝項目的 DisplayVersion；失敗只記錄，不影響啟動。設定頁仍以執行中的實際版本為準。
- Authenticode 簽章列為發布準備項：確認是否已有憑證／簽署服務；若採簽章，先簽桌面 EXE，再共用於 ZIP／更新 EXE／Setup，最後簽 Setup 並產生 checksums，避免位元組契約失效。

## 實作順序

- [x] 0. 實作前建立對應 Spectra change，納入 desktop-distribution 與 desktop-self-update 的變更。
- [x] 1. 擴充桌面工作目錄判定與既有測試，涵蓋安裝標記、首次建立、錯誤停止及便攜／開發相容；確認 WebView2 profile 位置。
- [x] 2. 新增 `scripts/release/windows-installer.iss`：固定 AppId、目前使用者安裝、捷徑、Runtime 檢查、執行中拒絕、降版拒絕及保留資料的解除安裝。
- [x] 3. 調整 Windows EXE 版本資源與安裝版更新後版本登錄；重用既有更新器，不下載 Setup 來自更新。
- [x] 4. 在 release workflow 準備固定版本的 Inno Setup；`package.sh` 使用同一份桌面 EXE 產生 Setup，納入 checksums，保留現有三個程式資產。
- [x] 5. 更新 `publish.sh`、release 契約與假 GitHub 發布測試：預期資產由四個改為五個（含 checksums）；驗證後才公開，任何缺檔或不符均留在草稿。
- [x] 6. 更新 Release 下載表、README 與入門文件：Setup 為桌面推薦、ZIP 標示便攜／server、版本化 EXE 保留給自更新；補手動遷移與移除資料位置。
- [ ] 7. 完成下列驗收及規格符合性檢查，再進入發布。

## 發布驗收

| 情境 | 必須觀察到的結果 |
|---|---|
| 標準帳戶乾淨安裝、中文／空白路徑 | 無提權；捷徑及直接雙擊均可進設定精靈；資料只寫到指定位置 |
| WebView2 已存在／缺少／下載失敗 | 已存在即跳過；缺少可補裝；失敗有可操作提示，不啟動失敗循環 |
| 安裝 N → 程式內更新 N+1 | helper 正常替換及重啟；標記、捷徑、設定、DB、附件、登入與偏好保留；解除安裝仍可用 |
| Setup 手動升級／重裝／降版 | 不覆寫資料；較舊 Setup 不覆蓋已自更新的新版；工作中或檔案被鎖就安全中止 |
| 解除安裝 → 再安裝 | 程式與捷徑移除，資料保留；重裝讀回原設定與歷史 |
| 便攜回歸／手動遷移 | 舊 ZIP 啟動與自更新不變；用合成資料驗證歷史及兩種附件路徑，記錄絕對路徑限制 |
| Release 草稿與完整性 | 五個資產齊備；四個程式資產的 SHA256 正確；Setup 安裝出的 EXE 與 ZIP／更新 EXE 相同 |

自動驗證沿用 `cmd/desktop`、`internal/desktopupdate`、`scripts/release/test/` 與 `scripts/e2e/`，新增最小安裝／移除 smoke；Windows 10／11 標準帳戶、Runtime 缺少與系統匣操作另做實機或 VM 驗收，CI 打包成功不等於已驗收。

本輪不做 MSI／MSIX、Store、winget、自啟動、全機安裝、自動資料匯入或更新器重寫；有明確需求時再加。
