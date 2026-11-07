# Miniapp MCP 偶發斷線調查

調查日期：2026-11-07。已重現並修正一個會讓 Claude 回報「miniapp disconnected」的 schema 相容性問題。修正僅涉及四個 schema 描述與一個回歸測試，未重啟或替換使用中的 desktop 服務。

## 已驗證的根因與觸發條件

`internal/mcp/tools.go` 的 `askSessionOut` 與 `getStatusOut` 都有 `pending_question`、`pending_permission`，四個欄位型別都是 `any`，沒有 schema 描述。Go SDK 推導後在 `tools/list` 回傳：

```json
{"properties":{"pending_question":true,"pending_permission":true}}
```

依 [JSON Schema 規範](https://json-schema.org/draft/2020-12/json-schema-core#section-4.3.2)，boolean schema `true` 合法且表示不限制內容；問題是部分舊 MCP client 的 schema 驗證不接受這種形式。實際 Claude CLI 2.1.318、2.1.319 在 `MCP_SDK_GENERATION=v1` 時，連線與 initialize 都成功，接著 `tools/list` 驗證失敗，錯誤指向兩個工具的上述四個欄位，重試三次後丟棄整份工具清單。

結果不只影響這兩個工具：`ToolSearch` 找不到任何 miniapp 工具，連唯讀的 `list_sessions` 都不能呼叫。CLI 給模型的提示把這組工具歸為 `MCP server disconnected`；模型因此向使用者回報斷線，雖然初始化狀態仍是 connected。這也解釋為什麼僅檢查 HTTP 連通或 `claude mcp get` 會漏掉問題。

同一個 server、同一個 CLI 版本切成 `MCP_SDK_GENERATION=v2`，工具載入與呼叫成功。已驗證的觸發條件是 client runtime/schema 驗證差異；不是只要 CLI 版本較舊就一定失敗，也沒有證明每次啟動會隨機切換 runtime。

## 重現與修正驗證

測試使用實際 Claude 原生執行檔、原對話的唯讀 fork、原工作目錄，保留 `--mcp-config` 與 permission MCP 的啟動方式。模型 API 由本機 Postman simulation 回傳固定 SSE 工具呼叫，只執行 `ToolSearch` 和唯讀 `list_sessions`。這個驗證矩陣的模型請求均由本機 fixture 處理；沒有要求真實模型替我們判斷成功與否。

| CLI | MCP runtime | 修正前的實際服務 | 修正後的隔離服務 |
| --- | --- | --- | --- |
| 2.1.318 | v1 | tools/list 驗證失敗、搜尋無結果、工具不能呼叫 | 搜尋與呼叫成功 |
| 2.1.318 | v2 | 成功 | 成功 |
| 2.1.319 | v1 | tools/list 驗證失敗、搜尋無結果、工具不能呼叫 | 搜尋與呼叫成功 |
| 2.1.319 | v2 | 成功 | 成功 |

修正前 v1 的兩次模型請求都包含 `mcp__miniapp__* (18)` 被列為 disconnected 的新提示；v2 對照與修正後四個案例的新提示都沒有移除 miniapp。這是與回報現象相符的重現，不只是一個人為製造的 TCP 斷線。

修正為四個 `any` 欄位加上 `jsonschema` 描述。序列化因此變成帶 description 的 schema 物件，仍保留任意 JSON 內容，不改變工具輸出資料，也不移除必要的權限或問題資訊。

新增 `TestToolsListPendingSchemasUseObjects`：經正式 `NewHTTPHandler`、真實 HTTP 與 Go SDK `ListTools`，驗證四個欄位在實際工具清單中都是 schema 物件。修正前四處都因 `got true` 失敗，修正後通過。原有 `TestStatusOutputSchemaAcceptsPermissionObjects` 也通過，確認實際權限內容仍可通過輸出驗證。

原始證據留在本機忽略目錄 `.miniapp/mcp-startup/`：`handshake-results.json`、`handshake-*.log`、`fixed-schema-results.json`、`fixed-schema-*.log` 與 `simulation-call.ndjson`。simulation 記錄包含原對話內容，不應加入版本控制或分享原始檔。

## 與實際歷史事件的對照及限制

在本機 Claude 對話紀錄找到 2026-11-04 13:58（台灣時間）事件：使用者要求透過 miniapp 聯繫另一段對話，CLI 2.1.318 在啟動約 5.2 秒後移除全部 18 個 miniapp deferred tools，發出「their MCP server disconnected；Do not search for them」提示，assistant 隨後回報無法連上。該提示的 failed/pending server 清單都沒有 miniapp。

同一對話當天多次啟動在工具消失與恢復之間交替；現有服務紀錄沒有顯示該時段的服務重啟。四個 `any` 欄位從 2026-10-16 的 `3b3228a` 就存在，早於這次事件。

上述特徵與已重現的 schema 問題相符，尤其是「initialize connected，但工具全數不可用、failed 清單沒有 miniapp」。不過歷史紀錄沒有保存當次 runtime 選擇或 tools/list 原始錯誤，因此無法斷言 11/4 必然同因，也不能把所有使用者回報都歸為這一個原因。現有證據不足以確認所有回報是否同因。

## 其餘調查結果

- 修正前兩個 CLI 版本各三次一般啟動均成功，因此版本號本身不是充分條件。
- 啟動時顯示 pending 的 v2 案例仍成功呼叫工具，不能把 pending 直接當成斷線原因。
- 正式 HTTP handler 的 7,200 次並行查詢、30 次取消後重用連線，以及實際 LAN 服務 30 次查詢均成功；這些有界量測不能排除其他網路或長時間問題。
- 另重現服務重建後舊 MCP session 失效；這是不同的觸發條件，沒有證據把它套到 11/4 事件。MCP 規範要求 client 在 session 404 後重新 initialize。
- GET 405 在 MCP 規範下可合法存在，不據此修改路由；DELETE 405／session 清理風險不在本次修正範圍。

探索過的 HTTP／CLI 診斷紀錄與 transport 壓力測試未納入最終變更，避免把與已驗證根因無關的改動一起留下。

## 驗證及重跑

```powershell
go test ./internal/mcp -run '^(TestToolsListPendingSchemasUseObjects|TestStatusOutputSchemaAcceptsPermissionObjects)$' -count=1
go test -race ./internal/mcp -timeout 90s
go test ./... -timeout 180s
go test -tags production ./internal/mcp ./internal/server -timeout 90s
go vet ./internal/mcp
```

以上最終檢查全部通過：`go test ./...`、MCP race 測試（11.157 秒）、production 標籤的 MCP/server 測試與 `go vet`。調查階段未部署或自動重啟使用中的服務。

## 參考

- [Claude MCP runtime 選項](https://code.claude.com/docs/en/env-vars.md)：`MCP_SDK_GENERATION` 的 v1／v2 對照方式。
- [MCP session 管理規範](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management)：區分工具清單相容性問題與 session 失效。
