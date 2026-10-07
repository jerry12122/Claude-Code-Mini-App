# 故障後建立接手會話

2026-10-07：程式與驗證完成，尚未重啟服務。

## 行為與範圍

- Runner 回 `ErrHandoff` 時，自動建立同供應商的新會話，透過 MCP 讀舊對話；MCP 未啟用、交接首則訊息再次失敗或自動建立失敗時，提供手動入口。取消與一般錯誤不觸發接手。
- `handoff_available` 傳送結構化來源設定、最後需求與原因。擁有者可按「建立接手會話」，沿用既有 `NewSessionComposer`；訪客仍由既有 API 權限拒絕建立。
- 預填名稱、目錄、供應商、權限模式、model、CLI 參數、effort 與可編輯首則訊息。model 同時支援舊 `--model` 引數；更換供應商清除不相容的 model／CLI／effort。共用表單的 ForwardModal 也套用這項清除。
- MCP 可用時請新 agent 呼叫 `get_messages`；否則帶入已載入歷史，最近 20 則、文字共 20,000 字元，優先保留最新尾段並標示裁切。最後需求不裁切。
- 接手提示只存在 App 的 session map：取消表單可再開，切換來源互不混用，新一輪 THINKING 清除。socket hook 不另存副本。
- 建立 API 在回應前儲存 effort，失敗嘗試刪除半成品。首則訊息沿用既有送出／保留草稿流程；自動接手與 MCP 共用同一 Registry。

## 驗證

- Go build／vet／全專案測試、WS race；正式 API 測試驗證 effort 持久化與失敗清理，WS 測試驗證自動接手與手動入口。
- `node poc/manual-handoff-review/check-history.js` 直接執行產品函式，驗證歷史尾段、裁切、上限、完整最後需求與 MCP 分支。
- `poc/manual-handoff-review/browser-check.js` 使用實際 App、socket／表單 hook、建立表單及開啟 handler；桌面 1280×900、手機 375×812 各 17 個斷言，涵蓋取消重開、來源隔離、設定繼承、切換供應商、連點及新一輪清除。網路、任務執行和聊天展示為假資料，未呼叫模型。
- 前端 19 個 JS 檔通過 Babel 語法檢查，沒有新增產品依賴或建置流程。PoC 均留在忽略的 `poc/`。

## 限制與後續

- 整頁重新整理或故障發生時不在線，不會恢復接手提示；TG WebView／真實供應商接手尚待實機驗收。
- 自動接手的連鎖防護僅檢查本輪提示的 marker，不能當作持久化的會話關係；進一步策略仍待決定。
- 保留 permission_mode，不轉移單次 allowedOnce。DB AllowedTools 目前不供 runner 啟動讀取，不能聲稱完整轉移工具授權。
- 額度路由、摘要服務與跨供應商原生上下文轉換不在本次產品實作內；相關實驗見 `poc/agent-handoff/`。
