# MiniApp PM skill 撰寫指南

依據 main `48c20c65ad2fc583bf31b0ae4eac701cfd9c86ed` 的工具契約整理。這是下一版來源的指南；已安裝的舊服務可能沒有 version、title 或 intent 欄位，使用前先看實際工具 schema。本文不會安裝 skill、改設定或啟動服務。

## 1. 先決定 skill 的責任

skill 要定義你想要的工作方式：PM 是否直接做小修、如何拆工、選 worker、何時請你決策、怎樣驗收與整合、何時保存交接。

平台提供工具、資料與部分守門。skill 載入不會建立工作室、不會把會話設為 PM，也不等於該執行器已有 MCP 連線。先在介面建立工作室、指定 PM，再確認該 PM 能讀取本室看板。程式目前有 Claude 的 PM 專用 bridge；其他執行器依實際工具設定確認，不能假設全部自動接好。

可用這張表劃分責任：

| 平台能檢查 | skill 仍要決定 |
| --- | --- |
| 現任 PM、成員資格、閒置與部分佇列條件 | 誰適合做、是否要換人、如何拆工 |
| 指定 MCP 寫入缺版或過期拒絕 | 新狀態下原計畫是否仍成立 |
| 正式 job 自動回報與結構化完工去重 | 交付是否達標、要不要退修 |
| 指定清理操作的使用者批准與狀態重驗 | 要清哪些目標、成果是否已保存 |
| 工作標題、指示、結果快照 | 哪些證據仍有效、要保留哪些限制 |

board version 只涵蓋 DB 工作流，不是工作目錄的 commit/hash 或不可變交付封存。程式也沒有把 worker done 自動轉成「PM 驗收通過」。不要把這兩件事寫成已由平台代辦。

## 2. 檔案與載入

整包範例只有三個檔案：

```text
miniapp-pm-example/
  SKILL.md
  references/
    pm-core.md
    authoring-guide.md
```

`SKILL.md` 的 YAML frontmatter 需要 `name` 與 `description`。description 說明何時使用；正文放核心決策；較長的工具契約在需要時才讀本指南。可直接修改範例，將資料夾與 name 改成自己的名稱。

MiniApp 自動短提醒需要 **SKILL.md 與 references/pm-core.md 兩份非空檔案**。服務先讀環境變數 `MINIAPP_PM_SKILL` 指向的資料夾；未設定時，從服務使用者的 `~/.claude/skills/supervisor-workflow` 尋找，找不到短檔時再嘗試 `~/.agents/skills/supervisor-workflow`。不是自動搜尋所有已安裝的 skill。

例如，在你原本的啟動方式啟動服務之前，讓該服務行程繼承：

```powershell
$env:MINIAPP_PM_SKILL = 'D:\Skills\miniapp-pm-example'
```

此路徑是**服務行程共用設定**，目前不是每個工作室各選一份 skill 的 UI。需要不同工作室策略，可在其 PM 專屬指令與備忘中指定；共用短提醒不要寫死某個 room、worker 或個人模型偏好。修改啟動環境不會自動變更已執行中的服務；服務更新另依使用者授權安排。

每輪短提醒帶完整 skill 路徑與版本。版本由 `pm-core.md` 加 `SKILL.md` 產生；任一份改動都會變，**只改本指南等其他 reference 不會改該版本**。重要流程變更也要同步核心檔的必要說明。首次、接任、版本不同或上下文缺少紀錄時讀全文；同版沿用，避免每輪重讀。短提醒不會取代執行器自己的 skill 發現機制，也不保證模型已讀全文；PM 與 worker 必須能讀到相關檔案。

## 3. 工具名稱先對實際連線

工具前面可能另有執行器產生的 namespace。以下列的是 server 註冊名稱：

| 用途 | PM 專用 MCP `miniapp_room` | 全域 MiniApp MCP |
| --- | --- | --- |
| 看板 | `get_board` | `room_get_board` |
| 近期工作室訊息 | `get_history` | `room_get_history` |
| 保存備忘全文 | `save_memo` | `room_save_memo` |
| 建立 worker | `create_worker` | `room_create_worker` |
| 正式派工 | `dispatch` | `room_dispatch` |
| 會話改名 | `rename_session` | `room_rename_session` |
| 額度快取 | `get_quota` | `get_quota` |
| 重置本室成員 | `reset_session` | 沒有對應同名 room 工具 |
| 刪除此工作室 | `room_delete` | 沒有對應同名 room 工具 |

PM 專用連線綁定現任 PM 與工作室，通常可省略 room_id；全域 room 工具請明確帶 room_id。全域另有 `send_message`、`ask_session`、`post_message`、`get_status`、`get_queue`、`resume_queue`、`delete_session` 等工具；它們不是 PM 專用 server 的完整能力。工具不可用時說明缺少的能力，不自行猜名稱、組 token 或換 raw SQL 繞過。

## 4. 先讀看板，再做變更

get_board 回傳 version、room_id、memo、members、jobs；部分 MCP 客戶端收到的是文字區塊內的 JSON。成員包含 session_id/role/status，job 包含 id/title/task_text/worker_id/worker_name/status/result_text/result_saved/result_message_id/worker_exists。

- session_id 識別會話，job_id 識別當次工作。名稱可改，不能當主鍵。
- worker_name 是派工當時的名稱快照；新名稱不應改寫舊工作。
- 看板目前最多 50 筆工作、進行中優先；不是完整歷史匯出介面。結果摘要最多 16,000 字元，不能拿摘要當完整原始 log。
- get_history 只取近期訊息，文字可能截斷。需要精確成果時讀原交付檔與版本。

MCP 的 save_memo、create_worker、dispatch、rename_session 需帶最新 expected_version。每次成功變更後，下一次寫入重新讀看板。不要連續使用同一舊版本；也不要在衝突後只換版本、跳過重新判斷。

派工參數範例（將角括號內容換成實際值；以可用連線呼叫 dispatch 或 room_dispatch）：

```json
{
  "room_id": "<ROOM_ID>",
  "session_id": "<閒置WORKER_ID>",
  "title": "修正登入後返回原頁",
  "text": "目標：保留登入前路由。允許範圍：本任務指定檔案。先確認目前行為，再做最小修正；交付改檔、原始碼版本、檢查結果與限制。最後答覆由系統回報，不另手動通知完工。",
  "expected_version": "<最新看板version>"
}
```

title 必填，1–120 個 Unicode 字元、單行，與會話名稱無關；text 為 1–64,000 bytes。成功回 job_id，只表示正式工作已建立。

建立 worker 需 name、可選 agent_type、expected_version；會沿用 PM 工作目錄與目前全域指令快照。不要假設自帶隔離工作樹或你的個人權限模式。依使用者政策檢查工作目錄、執行器與權限，再派工。

save_memo 的 text 是完整備忘，最多 32,000 bytes。寫入前讀現值，只保存當前目標、job/來源版本、有效驗證、阻塞、下一步，不無限附加日誌或秘密。

get_quota 是快取，percent 表示已用比例。stale、未知時間或「歷史估計」不是即時剩餘額度；skill 要自行訂可接受的風險與換人策略，不能將示例百分比變成所有人的固定政策。

## 5. worker 提問與完工是不同協定

正式 worker 的最後答覆由 Finish→Deliver 流程自動送給目前 PM。同一 job 只有一份最終交付，修正版本或完工後退修再開新 job。請在派工文字交代這件事，worker 不會因為加入工作室就讀到 PM 的全部 skill。

非同步提問用全域 send_message：

```json
{
  "session_id": "<目前PM_ID>",
  "from_session_id": "<WORKER_ID>",
  "intent": "question",
  "job_id": 123,
  "text": "指定檔案與現行目錄不同，應以哪個為準？"
}
```

正式 worker 向現任 PM 漏來源、意圖或必要 job_id 會被拒絕。工作完成後仍可用原 job_id 提新問題。同步 ask_session 本身是明確詢問，目標需閒置；互問要提供 from_session_id，可能等待使用者或逾時，不用它另送完工通知。

若既有整合確實需要結構化完工確認，send_message 可使用以下欄位；text 保留為明確說明：

```json
{
  "session_id": "<目前PM_ID>",
  "from_session_id": "<WORKER_ID>",
  "intent": "completion",
  "job_id": 123,
  "text": "本工作最終結果由自動回報交付。"
}
```

這只確認自動入口，不會把 text 當第二份交付或另喚醒 PM；不會替正在跑的工作強制完工。舊相容欄位 completion_job_id 與上面的 completion/job_id 二擇一，不能和 question 混用。一般使用只留最後答覆即可，不需要多呼叫一次。

### 任務完成通知（可選）

由 skill 決定何時通知、文字寫什麼。MCP 不讀 skill、不判斷整份任務是否完成，也不把 `post_message` 限制成只能在結束時呼叫。沒有新的自動 event hook；不呼叫就不會通知。使用者可自行改時機與文字。

要通知時用全域 `post_message`（名稱以實際連線為準）：

```json
{
  "session_id": "<自己的PM_ID>",
  "text": "登入返回已驗收。剩一項：真機未測。",
  "author": "PM",
  "notify": true
}
```

`author` 可省略。這裡的 `session_id` 選自己的 PM 會話，只是本範例要把紀錄留在 PM 對話；工具會寫入你給的那個既有會話，程式沒有「只能寫 PM」的限制。前後空白會去掉，其餘文字原樣寫進氣泡。`notify` 為 true 且服務已有 Bot token 時，同一段文字透過既有 Telegram 設定，送給目前已註冊的使用者。

回傳 `ok` 只表示這則聊天紀錄寫入成功。通知函式回錯、沒有 token，或沒有註冊使用者時，寫入仍回 `ok`，結果不會標明推播失敗，也不會重試。`notify` 為 false 或省略時只寫會話、不推播。寫入不等目標那一輪結束，也不啟動它的 agent；對方正在執行時也可以寫。停用的 agent 類型仍會被現有寫入檢查拒絕。

這次呼叫不額外操作任務，也不另做已讀或未讀標記。它留下一則正常訊息（預設角色寫成 claude、狀態 done，`is_read` 維持新訊息的未讀）。非 Telegram 會話的未讀數會把這種訊息算進去，所以可能增加未讀計數。不要把 `ok` 理解成已讀數不變，也不要假設另有手動未讀標記可清掉它。

不要用這次呼叫代替正式 job 的自動完工回報，也不要用它再喚醒正在跑的 worker。

### done 還不是驗收通過

queued/running 表示待執行／執行中；blocked/slow 是注意訊號，不等於失敗；done/failed/interrupted 是執行結果。PM 自行核對需求、來源版本與有效證據，再在 memo 記錄驗收或退修；目前沒有獨立的「accept job」工具。

驗收至少能回答：改的是哪個版本、驗證了什麼、沒有驗證什麼。新原始碼、測試、依賴或環境使證據失效時只補相關檢查。不要為了收到重複回報、切換階段或收尾再跑同版全庫。

send_message 在對方忙碌時會排成下一輪，**不是保證即時修改當前執行計畫的通道**。已完成後的工作或退修改用正式 dispatch；不要堆多筆補充，讓它們在交付後又改掉已凍結成果。若只是提問，先判斷是否真的需要喚醒對方。

## 6. 清理與錯誤處理

| 狀況 | skill 應做什麼 |
| --- | --- |
| expected_version 缺少或過期 | 重新讀看板、核對差異，再決定是否送出新請求 |
| PM 交接或範圍拒絕 | 核對目前角色及室別；不帶舊 PM 決策繼續寫入 |
| worker 忙碌、有 queue、等授權 | 不重派、不無條件解除暫停；保留原工作並確認原因 |
| failed/interrupted | 先查部分成果與副作用，必要退修另開 job |
| 回報是相同 job 的同一交付 | 不再驗收或回覆「收到」 |
| 缺確認、取消或確認逾時 | 保留資料，向使用者說明未執行 |

清理先說明具體目標、動作、影響及備份，取得該清單確認。PM 專用 reset_session/room_delete 還需 expected_version，server 會向 PM 會話的使用者面板詢問，批准綁定當次範圍與狀態、五分鐘單次有效。模型不可代答；服務需要能連到該確認面板。

全域 delete_session 另外需要 session_id、room_id、pm_session_id、expected_version，仍走使用者確認。它不是繞過本室／共用／busy 檢查的替代入口。沒有該工具就請使用者用現有介面處理，不自行造一個 room_delete 別名。

reset 清的是 MiniApp 續接與資料，不代表清掉 provider 本機歷史或額度；刪工作室會刪本室關係、memo、jobs與室內佇列，並不刪所有成員會話。個別操作後果以工具顯示的確認清單為準。

from_session_id/intent 是協定宣告，不是 OS 身分證明。共用 owner 憑證、無限制 shell/raw SQL 或故意把通知宣告成 question 不會被這套工具完全隔離；不要在 skill 承諾無法繞過的安全邊界。

## 7. 節省脈絡與驗證你的 skill

核心檔只放經常需要、會改變決策的規則；詳細契約按需讀。Memo 保存狀態而非逐輪全文；worker 用一份最後交付；不為維持快取送保溫訊息。相同工作包可重用會話，換模組、角色或反覆沿用過期假設時提供短交接；沒有固定幾輪就自動刪除的規則。

先檢查 YAML、相對連結及 JSON 範例；行為驗證放在隔離測試環境，依需要驗這些情境：

- 新 PM 能從看板找正確 worker，取得新版本後派工，留下 job_id。
- worker 改名後仍對得上原工作；結果摘要不足時會找原始證據。
- 過期版本、漏標識訊息與重複完工不造成未預期寫入；正常詢問仍可送。
- worker 回 done 但驗證失敗時，PM 退修新 job，沒有宣稱完成。
- 沒有清理確認或工具不可用時保留資料，沒有改走 shell 刪除。
- 自訂通知只在 skill 決定要發時呼叫 `post_message`；`ok` 只代表會話已寫入，沒有 token、沒有收件者或推播失敗時不當成通知成功。留下的正常訊息可能增加該非 Telegram 會話的未讀計數。

本範例已做格式與原始碼契約核對；未替你建立工作室、安裝技能或在真 worker 上實跑。不同執行器的工具與檔案可讀性仍需在自己的環境確認。

原始碼查核位置：`internal/ws/pm_prompt.go`、`internal/ws/claudeperm.go`、`internal/mcp/room.go`、`internal/mcp/tools.go`、`internal/mcp/server.go`、`internal/db/room_workflow.go`。工具 schema 或來源版本改變時，重新核對對應章節即可。
