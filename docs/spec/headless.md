> ## Documentation Index

> Fetch the complete documentation index at: https://code.claude.com/docs/llms.txt

> Use this file to discover all available pages before exploring further.



\# 以程式方式執行 Claude Code



> 使用 Agent SDK 從 CLI、Python 或 TypeScript 以程式方式執行 Claude Code。



\[Agent SDK](https://platform.claude.com/docs/zh-TW/agent-sdk/overview) 提供與 Claude Code 相同的工具、agent 迴圈和上下文管理。它可作為 CLI 用於指令碼和 CI/CD，或作為 \[Python](https://platform.claude.com/docs/zh-TW/agent-sdk/python) 和 \[TypeScript](https://platform.claude.com/docs/zh-TW/agent-sdk/typescript) 套件供完整的程式控制。



<Note>

&#x20; CLI 之前稱為「無頭模式」。`-p` 旗標和所有 CLI 選項的工作方式相同。

</Note>



若要從 CLI 以程式方式執行 Claude Code，請傳遞 `-p` 和您的提示以及任何 \[CLI 選項](/zh-TW/cli-reference)：



```bash theme={null}

claude -p "Find and fix the bug in auth.py" --allowedTools "Read,Edit,Bash"

```



本頁涵蓋透過 CLI (`claude -p`) 使用 Agent SDK。如需具有結構化輸出、工具核准回呼和原生訊息物件的 Python 和 TypeScript SDK 套件，請參閱 \[完整 Agent SDK 文件](https://platform.claude.com/docs/zh-TW/agent-sdk/overview)。



\## 基本用法



將 `-p`（或 `--print`）旗標新增至任何 `claude` 命令以非互動方式執行它。所有 \[CLI 選項](/zh-TW/cli-reference) 都適用於 `-p`，包括：



\* `--continue` 用於 \[繼續對話](#continue-conversations)

\* `--allowedTools` 用於 \[自動核准工具](#auto-approve-tools)

\* `--output-format` 用於 \[結構化輸出](#get-structured-output)



此範例詢問 Claude 關於您的程式碼庫的問題並列印回應：



```bash theme={null}

claude -p "What does the auth module do?"

```



\## 範例



這些範例突出顯示常見的 CLI 模式。



\### 取得結構化輸出



使用 `--output-format` 控制回應的傳回方式：



\* `text`（預設）：純文字輸出

\* `json`：包含結果、工作階段 ID 和中繼資料的結構化 JSON

\* `stream-json`：用於即時串流的換行分隔 JSON



此範例以 JSON 格式傳回專案摘要及工作階段中繼資料，文字結果在 `result` 欄位中：



```bash theme={null}

claude -p "Summarize this project" --output-format json

```



若要取得符合特定結構描述的輸出，請使用 `--output-format json` 搭配 `--json-schema` 和 \[JSON Schema](https://json-schema.org/) 定義。回應包含關於請求的中繼資料（工作階段 ID、使用情況等），結構化輸出在 `structured\_output` 欄位中。



此範例從 auth.py 提取函式名稱並將其作為字串陣列傳回：



```bash theme={null}

claude -p "Extract the main function names from auth.py" \\

&#x20; --output-format json \\

&#x20; --json-schema '{"type":"object","properties":{"functions":{"type":"array","items":{"type":"string"}}},"required":\["functions"]}'

```



<Tip>

&#x20; 使用 \[jq](https://jqlang.github.io/jq/) 之類的工具來解析回應並提取特定欄位：



&#x20; ```bash theme={null}

&#x20; # Extract the text result

&#x20; claude -p "Summarize this project" --output-format json | jq -r '.result'



&#x20; # Extract structured output

&#x20; claude -p "Extract function names from auth.py" \\

&#x20;   --output-format json \\

&#x20;   --json-schema '{"type":"object","properties":{"functions":{"type":"array","items":{"type":"string"}}},"required":\["functions"]}' \\

&#x20;   | jq '.structured\_output'

&#x20; ```

</Tip>



\### 串流回應



使用 `--output-format stream-json` 搭配 `--verbose` 和 `--include-partial-messages` 以在產生令牌時接收它們。每一行都是代表事件的 JSON 物件：



```bash theme={null}

claude -p "Explain recursion" --output-format stream-json --verbose --include-partial-messages

```



下列範例使用 \[jq](https://jqlang.github.io/jq/) 篩選文字差異並僅顯示串流文字。`-r` 旗標輸出原始字串（無引號），`-j` 不帶換行符號的聯結，因此令牌會連續串流：



```bash theme={null}

claude -p "Write a poem" --output-format stream-json --verbose --include-partial-messages | \\

&#x20; jq -rj 'select(.type == "stream\_event" and .event.delta.type? == "text\_delta") | .event.delta.text'

```



如需具有回呼和訊息物件的程式化串流，請參閱 Agent SDK 文件中的 \[即時串流回應](https://platform.claude.com/docs/zh-TW/agent-sdk/streaming-output)。



\### 自動核准工具



使用 `--allowedTools` 讓 Claude 使用某些工具而無需提示。此範例執行測試套件並修復失敗，允許 Claude 執行 Bash 命令和讀取/編輯檔案而無需請求許可：



```bash theme={null}

claude -p "Run the test suite and fix any failures" \\

&#x20; --allowedTools "Bash,Read,Edit"

```



\### 建立提交



此範例檢查暫存的變更並建立具有適當訊息的提交：



```bash theme={null}

claude -p "Look at my staged changes and create an appropriate commit" \\

&#x20; --allowedTools "Bash(git diff \*),Bash(git log \*),Bash(git status \*),Bash(git commit \*)"

```



`--allowedTools` 旗標使用 \[權限規則語法](/zh-TW/settings#permission-rule-syntax)。尾部的 ` \*` 啟用前綴匹配，因此 `Bash(git diff \*)` 允許任何以 `git diff` 開頭的命令。空格在 `\*` 之前很重要：沒有它，`Bash(git diff\*)` 也會符合 `git diff-index`。



<Note>

&#x20; 使用者叫用的 \[skills](/zh-TW/skills) 如 `/commit` 和 \[內建命令](/zh-TW/commands) 僅在互動模式中可用。在 `-p` 模式中，改為描述您想要完成的任務。

</Note>



\### 自訂系統提示



使用 `--append-system-prompt` 新增指示同時保持 Claude Code 的預設行為。此範例將 PR 差異管道傳送至 Claude 並指示它檢查安全漏洞：



```bash theme={null}

gh pr diff "$1" | claude -p \\

&#x20; --append-system-prompt "You are a security engineer. Review for vulnerabilities." \\

&#x20; --output-format json

```



請參閱 \[系統提示旗標](/zh-TW/cli-reference#system-prompt-flags) 以取得更多選項，包括 `--system-prompt` 以完全取代預設提示。



\### 繼續對話



使用 `--continue` 繼續最近的對話，或使用 `--resume` 搭配工作階段 ID 以繼續特定對話。此範例執行檢查，然後傳送後續提示：



```bash theme={null}

\# First request

claude -p "Review this codebase for performance issues"



\# Continue the most recent conversation

claude -p "Now focus on the database queries" --continue

claude -p "Generate a summary of all issues found" --continue

```



如果您執行多個對話，請擷取工作階段 ID 以繼續特定對話：



```bash theme={null}

session\_id=$(claude -p "Start a review" --output-format json | jq -r '.session\_id')

claude -p "Continue that review" --resume "$session\_id"

```



\## 後續步驟



\* \[Agent SDK 快速入門](https://platform.claude.com/docs/zh-TW/agent-sdk/quickstart)：使用 Python 或 TypeScript 建立您的第一個 agent

\* \[CLI 參考](/zh-TW/cli-reference)：所有 CLI 旗標和選項

\* \[GitHub Actions](/zh-TW/github-actions)：在 GitHub 工作流程中使用 Agent SDK

\* \[GitLab CI/CD](/zh-TW/gitlab-ci-cd)：在 GitLab 管道中使用 Agent SDK



