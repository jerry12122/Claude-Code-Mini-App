> ## Documentation Index

> Fetch the complete documentation index at: https://code.claude.com/docs/llms.txt

> Use this file to discover all available pages before exploring further.



\# CLI 參考



> Claude Code 命令列介面的完整參考，包括命令和旗標。



\## CLI 命令



您可以使用這些命令來啟動工作階段、管道內容、繼續對話和管理更新：



| 命令                              | 描述                                                                                                                                                                               | 範例                                                          |

| :------------------------------ | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :---------------------------------------------------------- |

| `claude`                        | 啟動互動式工作階段                                                                                                                                                                        | `claude`                                                    |

| `claude "query"`                | 使用初始提示啟動互動式工作階段                                                                                                                                                                  | `claude "explain this project"`                             |

| `claude -p "query"`             | 透過 SDK 查詢，然後退出                                                                                                                                                                   | `claude -p "explain this function"`                         |

| `cat file \\| claude -p "query"` | 處理管道內容                                                                                                                                                                           | `cat logs.txt \\| claude -p "explain"`                       |

| `claude -c`                     | 在目前目錄中繼續最近的對話                                                                                                                                                                    | `claude -c`                                                 |

| `claude -c -p "query"`          | 透過 SDK 繼續                                                                                                                                                                        | `claude -c -p "Check for type errors"`                      |

| `claude -r "<session>" "query"` | 按 ID 或名稱繼續工作階段                                                                                                                                                                   | `claude -r "auth-refactor" "Finish this PR"`                |

| `claude update`                 | 更新至最新版本                                                                                                                                                                          | `claude update`                                             |

| `claude auth login`             | 登入您的 Anthropic 帳戶。使用 `--email` 預先填入您的電子郵件地址，使用 `--sso` 強制進行 SSO 驗證，使用 `--console` 以 Anthropic Console 登入以進行 API 使用計費，而不是 Claude 訂閱                                               | `claude auth login --console`                               |

| `claude auth logout`            | 從您的 Anthropic 帳戶登出                                                                                                                                                               | `claude auth logout`                                        |

| `claude auth status`            | 以 JSON 格式顯示驗證狀態。使用 `--text` 以人類可讀的格式輸出。如果已登入則以代碼 0 退出，如果未登入則以代碼 1 退出                                                                                                             | `claude auth status`                                        |

| `claude agents`                 | 列出所有已設定的 \[subagents](/zh-TW/sub-agents)，按來源分組                                                                                                                                    | `claude agents`                                             |

| `claude auto-mode defaults`     | 以 JSON 格式列印內建的 \[auto mode](/zh-TW/permission-modes#eliminate-prompts-with-auto-mode) 分類器規則。使用 `claude auto-mode config` 查看您的有效設定及套用的設定                                           | `claude auto-mode defaults > rules.json`                    |

| `claude mcp`                    | 設定 Model Context Protocol (MCP) 伺服器                                                                                                                                              | 請參閱 \[Claude Code MCP 文件](/zh-TW/mcp)。                       |

| `claude plugin`                 | 管理 Claude Code \[plugins](/zh-TW/plugins)。別名：`claude plugins`。請參閱 \[plugin 參考](/zh-TW/plugins-reference#cli-commands-reference) 以了解子命令                                             | `claude plugin install code-review@claude-plugins-official` |

| `claude remote-control`         | 啟動 \[Remote Control](/zh-TW/remote-control) 伺服器以從 Claude.ai 或 Claude 應用程式控制 Claude Code。在伺服器模式下執行（無本機互動式工作階段）。請參閱 \[伺服器模式旗標](/zh-TW/remote-control#start-a-remote-control-session) | `claude remote-control --name "My Project"`                 |



\## CLI 旗標



使用這些命令列旗標自訂 Claude Code 的行為。`claude --help` 不會列出每個旗標，因此旗標在 `--help` 中的缺失並不表示它無法使用。



| 旗標                                        | 描述                                                                                                                                                                                                           | 範例                                                                                                 |

| :---------------------------------------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------------------------- |

| `--add-dir`                               | 新增額外的工作目錄供 Claude 讀取和編輯檔案。授予檔案存取權；大多數 `.claude/` 設定 \[未從這些目錄探索](/zh-TW/permissions#additional-directories-grant-file-access-not-configuration)。驗證每個路徑是否存在為目錄                                                  | `claude --add-dir ../apps ../lib`                                                                  |

| `--agent`                                 | 為目前工作階段指定代理程式（覆蓋 `agent` 設定）                                                                                                                                                                                 | `claude --agent my-custom-agent`                                                                   |

| `--agents`                                | 透過 JSON 動態定義自訂 subagents。使用與 subagent \[frontmatter](/zh-TW/sub-agents#supported-frontmatter-fields) 相同的欄位名稱，加上代理程式指示的 `prompt` 欄位                                                                            | `claude --agents '{"reviewer":{"description":"Reviews code","prompt":"You are a code reviewer"}}'` |

| `--allow-dangerously-skip-permissions`    | 新增 `bypassPermissions` 到 `Shift+Tab` 模式循環而不立即啟動它。允許您以不同的模式（如 `plan`）開始，稍後切換到 `bypassPermissions`。請參閱 \[permission modes](/zh-TW/permission-modes#skip-all-checks-with-bypasspermissions-mode)                 | `claude --permission-mode plan --allow-dangerously-skip-permissions`                               |

| `--allowedTools`                          | 無需提示權限即可執行的工具。請參閱 \[permission rule syntax](/zh-TW/settings#permission-rule-syntax) 以了解模式匹配。若要限制可用的工具，請改用 `--tools`                                                                                           | `"Bash(git log \*)" "Bash(git diff \*)" "Read"`                                                      |

| `--append-system-prompt`                  | 將自訂文字附加到預設系統提示的末尾                                                                                                                                                                                            | `claude --append-system-prompt "Always use TypeScript"`                                            |

| `--append-system-prompt-file`             | 從檔案載入額外的系統提示文字並附加到預設提示                                                                                                                                                                                       | `claude --append-system-prompt-file ./extra-rules.txt`                                             |

| `--bare`                                  | 最小模式：跳過 hooks、skills、plugins、MCP 伺服器、自動記憶體和 CLAUDE.md 的自動探索，以便指令碼呼叫啟動更快。Claude 可以存取 Bash、檔案讀取和檔案編輯工具。設定 \[`CLAUDE\_CODE\_SIMPLE`](/zh-TW/env-vars)。請參閱 \[bare mode](/zh-TW/headless#start-faster-with-bare-mode) | `claude --bare -p "query"`                                                                         |

| `--betas`                                 | 要包含在 API 請求中的 Beta 標頭（僅限 API 金鑰使用者）                                                                                                                                                                          | `claude --betas interleaved-thinking`                                                              |

| `--channels`                              | （研究預覽）MCP 伺服器，其 \[channel](/zh-TW/channels) 通知 Claude 應在此工作階段中監聽。以空格分隔的 `plugin:<name>@<marketplace>` 項目清單。需要 Claude.ai 驗證                                                                                    | `claude --channels plugin:my-notifier@my-marketplace`                                              |

| `--chrome`                                | 啟用 \[Chrome 瀏覽器整合](/zh-TW/chrome) 以進行網頁自動化和測試                                                                                                                                                                 | `claude --chrome`                                                                                  |

| `--continue`, `-c`                        | 載入目前目錄中最近的對話                                                                                                                                                                                                 | `claude --continue`                                                                                |

| `--dangerously-load-development-channels` | 啟用不在核准允許清單上的 \[channels](/zh-TW/channels-reference#test-during-the-research-preview)，用於本機開發。接受 `plugin:<name>@<marketplace>` 和 `server:<name>` 項目。提示確認                                                        | `claude --dangerously-load-development-channels server:webhook`                                    |

| `--dangerously-skip-permissions`          | 略過權限提示。等同於 `--permission-mode bypassPermissions`。請參閱 \[permission modes](/zh-TW/permission-modes#skip-all-checks-with-bypasspermissions-mode) 以了解此操作會和不會略過的內容                                                 | `claude --dangerously-skip-permissions`                                                            |

| `--debug`                                 | 啟用偵錯模式，可選類別篩選（例如，`"api,hooks"` 或 `"!statsig,!file"`）                                                                                                                                                         | `claude --debug "api,mcp"`                                                                         |

| `--debug-file <path>`                     | 將偵錯日誌寫入特定檔案路徑。隱含啟用偵錯模式。優先於 `CLAUDE\_CODE\_DEBUG\_LOGS\_DIR`                                                                                                                                                      | `claude --debug-file /tmp/claude-debug.log`                                                        |

| `--disable-slash-commands`                | 為此工作階段停用所有 skills 和命令                                                                                                                                                                                        | `claude --disable-slash-commands`                                                                  |

| `--disallowedTools`                       | 從模型的內容中移除且無法使用的工具                                                                                                                                                                                            | `"Bash(git log \*)" "Bash(git diff \*)" "Edit"`                                                      |

| `--effort`                                | 為目前工作階段設定 \[effort level](/zh-TW/model-config#adjust-effort-level)。選項：`low`、`medium`、`high`、`max`（僅限 Opus 4.6）。工作階段範圍且不會持久化到設定                                                                                | `claude --effort high`                                                                             |

| `--fallback-model`                        | 當預設模型過載時啟用自動回退到指定的模型（僅列印模式）                                                                                                                                                                                  | `claude -p --fallback-model sonnet "query"`                                                        |

| `--fork-session`                          | 繼續時，建立新的工作階段 ID 而不是重複使用原始 ID（與 `--resume` 或 `--continue` 搭配使用）                                                                                                                                               | `claude --resume abc123 --fork-session`                                                            |

| `--from-pr`                               | 繼續連結到特定 GitHub PR 的工作階段。接受 PR 編號或 URL。透過 `gh pr create` 建立時會自動連結工作階段                                                                                                                                         | `claude --from-pr 123`                                                                             |

| `--ide`                                   | 如果恰好有一個有效的 IDE 可用，在啟動時自動連線到 IDE                                                                                                                                                                              | `claude --ide`                                                                                     |

| `--init`                                  | 執行初始化 hooks 並啟動互動模式                                                                                                                                                                                          | `claude --init`                                                                                    |

| `--init-only`                             | 執行初始化 hooks 並退出（無互動式工作階段）                                                                                                                                                                                    | `claude --init-only`                                                                               |

| `--include-hook-events`                   | 在輸出串流中包含所有 hook 生命週期事件。需要 `--output-format stream-json`                                                                                                                                                      | `claude -p --output-format stream-json --include-hook-events "query"`                              |

| `--include-partial-messages`              | 在輸出中包含部分串流事件。需要 `--print` 和 `--output-format stream-json`                                                                                                                                                    | `claude -p --output-format stream-json --include-partial-messages "query"`                         |

| `--input-format`                          | 為列印模式指定輸入格式（選項：`text`、`stream-json`）                                                                                                                                                                         | `claude -p --output-format json --input-format stream-json`                                        |

| `--json-schema`                           | 在代理程式完成其工作流程後取得符合 JSON Schema 的驗證 JSON 輸出（僅列印模式，請參閱 \[structured outputs](https://platform.claude.com/docs/en/agent-sdk/structured-outputs)）                                                                  | `claude -p --json-schema '{"type":"object","properties":{...}}' "query"`                           |

| `--maintenance`                           | 執行維護 hooks 並啟動互動模式                                                                                                                                                                                           | `claude --maintenance`                                                                             |

| `--max-budget-usd`                        | 在停止前在 API 呼叫上花費的最大美元金額（僅列印模式）                                                                                                                                                                                | `claude -p --max-budget-usd 5.00 "query"`                                                          |

| `--max-turns`                             | 限制代理程式轉數（僅列印模式）。達到限制時以錯誤退出。預設無限制                                                                                                                                                                             | `claude -p --max-turns 3 "query"`                                                                  |

| `--mcp-config`                            | 從 JSON 檔案或字串載入 MCP 伺服器（以空格分隔）                                                                                                                                                                                | `claude --mcp-config ./mcp.json`                                                                   |

| `--model`                                 | 使用最新模型的別名（`sonnet` 或 `opus`）或模型的完整名稱為目前工作階段設定模型                                                                                                                                                              | `claude --model claude-sonnet-4-6`                                                                 |

| `--name`, `-n`                            | 為工作階段設定顯示名稱，顯示在 `/resume` 和終端標題中。您可以使用 `claude --resume <name>` 繼續已命名的工作階段。<br /><br />\[`/rename`](/zh-TW/commands) 在工作階段中途變更名稱，也會在提示列中顯示                                                                    | `claude -n "my-feature-work"`                                                                      |

| `--no-chrome`                             | 為此工作階段停用 \[Chrome 瀏覽器整合](/zh-TW/chrome)                                                                                                                                                                       | `claude --no-chrome`                                                                               |

| `--no-session-persistence`                | 停用工作階段持久性，使工作階段不會儲存到磁碟且無法繼續（僅列印模式）                                                                                                                                                                           | `claude -p --no-session-persistence "query"`                                                       |

| `--output-format`                         | 為列印模式指定輸出格式（選項：`text`、`json`、`stream-json`）                                                                                                                                                                  | `claude -p "query" --output-format json`                                                           |

| `--enable-auto-mode`                      | 在 `Shift+Tab` 循環中解鎖 \[auto mode](/zh-TW/permission-modes#eliminate-prompts-with-auto-mode)。需要 Team、Enterprise 或 API 方案以及 Claude Sonnet 4.6 或 Opus 4.6                                                         | `claude --enable-auto-mode`                                                                        |

| `--permission-mode`                       | 以指定的 \[permission mode](/zh-TW/permission-modes) 開始。接受 `default`、`acceptEdits`、`plan`、`auto`、`dontAsk` 或 `bypassPermissions`。覆蓋設定檔案中的 `defaultMode`                                                           | `claude --permission-mode plan`                                                                    |

| `--permission-prompt-tool`                | 指定 MCP 工具以在非互動模式下處理權限提示                                                                                                                                                                                      | `claude -p --permission-prompt-tool mcp\_auth\_tool "query"`                                         |

| `--plugin-dir`                            | 為此工作階段僅從目錄載入 plugins。每個旗標採用一個路徑。重複旗標以使用多個目錄：`--plugin-dir A --plugin-dir B`                                                                                                                                  | `claude --plugin-dir ./my-plugins`                                                                 |

| `--print`, `-p`                           | 列印回應而不進入互動模式（請參閱 \[Agent SDK 文件](https://platform.claude.com/docs/en/agent-sdk/overview) 以了解程式化使用詳細資訊）                                                                                                        | `claude -p "query"`                                                                                |

| `--remote`                                | 在 claude.ai 上建立新的 \[web session](/zh-TW/claude-code-on-the-web)，並提供工作描述                                                                                                                                       | `claude --remote "Fix the login bug"`                                                              |

| `--remote-control`, `--rc`                | 啟動互動式工作階段，並啟用 \[Remote Control](/zh-TW/remote-control#start-a-remote-control-session)，以便您也可以從 claude.ai 或 Claude 應用程式控制它。可選擇傳遞工作階段的名稱                                                                         | `claude --remote-control "My Project"`                                                             |

| `--replay-user-messages`                  | 從 stdin 重新發出使用者訊息回到 stdout 以進行確認。需要 `--input-format stream-json` 和 `--output-format stream-json`                                                                                                             | `claude -p --input-format stream-json --output-format stream-json --replay-user-messages`          |

| `--resume`, `-r`                          | 按 ID 或名稱繼續特定工作階段，或顯示互動式選擇器以選擇工作階段                                                                                                                                                                            | `claude --resume auth-refactor`                                                                    |

| `--session-id`                            | 為對話使用特定的工作階段 ID（必須是有效的 UUID）                                                                                                                                                                                 | `claude --session-id "550e8400-e29b-41d4-a716-446655440000"`                                       |

| `--setting-sources`                       | 要載入的設定來源的逗號分隔清單（`user`、`project`、`local`）                                                                                                                                                                    | `claude --setting-sources user,project`                                                            |

| `--settings`                              | 設定 JSON 檔案的路徑或要載入其他設定的 JSON 字串                                                                                                                                                                               | `claude --settings ./settings.json`                                                                |

| `--strict-mcp-config`                     | 僅使用 `--mcp-config` 中的 MCP 伺服器，忽略所有其他 MCP 設定                                                                                                                                                                  | `claude --strict-mcp-config --mcp-config ./mcp.json`                                               |

| `--system-prompt`                         | 用自訂文字取代整個系統提示                                                                                                                                                                                                | `claude --system-prompt "You are a Python expert"`                                                 |

| `--system-prompt-file`                    | 從檔案載入系統提示，取代預設提示                                                                                                                                                                                             | `claude --system-prompt-file ./custom-prompt.txt`                                                  |

| `--teleport`                              | 在本機終端中繼續 \[web session](/zh-TW/claude-code-on-the-web)                                                                                                                                                        | `claude --teleport`                                                                                |

| `--teammate-mode`                         | 設定 \[agent team](/zh-TW/agent-teams) 隊友的顯示方式：`auto`（預設）、`in-process` 或 `tmux`。請參閱 \[Choose a display mode](/zh-TW/agent-teams#choose-a-display-mode)                                                           | `claude --teammate-mode in-process`                                                                |

| `--tmux`                                  | 為 worktree 建立 tmux 工作階段。需要 `--worktree`。在可用時使用 iTerm2 原生窗格；傳遞 `--tmux=classic` 以使用傳統 tmux                                                                                                                    | `claude -w feature-auth --tmux`                                                                    |

| `--tools`                                 | 限制 Claude 可以使用的內建工具。使用 `""` 停用全部，`"default"` 為全部，或工具名稱如 `"Bash,Edit,Read"`                                                                                                                                   | `claude --tools "Bash,Edit,Read"`                                                                  |

| `--verbose`                               | 啟用詳細記錄，顯示完整的逐轉輸出                                                                                                                                                                                             | `claude --verbose`                                                                                 |

| `--version`, `-v`                         | 輸出版本號                                                                                                                                                                                                        | `claude -v`                                                                                        |

| `--worktree`, `-w`                        | 在隔離的 \[git worktree](/zh-TW/common-workflows#run-parallel-claude-code-sessions-with-git-worktrees) 中啟動 Claude，位於 `<repo>/.claude/worktrees/<name>`。如果未提供名稱，則會自動產生一個                                           | `claude -w feature-auth`                                                                           |



\### 系統提示旗標



Claude Code 提供四個旗標用於自訂系統提示。所有四個都在互動和非互動模式中運作。



| 旗標                            | 行為           | 範例                                                      |

| :---------------------------- | :----------- | :------------------------------------------------------ |

| `--system-prompt`             | 取代整個預設提示     | `claude --system-prompt "You are a Python expert"`      |

| `--system-prompt-file`        | 用檔案內容取代      | `claude --system-prompt-file ./prompts/review.txt`      |

| `--append-system-prompt`      | 附加到預設提示      | `claude --append-system-prompt "Always use TypeScript"` |

| `--append-system-prompt-file` | 將檔案內容附加到預設提示 | `claude --append-system-prompt-file ./style-rules.txt`  |



`--system-prompt` 和 `--system-prompt-file` 互斥。附加旗標可以與任一取代旗標組合。



對於大多數使用案例，請使用附加旗標。附加會保留 Claude Code 的內建功能，同時新增您的需求。僅當您需要對系統提示進行完全控制時，才使用取代旗標。



\## 另請參閱



\* \[Chrome 擴充功能](/zh-TW/chrome) - 瀏覽器自動化和網頁測試

\* \[互動模式](/zh-TW/interactive-mode) - 快捷鍵、輸入模式和互動功能

\* \[快速入門指南](/zh-TW/quickstart) - Claude Code 入門

\* \[常見工作流程](/zh-TW/common-workflows) - 進階工作流程和模式

\* \[設定](/zh-TW/settings) - 設定選項

\* \[Agent SDK 文件](https://platform.claude.com/docs/en/agent-sdk/overview) - 程式化使用和整合



