## Why

目前 Windows 桌面版只有便攜下載，使用者須自行解壓及建立入口，且程式與資料混放。新增免提權安裝包，提供固定入口與安全的升級／移除行為。

## What Changes

- 新增目前使用者的 Windows x64 Setup，保留便攜 ZIP、Linux server 與既有桌面更新 EXE。
- 安裝標記使桌面啟動切至獨立資料目錄；保留便攜與開發啟動規則。
- 安裝器處理 WebView2、捷徑、版本與鎖定檢查；移除及升級保留資料。
- 沿用 EXE 自更新，同步安裝版解除安裝項目的版本。
- Release 五個資產驗證後才公開；補遷移文件與安裝 smoke 驗證。

## Capabilities

### New Capabilities

無。

### Modified Capabilities

- `desktop-distribution`: 新增目前使用者安裝、獨立資料、Runtime 與五資產發布契約。
- `desktop-self-update`: 安裝版沿用原地更新並在成功啟動後同步登錄版本。

## Impact

- cmd/desktop、scripts/release、.github/workflows/release.yml、.github/release-header.md。
- README.md、README.zh-TW.md、docs/getting-started.md、docs/getting-started.zh-TW.md、docs/plan/todo/windows-installer.md。
- 建置需要固定版本 Inno Setup 及既有 windres；不新增 Go runtime 依賴，不升級 Wails。
