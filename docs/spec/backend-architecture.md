# 後端架構規範 (Backend Architecture)

本文件是 `internal/` 與 `cmd/` 的分層、依賴與命名約定。規則以 `internal/arch/arch_test.go`
自動驗證（會被 `go test ./...` 一併執行），既存違規列在該檔的 `knownViolations` 與本文第 8 節，
**例外只能減少，不得新增**。

`poc/` 是拋棄式探針，不受本規範約束，也不納入檢查。

---

## 0. 前提（不得更動）

這些是 `CLAUDE.md`「架構決策」與 `CONTRIBUTING.md`「Core Architectural Principles」的延伸，
改動需要先改那兩份文件：

* **單一 Go 二進位**：API、WebSocket、背景任務與靜態資源同一個執行檔，不分離部署。
* **每條訊息 spawn 一個子進程**，用後即棄，不使用 PTY。
* **WebSocket 單一雙向連線**，不用 SSE。
* **多 Agent 平等**：功能不得綁定單一 CLI。新增能力若只有某個 agent 支援，
  必須在 `agent` 層留下共用語意（例如 `RunOptions.ExtraArgs` 的 key），不得在上層寫 `if agentType == "claude"` 的分支堆。
* **不新增執行期依賴**：能用標準庫或既有依賴解決的，不加 `go.mod` 條目。

---

## 1. 分層與依賴方向

```
L8  cmd/server · cmd/desktop                    進入點
L7  server                                      組裝與路由
L6  api                                         HTTP handler
L5  mcp · tgclient                              其他對外介面
L4  ws                                          回合編排與 WebSocket 傳輸
L3  clicheck                                    runner 探測
L2  claude · codex · cursor · kiroacp ·
    antigravity · kiro                          Agent runner
L1  model · quota · shell · gitinfo · opener     領域工具
L0  agent · db · config · auth · tg · media ·
    usage · proc · logging · static ·
    version · desktopupdate                     基礎
```

**依賴只能指向嚴格更低的層。** 同層套件之間也不得互相 import——需要共用就下沉到更低的層。
L0 套件不得 import 任何其他內部套件。

這個分層不是憑空畫的，它就是目前 `go list` 實際的依賴圖。規則的作用是把現狀鎖住，
讓之後的新邊必須是刻意的決定，而不是順手 import。

**新增套件必須在 `arch_test.go` 的 `layerOf` 登記層號**，否則檢查失敗。
這是刻意的強迫機制：決定層號就是決定這個套件能依賴什麼、能被誰依賴。

測試檔不受本節約束。`internal/server` 的測試 import `api`、`ws` 是正常的，
跨層組裝的整合測試本來就該放在高層套件。

---

## 2. 各層職責

### L0 基礎

| 套件 | 職責 | 不放 |
|---|---|---|
| `agent` | `Runner` 介面、`Event`／`RunOptions` 等跨 runner 共用型別、runner 註冊表 | 任何具體 CLI 的實作 |
| `db` | SQLite 開啟、schema 遷移、所有 SQL | 業務規則、HTTP／WS 概念 |
| `config` | YAML 設定讀寫 | 設定的使用邏輯 |
| `auth` | web session token、IP／CIDR 判定 | 路由與 handler |
| `tg` | Telegram Bot API（`initData` 驗簽、推播） | Telegram 使用者帳號登入（在 `tgclient`） |
| `media` | 附件存檔與路徑計算 | 附件的 DB 紀錄（在 `db`） |
| `usage` | CLI 用量輸出的解析與正規化 | 取得用量的程序呼叫（在 `quota`） |
| `proc` | 子進程屬性與樹狀終止（平台差異） | 具體要跑哪個命令 |
| `logging` | slog／zap 初始化、日誌環形緩衝 | 業務日誌內容 |
| `static` | 前端資源的 embed、bundle 與 handler | 頁面邏輯 |
| `version`、`desktopupdate` | 版本字串、桌面自我更新 | — |

**`db` 是唯一能寫 SQL 的地方。** 其他套件只能呼叫 `db` 的方法；
唯一允許外洩的 `database/sql` 符號是 `sql.ErrNoRows`（用於 `errors.Is` 判斷找不到資料）。

### L1 領域工具

無狀態的解析與計算：`model`（model 名稱解析與顯示）、`usage` 之上的 `quota`（抓取與快取額度）、
`shell`（host shell 執行與允許清單）、`gitinfo`（分支、狀態查詢與簡單寫入操作）、`opener`（在主機開啟檔案總管／VSCode）。

這層可以 spawn 子進程查詢外部狀態，但**不得持有 DB**，也不得知道 session 的存在。

### L2 Agent runner

每個 CLI 一個套件，實作 `agent.Runner`，並在 `init()` 以 `agent.Register` 註冊。

* **註冊是唯一的對外接線方式。** 不得讓 `agent` 反向 import 具體 runner。
* **runner 不得 import `ws`**。runner 的輸出是 `agent.Event`，由上層決定怎麼廣播與寫庫。
  目前 `tgclient` 違反此條（見第 8 節）。
* 子進程啟動一律帶 `proc.SysProcAttr()`，並在 `RunOptions.OnStart` 回報 PID。
* 單檔建議拆成 `runner.go`（旗標組裝與執行）、`events.go`（事件解析）、`models.go`（model 列表）。

### L3 `clicheck`

偵測各 CLI 是否可用。它 import runner 套件取得 binary 名稱，所以必須在 runner 之上。

### L4 `ws`

回合編排：接收使用者輸入、呼叫 runner、套用事件、改狀態、寫庫、廣播、處理授權與佇列。
這是系統的核心協調層，也是目前最需要拆解的地方。

新增回合行為時：**先問能不能放在 `applyRunEvent` 既有的事件套用路徑裡**，
而不是在 `NewHandler` 的巨型 closure 再加一個 `case`。

### L5 `mcp`、`tgclient`

建立在 `ws` 之上的其他對外介面。`mcp` 讓外部 agent 操作 session；
`tgclient` 以使用者帳號同步 Telegram 聊天。兩者不得互相 import。

### L6 `api`

HTTP handler。職責是**解析請求、呼叫下層、組出回應**。

* handler 以 struct 持有依賴（`db`、`quota` 等），用方法當 Fiber handler，不用套件層全域變數。
* 不得 spawn 子進程（目前 `api/open.go` 違反，見第 8 節）。
* 驗證全部在這層做完：路徑、長度、型別、權限。下層假設輸入已驗證。

### L7 `server`

組裝：讀設定、開 DB、建 handler、註冊路由、啟動與關閉。

**路由的 handler 必須是 `api` 的具名方法，不得在 `server` 內寫匿名 handler。**
目前 `runNormal` 內有約 10 個內嵌 handler 違反此條（見第 8 節）。

### L8 `cmd/`

`main` 只做旗標解析與呼叫 `server`。桌面版額外處理視窗、托盤與更新。

---

## 3. 跨層共用的硬規則

| 編號 | 規則 | 理由 |
|---|---|---|
| R2 | 註冊 runner 的套件不得 import `internal/ws` | 保持 runner 與傳輸層解耦，runner 可單獨測試 |
| R3 | `sql.Open` 與 SQLite driver 只在 `internal/db` | 資料存取單一入口，避免連線設定與 PRAGMA 分歧 |
| R4 | 只有 `execAllowed` 清單的套件能 `exec.Command` | spawn 子進程是攻擊面，要能一眼看完所有發生處 |
| R5 | 用 `log/slog`，不用 stdlib `log` | 日誌統一經 `logging`，才能進環形緩衝供前端查看 |
| R6 | 非測試碼不得 `panic` | 單一進程服務，panic 會連帶中斷其他 session |

`execAllowed`：`proc`、`shell`、`gitinfo`、`clicheck`、`quota`、`opener`、`desktopupdate`
與六個 runner 套件。

---

## 4. 檔案與函式大小

* **單檔上限 600 行**（非測試檔）。
* **單一函式上限 150 行**（非測試檔，含方法）。

600 取在現況第四大檔（`tgclient/manager.go`，531 行）之上，150 取在把白名單控制在 6 項的位置。
這兩個數字的作用不是美學，是讓規則上線時白名單短到還有約束力——白名單一長，規則就等於沒有。

超過就拆：先把獨立職責搬成同套件新檔，再考慮下沉到更低的層。
**不要用「加一個參數、多一個 `if`」的方式延長既有大函式**，那會讓白名單條目永久化。

測試檔不設上限。表格驅動測試天生就長，限制它只會逼人把 case 砍掉。

---

## 5. 命名與風格

| 對象 | 規則 | 例 |
|---|---|---|
| `.go` 檔名 | 全小寫，底線分隔，不含連字號與大寫 | `acp_runner.go`、`proc_windows.go` |
| 套件名 | 單字小寫，不用底線與縮寫堆疊 | `kiroacp`、`desktopupdate` |
| 平台差異 | 用檔名後綴，不用 `runtime.GOOS` 分支 | `proc_windows.go` / `proc_unix.go` |
| 註解 | 正體中文，寫 WHY 不寫 WHAT，密度比照周邊 | — |
| 錯誤 | `fmt.Errorf("...: %w", err)` 保留鏈；哨兵錯誤用 `Err` 前綴並以 `errors.Is` 比對 | `agent.ErrHandoff` |
| context | 所有可能阻塞的匯出函式第一個參數是 `ctx context.Context` | — |

`gofmt` 必須乾淨（`gofmt -l internal cmd` 無輸出）。本倉庫 `.gitattributes` 設 `eol=lf`，
Windows 工作區的 CRLF 不算違規。

---

## 6. 新增程式碼的步驟

1. **先問要不要新增。** 現有套件能放得下就不開新套件；能用標準庫就不加依賴。
2. 依第 1、2 節決定層與套件。新套件要在 `arch_test.go` 的 `layerOf` 登記層號。
3. 依第 5 節命名。
4. 非 trivial 的邏輯留一支測試：同套件 `*_test.go`，表格驅動，不引入測試框架。
   套件的非測試碼超過 100 行就**必須**有測試檔（R9）。
5. 跑驗證（第 7 節）。

**跨層的大改動（例如拆 `ws.NewHandler`、引入 service 層）走 Spectra**：
先 `/spectra-propose` 立 change，不要直接動手。這類改動會同時牽動分層規則與白名單，
需要先有 proposal 說清楚新的分層長什麼樣。

---

## 7. 驗證

```bash
go test ./internal/arch/      # 分層與規模檢查
gofmt -l internal cmd         # 應無輸出
go vet ./internal/... ./cmd/...
go test ./...                 # 全部測試（含上述分層檢查）
```

檢查項目：

| 編號 | 規則 | 來源 |
|---|---|---|
| R1 | 套件都已登記層號；import 只指向更低的層 | 第 1 節 |
| R2 | runner 套件不 import `ws` | 第 2、3 節 |
| R3 | `sql.Open`／SQLite driver 只在 `db` | 第 2、3 節 |
| R4 | `exec.Command` 只在 `execAllowed` 清單 | 第 3 節 |
| R5 | 不 import stdlib `log` | 第 3 節 |
| R6 | 非測試碼不 `panic` | 第 3 節 |
| R7 | 單檔 ≤ 600 行 | 第 4 節 |
| R8 | 單一函式 ≤ 150 行 | 第 4 節 |
| R9 | 非測試碼 > 100 行的套件要有測試檔 | 第 6 節 |
| R10 | `.go` 檔名全小寫、無連字號 | 第 5 節 |
| R11 | 白名單無死條目（處理完要清除） | 第 8 節 |

以下屬**人工審查**，腳本不檢查：多 Agent 相容性、錯誤包裝、`ctx` 傳遞、
註解語言與密度、輸入驗證是否在 L6 做完、是否新增依賴。

---

## 8. 現存例外（待遷移）

| 編號 | 項目 | 處理方向 |
|---|---|---|
| R8 | `ws/handler.go:NewHandler`（1524 行） | **最優先。** 43 個 `case` 分支、80 處 `database.*` 呼叫擠在一個 closure。按訊息類型拆成具名 handler，狀態收進一個回合物件 |
| R8 | `server/server.go:runNormal`（450 行） | 內嵌的約 10 個路由 handler（登入、Telegram 驗證、`/config`、`/model-options/refresh`）移往 `api`，比照既有的 `sh`／`shareH`／`sth` 寫法 |
| R7 | `ws/handler.go`、`mcp/tools.go`、`server/server.go` | 隨上面兩項一起縮小 |
| R8 | `kiroacp/runner.go:Runner.runOnce`（313 行） | 拆出 ACP 握手與回合串流 |
| R8 | `db/db.go:DB.migrate`（164 行） | schema 清單，會持續增長。現為字串比對忽略 `duplicate column`，改成 `PRAGMA user_version` 順序遷移後此例外可一併處理 |
| R8 | `cmd/desktop/main_windows.go:main`（185 行） | 視窗與托盤組裝，低優先 |
| R2 | `tgclient` 註冊 runner 卻 import `ws` | 把 `ws` 的廣播抽成 `tgclient` 自己定義的 callback 介面，由 `server` 注入 |
| R3 | `quota` 直接開 SQLite | 讀的是 Cursor 自己的 `state.vscdb` 取 token，不是本專案的 DB，長期保留。已在 `readCursorAccessToken` 註明唯讀 |
| R6 | `logging` 的 `panic` | 日誌初始化失敗，可保留；若改為回傳 error 則一併清除 |

### 已清除

| 編號 | 項目 | 做法 |
|---|---|---|
| R4 | `api/open.go` spawn 子進程 | 命令組裝與啟動移到新的 L1 套件 `internal/opener`，`api` 不再 import `os/exec` |
| R5 | `static` 用 stdlib `log` | bundle 編譯失敗改用 `slog.Error` |
| R8 | `cursor/runner.go:Runner.Run`（165 行） | 旗標組裝拆成 `cursorArgs`、子進程組裝拆成 `cursorCommand` |

處理掉一項，就從 `arch_test.go` 的 `knownViolations` 刪掉對應條目。R11 會確保白名單不殘留死條目。
