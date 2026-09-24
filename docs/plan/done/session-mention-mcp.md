# Session @mention 與 MCP 諮詢署名

## 目標
讓目前 session 的 agent 能用既有 miniapp MCP，向 `@` 標記的其他 session **詢問／討論**。這不是 Forward（把舊訊息轉寄過去）。

## 範圍
- MCP `send_message`：選填 `from_session_id`；有帶則蓋上自介 header + 回覆署名 footer
- Web 輸入框 `@` 選單：插入 `@[名稱](session:<uuid>)` token
- 送出時前置 `[miniapp] self / mention`，指示當前 agent 用 MCP 提問

## 行為
- `@` 開啟選單；選完後文字區清掉 `@query`，在輸入框**上方**顯示可移除 chip（不塞 uuid）
- 送出時依 chips 前置 `[miniapp] self / mention`，指示當前 agent 用 MCP 提問
- 按送出不會自動 ping 對方
- 接收方看到的 user 訊息含 `from_session_id` 與「你是 session_id」，後續可 MCP 回嘴
- shell 模式不開 mention／chips；slash `/` 與 `@` 不同時開
- Forward modal 不變

## 檔案
- `internal/mcp/envelope.go`、`envelope_test.go`、`tools.go`
- `internal/static/js/chat/MentionMenu.js`、`ChatView.js`
- `internal/static/index.html`、`AGENT.md`
