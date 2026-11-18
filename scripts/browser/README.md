# 瀏覽器回歸與 BrowserStack

沿用 Python Playwright，使用正式版 Go server、真實 HTTP／WebSocket／SQLite。
每次執行建立暫存設定檔、隨機密碼、臨時埠與假 Claude CLI，不使用個人 DB 或真實模型帳號。

## 本機

需 Go（依 go.mod）、Node.js、Python 3.10+ 與 Edge。

```powershell
python -m pip install -r scripts/browser/requirements.txt
python -m pytest scripts/browser/test_regression.py -q --browser-channel msedge --tracing retain-on-failure --screenshot only-on-failure --junitxml=test-results/local.xml
```

## GitHub Actions

Repository secrets：`BROWSERSTACK_USERNAME`、`BROWSERSTACK_ACCESS_KEY`。
不要將憑證放入設定檔。`browserstack.yml` 只定義平台與診斷選項。

- PR／main push：本機 Edge 回歸。
- `codex/browserstack-*` 分支 push：先本機回歸，再執行 BrowserStack Windows 11 Chrome。
- workflow 合併至 main 後：可用 `gh workflow run browser-tests.yml -f browserstack=true` 手動執行。

雲端由官方 SDK 建立 Local tunnel，沿用相同測試案例；目前一次使用一個平台。
需 Java，CI 安裝 Temurin 21。`browserstack-sdk pytest scripts/browser/test_regression.py` 可在已設定憑證的環境執行。
Actions 保存 JUnit、失敗截圖與 trace 七天；影片及 session 結果到 BrowserStack Automate 檢視。

目前涵蓋登入、串流與歷史、拒絕授權、附件、三種寬度的設定與主題持久化，共七個案例。
尺寸模擬不等於手機真機；Wails 原生流程及 Telegram 容器仍由專用測試驗證。

## UI 視覺檢查

`ui-review` job 會在 Chrome、Edge、Firefox、macOS WebKit、Samsung Galaxy S25 Ultra／Android 15 Chrome、iPhone 17 Pro／iOS 26 Safari 執行。
最多同時執行兩個 UI session；既有 Chrome 回歸可另外使用一個 session。
桌面瀏覽器檢查 1280、768、390、320px 四種寬度；真機保留原生 viewport。

```powershell
# 獨立虛擬環境，不可與 Microsoft playwright 混裝。
python -m pip install -r scripts/browser/requirements-visual.txt
python -m pytest scripts/browser/test_ui_review.py -q --junitxml=test-results/ui-local.xml
```

成功與失敗畫面都會保存於 `test-results/ui/<環境>/<寬度>/`，包含 PNG、實際 UA／viewport／主題與問題 JSON。
檢查登入、列表、深淺色設定、新會話、長標題、草稿、手機工具／模型、權限提示、歷史及分享。
截圖會停用動畫；狀態先等待預期畫面，不用固定 sleep。UI 問題會讓 job 失敗，仍上傳完整 artifact。
截圖必須另做人工視覺檢查；這不是自動像素差異核准，也不能以測試綠燈取代畫面驗收。

真機 iOS 使用支援的 locator 等待與明確狀態斷言；本次 Python bridge 的 `expect(...).to_be_visible()` 在可見登入欄位上回報空白 AssertionError。
macOS WebKit 使用隔離的 HTTPS/WSS 反向代理，避開遠端 HTTP proxy 的明文 WebSocket 握手失敗。代理僅綁定 loopback，使用記憶體中的臨時自簽憑證；只有這個測試 context 允許該憑證。
`python scripts/browser/check_fixtures.py` 驗證代理拒絕非本機上游，並確認失敗報告與擷取輸出的憑證遮罩，CI 也會執行。正式伺服器與授權流程不變。
768px 返回入口與 iPhone 分享內容的修復、版本差異與畫面證據見 [UI 驗收報告](../../docs/reviews/2026-11-17-browserstack-ui.md)。
報告內 `.spectra/design-cache/` 圖片連結只在保存證據的本機有效；GitHub 上請從報告的 Actions 連結下載對應 `ui-*` artifact。
