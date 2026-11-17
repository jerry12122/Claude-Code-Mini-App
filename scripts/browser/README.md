# 瀏覽器回歸與 BrowserStack

沿用 Python Playwright，使用正式版 Go server、真實 HTTP／WebSocket／SQLite。
每次執行建立暫存設定檔、隨機密碼、臨時埠與假 Claude CLI，不使用個人 DB 或真實模型帳號。

## 本機

需 Go（依 go.mod）、Node.js、Python 3.10+ 與 Edge。

```powershell
python -m pip install -r scripts/browser/requirements.txt
python -m pytest scripts/browser -q --browser-channel msedge --tracing retain-on-failure --screenshot only-on-failure --junitxml=test-results/local.xml
```

## GitHub Actions

Repository secrets：`BROWSERSTACK_USERNAME`、`BROWSERSTACK_ACCESS_KEY`。
不要將憑證放入設定檔。`browserstack.yml` 只定義平台與診斷選項。

- PR／main push：本機 Edge 回歸。
- `codex/browserstack-*` 分支 push：先本機回歸，再執行 BrowserStack Windows 11 Chrome。
- workflow 合併至 main 後：可用 `gh workflow run browser-tests.yml -f browserstack=true` 手動執行。

雲端由官方 SDK 建立 Local tunnel，沿用相同測試案例；目前一次使用一個平台。
需 Java，CI 安裝 Temurin 21。`browserstack-sdk pytest scripts/browser` 可在已設定憑證的環境執行。
Actions 保存 JUnit、失敗截圖與 trace 七天；影片及 session 結果到 BrowserStack Automate 檢視。

目前涵蓋登入、串流與歷史、拒絕授權、附件、三種寬度的設定與主題持久化，共七個案例。
尺寸模擬不等於手機真機；Wails 原生流程及 Telegram 容器仍由專用測試驗證。
