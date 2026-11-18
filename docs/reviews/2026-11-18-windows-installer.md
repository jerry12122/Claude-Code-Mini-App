# Windows 安裝包實作與驗證

變更：`windows-installer`。本機 Windows 11 Pro 10.0.22631，非提權程序；Inno Setup 6.7.1。程式及 Setup 延續既有未簽章發布方式；尚未建立 Release 或 tag。

## 交付內容

- 目前使用者安裝、開始功能表及選用桌面捷徑、固定安裝位置、獨立資料／WebView2 profile。
- 桌面／維護雙 mutex 及舊便攜版 Wails 單一實例鎖檢查、鎖檔拒絕、未知／零版本與較新版 EXE 拒絕覆蓋；解除安裝保留資料及未知檔案。
- 微軟官方 WebView2 bootstrapper 的下載及 Authenticode 驗證，Runtime 缺少才補裝；建置或安裝失敗阻擋後續操作。
- VERSION 產生 Windows 四段版本資源，Setup、ZIP 與自更新資產共用同一份桌面 EXE；五資產草稿發布，checksums 精確涵蓋四個程式資產。
- 中英安裝／遷移文件；不新增 Go 依賴、不升級 Wails，不自動搬移便攜資料。

本機測試產物：`dist/claude-miniapp-v0.20.1-windows-amd64-setup.exe`，SHA256 見 `dist/checksums.txt`；Windows EXE FileVersion／ProductVersion 均為 `0.20.1.0`。此為目前工作樹的測試包，不代表已發布新版本。

## 已執行驗證

| 命令／檢查 | 結果 |
|---|---|
| go generate ./cmd/desktop；go test ./cmd/desktop | 通過，含 Windows 圖示與版本資源、安裝目錄／失敗、便攜、mutex、登錄測試 |
| go test ./internal/desktopupdate | 通過，含標記存在時真正 helper 程序啟動不建立安裝資料 |
| RELEASE_DIST=dist 對 TestReleaseDistContract | 通過，四個 SHA256、ZIP EXE 位元組及 updater 不選 Setup |
| bash scripts/release/test/test-release.sh | 46 通過、0 失敗；只呼叫假 gh，缺 Setup／毀損 Setup／漏 Setup checksum 均拒絕公開；不受信任 prerequisite 在任何程式資產產生前失敗 |
| installer-smoke.ps1 -DesktopExe dist 中的桌面 EXE -AppVersion 0.20.1 | 通過，實際 Inno 安裝／移除，產品 ID、路徑、mutex、Runtime 登錄均隔離 |
| installer-smoke.ps1 預設合成 EXE | 通過；降版含 major 60000、零／未知版本、五種檔案鎖、雙 mutex、捷徑、資料及未知檔保留、Runtime 失敗／重新檢查／跳過、無簽章／錯誤簽署者拒絕 |
| 官方 bootstrapper 下載與嚴格簽章檢查 | 通過：Valid、Microsoft Corporation；打包時重驗同一個檔案 |
| go vet ./cmd/desktop ./internal/desktopupdate；git diff --check | 通過 |
| go test -tags production ./internal/... ./cmd/... | 最後依序執行通過；JSON 結果位於 dist/tests-production.json |
| go test ./internal/... ./cmd/... | 未全綠：TestRunReturnsWhenOrphanHoldsStdoutAfterResult 於全套執行逾時；其餘套件通過，結果位於 dist/tests-normal.json |
| go test ./internal/cursor；該失敗案例單獨重跑 | 均通過；本次未修改 Cursor。最初並行執行時 server production 亦失敗，單跑與最後 production 全套均通過 |

最初以佔位行為確認新路徑、mutex、registry 測試失敗；Windows 版本資源測試先因缺資源失敗。發布器另外確認了漏 Setup checksum 仍接受的失敗案例，再修為任何 GitHub 呼叫前拒絕。安裝 smoke 也抓出 signed packed version 對 major 60000 的誤判，已改逐段比較且通過。舊便攜版沒有新 mutex 的重現案例先確認會錯誤接受安裝，再補 Wails 既有單一實例鎖檢查。

## 規格情境與測試對應

| Scenario | 自動驗證／缺口 |
|---|---|
| Assemble and publish installation assets | TestReleaseDistContract、test-release.sh |
| Missing or corrupt Setup | test-release.sh 的缺檔、遠端毀損及漏 checksum |
| First installed startup | TestInstalledWorkDir、TestSetupSaveWritesConfig；實際桌面精靈啟動仍待實機整合驗收 |
| Invalid data location | TestInstalledWorkDirFailsClosed |
| Portable startup | TestUnmarkedWorkDir、TestWorkDirForFirstRun |
| Install and remove | installer-smoke.ps1 的真實 Inno 安裝／移除與保留檔案斷言 |
| Running or newer application | installer-smoke.ps1 的 mutex、鎖定、60000／零／未知版本拒絕 |
| Launch during maintenance | TestDesktopMaintenanceMutex |
| Runtime already present | installer-smoke.ps1 的 Runtime 存在時跳過失敗 bootstrapper |
| Runtime absent or installation fails | installer-smoke.ps1 的隔離登錄與合成 bootstrapper；真實缺 Runtime 環境待驗收 |
| Untrusted prerequisite | prepare-webview2.ps1 對無簽章／錯誤簽署者失敗，官方檔案成功 |
| Installed desktop self-update | TestDesktopUpdateFlow、TestInstalledVersionRegistry、TestInstalledWorkDir 覆蓋組件；完整安裝 N → N+1、profile／登入保留為整合測試缺口 |
| Registry unavailable or installation absent | TestInstalledVersionRegistry 的既存／缺少 key 與錯誤分支 |
| Helper process | TestDesktopUpdateHelperStartup 真正子程序；installed.flag 存在也不得建立資料 |

TDD 範圍排除：單一文件的文字呈現，以五檔內容審查與 46 個本機檔案連結檢查驗證。上述實機整合缺口不是排除項，不宣稱已通過。

## 發版前待驗收

- Windows 10 與乾淨標準帳戶、中文／空白使用者路徑的完整設定精靈及日常操作。
- 真正沒有 WebView2 Runtime 的機器，含可連網、離線／補裝失敗及 HKLM 安裝既存 Runtime。
- 安裝 N → 程式內更新 N+1 → Setup 重裝／移除：profile、登入、偏好、DB、附件與 DisplayVersion 一起保留。
- 用合成聊天及兩種附件實跑手動遷移；絕對路徑不得批次改寫，仍引用原檔者保留原資料夾。
- 確認普通全套的 Cursor 逾時測試穩定；補齊實機驗收後再考慮 archive 與正式發布。

## 提交前驗證與審查

`spectra-verify`：5/5 任務及五項 requirement 均有實作；14 個 scenario 的測試對應見上表。沒有缺少實作的 Critical；首次桌面精靈、真實 Runtime 補裝、安裝版跨版更新仍為 Warning／not-run，未以組件測試宣稱完整驗收。Delta specs 沒有 Example 區塊。

`spectra-review`：以 touched_tracking 選出的 28 個檔案及變更 artifacts，檢查正確性、效率、簡化重用與專案慣例，沒有需修正的程式缺陷。審查基底 `391c7fc` → `dbc49fc` 加目前工作樹；snapshot `edef7e4308772a710108cf946f3072d3cb68c926` 已確認未變動。二進位資源不以文字審查，改用 Windows 圖示／版本測試驗證；共用 README 僅有本次安裝包差異，`docs/plan/todo.md` 的 Supervisor 既有修改不納入本次提交。

提交前重新執行 `go test ./cmd/desktop ./internal/desktopupdate`、`go vet ./cmd/desktop ./internal/desktopupdate`、workflow 結構檢查與 `git diff --check`，結果均為 passed-current。46 項發布測試、真實 EXE 安裝 smoke 與 production 全套採用本次對話先前的 passed-prior 記錄，相關程式／測試／設定內容相同；普通全套先前失敗與實機缺口仍保留。可以提交實作，不代表允許略過發布驗收；本次不 archive、不 push、不發版。

本次不新增程式簽章服務。CI 工具由 [Inno Setup 官方 6.7.1 release](https://github.com/jrsoftware/issrc/releases/tag/is-6_7_1) 下載並固定官方 release asset 的 SHA256；WebView2 依 [Microsoft 的部署與 Runtime 登錄說明](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution)。

## 測試診斷殘留

自動核准審查拒絕清理先前失敗的隔離測試資料，理由僅為 `blocked by policy`；保留 `D:\Temp\ClaudeMiniAppSmoke-*` 診斷，以及 `ClaudeMiniAppSmoke-359b9c6b29714b6ea6f734e6db17c783` 的隔離測試登錄／捷徑。正式應用及使用者資料未受影響；後續成功 smoke 已自行清理。
