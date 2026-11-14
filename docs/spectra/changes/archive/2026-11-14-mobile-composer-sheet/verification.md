# 手機工具面板驗收紀錄

日期：2026-11-14。範圍：`mobile-composer-sheet` 的 6 個任務、9 個需求、32 個場景；沒有 `##### Example:` 區塊。正式介面已接到 ChatView，本次不發版。

## 已執行

| 指令 | 結果與範圍 |
| --- | --- |
| `python scripts/composer-attachment.test.py` | passed-current；實際 ChatView、ComposerTools、ui-atoms、chat-header、useChatComposer；使用 useChatSocket 原始 flushPendingModes callback，攔截 API／WS，無真實上傳 |
| `node --test scripts/remaining-ux.test.cjs scripts/console-usability.test.cjs scripts/theme.test.cjs` | passed-current；3 個檔案全通過，包含既有草稿附件備份／恢復與焦點回歸 |
| `go test ./internal/static` | passed-current；開發資源測試通過（末次由 Go cache 命中） |
| `go test -tags production ./internal/static` | passed-current；嵌入資源及正式 bundle 測試通過 |

環境：Windows amd64、Python 3.13.5、Node v22.17.0、Go go1.26.11；Playwright 的 msedge channel，Edge 157.0.4341.46，headless Chromium。所有結果來自本次實際工具輸出，不以靜態檢查代替執行結果。

瀏覽器測試使用 repo 內 React、ReactDOM、Tailwind 與 go.mod 已有的 esbuild Go CLI；不新增 npm 相依。檢查 390×844、844×390、320×480、1280×900，639／640 fine-pointer 邊界、844×700 coarse-pointer 開啟鎖定、visualViewport 高度 300、雙主題及 200% 文字放大。JS pageerror 會使測試失敗。

## 需求與實作

下表測試均指 `scripts/composer-attachment.test.py`；各場景的精確對照見下節。所有需求的 Example 對照均為「無 Example 區塊」。

| 需求 | 實作位置 | 已檢查的測試與結果 |
| --- | --- | --- |
| Explicit mobile tools entry | ChatView 的 toolsOpen／工具 render；ComposerTools 的 compact 判斷與 Plus | 入口、焦點／失焦、唯讀與 interrupt-only 矩陣；passed-current |
| Sheet state survives focus and viewport changes | ComposerTools 的 mobile latch、position、page；index.html 的 dialog／內部捲動 CSS | 高度、旋轉、轉桌面、設定返回、內部捲動、背景位置；passed-current |
| Native picker lifecycle preserves attachment drafts | ChatView 的常駐 file input、pickerRequestRef、uploadAllowedRef、uploadFiles／handleFilesPicked | 取消、同檔重選、多檔錯誤／重試、session／權限過期；passed-current |
| Accessible dismissal consumes only the tools interaction | ComposerTools 原生 dialog、close／back、popstate、Telegram BackButton | 可及性名稱、Tab／Shift+Tab、Escape、遮罩、Back／Forward、焦點與訂閱清理；passed-current |
| Sheet lifecycle retains existing control restrictions | ChatView 的 canAct／shareOver／isFocused／taskRunning 與工具既有停用條件 | Agent／guest／shell／busy／斷線矩陣、生命週期、下次送出套用；passed-current |
| Settings use in-sheet single-choice pages | ComposerToolChoice；ui-atoms 既有 selectors 的 sheet 呈現 | 當前值、選擇／返回、選項失敗保留 default／current、選擇中轉 busy；passed-current |
| Composer action row hosts model, effort, and permission controls | ChatView 的單份工具元素與桌面／手機排列 | 桌面順序、單份 id、手機 Agent／guest／shell 矩陣；passed-current |
| Chat header no longer duplicates the controls | 沿用未修改的 chat-header.js | 實際 header 在桌面／手機無 selector，Plus 可在未聚焦 textarea 時開啟；passed-current |
| Compact controls and zoom retain access to controls | index.html 的 compact grid、dialog、textarea CSS | 雙主題、觸控入口、窄高捲動、文字放大、textarea 失焦與附件保留；passed-current |

## 逐場景對照

`M` = specs/mobile-composer-sheet，`C` = specs/composer-model-controls，`U` = specs/console-usability。每列均有自動測試；實機限制另列，不歸為測試範圍排除。

| 場景 | 任務 | 測試行為 |
| --- | --- | --- |
| M: Open from an unfocused composer | 1.2 | 未聚焦 textarea 開 Plus，具名 dialog 顯示 |
| M: Type and leave the message input | 1.2 | 四行草稿 focus／blur 前後高度相同，面板不自行開啟 |
| M: Desktop and non-editable chats | 1.1／1.2／3.1 | 桌面 inline；viewer／snapshot／舊 Kiro／分享結束／interrupt-only 無入口 |
| M: Blur and dismiss the keyboard | 3.1 | 原生 picker 失焦、visualViewport 300 與高度變化保留開啟 |
| M: Change a setting inside the sheet | 1.2 | 選 Sonnet 後回首頁顯示新值並聚焦原入口 |
| M: Rotate and switch layouts | 3.1 | 直向→橫向→非 compact 高度仍開啟，轉桌面關閉並保留 Opus／草稿 |
| M: Scroll a constrained sheet | 3.1／3.2 | 低高度捲到權限／max；Close 仍可見，背景 wheel 前後位置相同 |
| M: Cancel and choose again | 2.1 | 空清單／cancel 保留面板，之後選檔關閉並建 chips |
| M: Choose multiple files with an upload failure | 2.1 | 上傳被延後時兩個 chip 已顯示且面板關閉；依序上傳，第二檔失敗可重試／移除 |
| M: Reattach the same file | 1.1／2.1／3.2 | 桌面及手機重選同檔，手機移除後再開 Plus 重選，唯一 input 保留 |
| M: Return to a different or non-writable composer | 2.1 | picker 開啟期間換 session／改 viewer，舊結果不上傳 |
| M: Dismiss with close or backdrop | 1.2／2.2 | Close 與真正遮罩關閉，焦點回 Plus，文字保留 |
| M: Dismiss with keyboard | 2.2 | Escape 關閉，全域 Escape 計數保持 0 |
| M: Return without leaving chat | 2.2 | Browser Back／Telegram callback 關閉，URL／既有 history.state 與草稿保留 |
| M: Repeated opening and browser history | 2.2／3.2 | 已開啟時重點 Plus 不堆疊；3 輪 Back／Forward 不重開；下一次 Back 直接回原前一筆紀錄 |
| M: Assistive and keyboard navigation | 2.2 | get_by_role 驗證可及性名稱、初始 Close 焦點、背景 inert、Tab／Shift+Tab 約束 |
| M: Supported and restricted tools | 1.2 | Claude／Codex／Cursor／Telegram／Kiro ACP／Antigravity、editor、shell、busy 工具矩陣 |
| M: Settings wait for send | 1.2／3.2 | 三項設定更動後 WS 為空；按 Send 才依序 set_mode／set_model／set_effort／input |
| M: Change mode from tools | 1.2 | Agent↔Shell 回工具首頁，不關面板且草稿與同一 file input 保留 |
| M: Release an obsolete sheet | 3.1 | session／viewer／分享結束／interrupt-only／失去 pane focus／卸載／桌面，modal／history／TG／捲動鎖清理；恢復不重開 |
| M: Select and return to the setting | 1.2 | 選模型、再開時 checked；點目前值仍回首頁 |
| M: Back without a change | 2.2 | header Back／Escape／Browser Back／TG 在單選頁先回首頁並聚焦原入口 |
| M: Options fail or controls become disabled | 1.2／3.2 | model API 失敗仍有 default／custom-model checked；選擇中 THINKING 禁用 radio |
| C: Controls visible in agent mode | 1.1／1.2 | 桌面工具順序固定，手機五項整列工具依序呈現 |
| C: Controls hidden in shell mode | 1.2 | Shell 隱藏附件與三項 Agent 設定，仍可切回 Agent |
| C: Controls hidden for guests | 1.2 | editor 只有附件；沒有 owner 模式與設定 |
| C: Desktop header | 1.1／3.2 | 實際 header select 數量為 0，工具仍在 composer |
| C: Mobile header | 1.2／3.2 | 實際 header select 數量為 0；不需輸入焦點或展開 details 即可開 Plus |
| U: Use supported selectors on a phone | 1.2／3.1 | Claude 支援工具、內部捲動與無水平溢出 |
| U: Enlarge a narrow interface | 3.1 | 320px 重排與 200% 文字，Close 可見、body 不橫向溢出；正式 viewport 未禁用 zoom |
| U: Preserve desktop controls | 1.1 | 實際 button／select DOM 順序、唯一 id 與選檔；原有 compact class 沿用 |
| U: Read a multiline draft after blur | 1.2／2.1 | 四行文字失焦不縮成 44px，重選檔後 chip 在失焦時仍可見 |

測試範圍排除：單一面板的精確色碼、24px 圓角與 padding 不以數值快照測試；它們只影響當前視覺表面。已依核准 mock 檢視 390px 雙主題與 844px 橫向截圖。條件切換、可操作性、捲動、預設值及失敗降級均有測試，沒有以視覺排除為由略過。

## 測試先失敗與修正證據

- 任務 1：產品 file input 位於 `.ra-composer-tools` 的斷言先失敗，移出後通過。
- 任務 2：手機找不到 Plus 的測試先失敗，正式工具面板接入後通過。
- 任務 3：非空選檔後 dialog 仍開啟的測試先失敗，接受附件後關閉且不聚焦 textarea 後通過。
- 任務 4：刻意移除 Escape 的 stopPropagation，新增的全域 Escape 計數斷言失敗；已還原並通過，沒有保留故意破壞的程式。
- 任務 5：恢復原 session／可寫權限時舊面板重開的測試先失敗，增加 session 歸屬與失效狀態清理後通過。
- 任務 6：Forward 後下一次 Back 停在同 URL 的額外紀錄，新增前一筆 history.state 斷言先失敗；現在消耗自己已關閉的 Forward entry，並防止重複回退，測試通過。
- 背景捲動調查：測試替身每次 render 都提供新 messages 陣列，重觸發 ChatView 既有 scrollIntoView(smooth)。改 body overflow 及攔 backdrop wheel 均未解決，追蹤也顯示 wheel 前就開始捲動。使用符合正式 hook 的穩定陣列後重現消失；嘗試的產品 workaround 已移除，保留原生 dialog 與 CSS 捲動鎖。測試頁亦改為正式頁相同的 standards mode／body class。

## 真機仍未驗證

| 裝置／瀏覽器 | 版本 | 結果 |
| --- | --- | --- |
| iOS Safari／VoiceOver | 無可用裝置，未取得版本 | not-run |
| Android Chrome／TalkBack | 無可用裝置，未取得版本 | not-run |
| Telegram Mini App iOS／Android WebView | 無可用裝置，未取得版本 | not-run；自動測試僅使用 BackButton stub |

待實機執行：開鍵盤後按 Plus，確認鍵盤收起但面板保留；設定單選、選目前值及返回；系統附件取消、成功、多檔失敗／重試；瀏覽器／Telegram 返回、旋轉、安全區；VoiceOver／TalkBack 的宣告及焦點。桌面 headless 的 file chooser、可及性樹及 touch fixture 通過，不能代表上述真機通過。

## 範圍限制

工作目錄原有 Kiro CLI 唯讀變更，ChatView 在任務開始前即為 dirty；本次保留該行為。Spectra 能歸屬相關檔案，但同檔舊變更的 hunk 歸屬仍須比對任務 baseline，不能把整個 dirty tree 視為本提案。SessionTabs／其他後端與文件舊變更不屬本次實作，沒有一併提交或發佈。


## v0.18.10 發佈前 verify／review

2026-11-14：以 `spectra scope --change mobile-composer-sheet --json` 捕捉並檢查 snapshot `551f9bb8ee7838ae82c76a4084c2ad95e595d5f9`，check-snapshot 回報 current。scope_source=touched_tracking、status=resolved，base/head=8cc71b3160a7a83dfd3b8c625e2aaa490c41ee93，含當前未提交層。

- 完整性：6/6 任務、9/9 需求、32/32 場景有測試；無 Example 區塊。單一表面的精確視覺值沿用前述排除與截圖檢視。
- 一致性：已將 Accessible dismissal 的敘述澄清為 Close／遮罩直接關閉整個面板，Escape／Back 才先返回上一層，符合原核准 mock／design。補上在單選頁使用 Close／遮罩的測試，`spectra validate mobile-composer-sheet` 通過；analyze 只有 32 項補 Example 的 Suggestion，無 Critical／Warning。
- 程式 review：已檢查正確性、效率、簡化／重用與專案慣例；未發現需要修正的程式缺陷。檢查檔案為下方前六個檔案；另外檢查 app.js 快捷鍵焦點修正，以及 Kiro 唯讀的 factory、API、DB、MCP、WS 與前端入口。ChatView 開始前已有 dirty 內容，檔案歸屬不等於 hunk 歸屬；這個限制保留，Kiro 與快捷鍵由本次發版授權一併納入。
- passed-current：`python scripts/composer-attachment.test.py`（含新增的 Close／遮罩子頁情境及雙窗格快捷鍵／草稿／游標／唯讀／IME 回歸）。
- passed-current：`BROWSER_BIN="C:/Program Files/Google/Chrome/Application/chrome.exe" node --test --test-reporter=dot scripts/*.test.cjs`，85 項通過。首次用預設 Edge 執行時，markdown-security 的內容檢查通過但暫存 Crashpad 清理回報 ENOTEMPTY；獨立重現後改用測試原有 BROWSER_BIN 支援的 Chrome，全套通過，沒有停用測試或修改測試斷言。
- passed-current：`go test ./internal/agent ./internal/api ./internal/db ./internal/mcp ./internal/ws`，Kiro 唯讀回歸通過，保留歷史與佇列，Kiro ACP 繼續支援。
- 真機驗收仍為 not-run，沿用上表；依使用者「能做的做完後直接發布」指示保留限制並繼續發版。

已測內容 SHA-256（版本／發版說明之外的程式與測試）：

| 檔案 | SHA-256 |
| --- | --- |
| internal/static/AGENT.md | 3583b5be07c2bde11f6669d1d87985e92dc8a015bed4b36081ff798319fb0676 |
| internal/static/index.html | 22bb063b93456c966aa6692228db89f0ae4784a14e1f83f46dd56856aff25109 |
| internal/static/js/chat/ChatView.js | 2c1da4eefe5280335af180fdb47f15ce8123cf858ed47d5436d052a610f9325f |
| internal/static/js/chat/ComposerTools.js | 762d3a8a12dd6b65002434ce1caf9e6ecf555313c6f8cdfe2d454ed7e0371396 |
| internal/static/js/ui/ui-atoms.js | b49fef215eef74c7d738833091f9891b6b03ff3c80cef10b3029637c880e6eb3 |
| scripts/composer-attachment.test.py | b886fd43b2967d03fc8c69d99d2ca618696eeebeab195f5a95392d7a1b767689 |
| internal/static/js/app.js | 54c3c6a32edc58902c5f00132c0864fd762638e94b1427683605d2bf2d0233b6 |


## 隔離發版工作目錄的最終補驗

發版工作目錄以 origin/main（9435020，v0.18.9）為基底，整合手機面板、快捷鍵、Kiro 唯讀與版本文件。前述只針對手機任務的範圍限制不排除本次使用者另行授權的 Kiro／快捷鍵更新。

- 快捷鍵：單一 requestAnimationFrame 仍讓焦點晚於快捷鍵事件。新增事件結束前應已聚焦目標輸入框的斷言，原實作確實失敗；改用 ReactDOM.flushSync 完成窗格切換，再立即聚焦。保留原生 keypress 的反向切換、游標／草稿與 IME 測試，雙窗格流程連續 20 次通過。
- 唯讀測試：原 fixture 先非同步設定 guest，再立即按快捷鍵，可能聚焦仍可寫但即將移除的 textarea；補等目標 textarea 已移除，以符合「目標已唯讀」的前提，保留聚焦 pane 與不得改寫原草稿的斷言。這不代表實作支援在任意後續權限變動時自動接管焦點。
- Windows 子程序測試在四組檢查同時執行時曾超過 7 秒；單項重跑及完整 go test ./internal/... 均通過，未調整 timeout 或停用測試。go test -tags production ./internal/... 全部通過。
- Chrome 執行 scripts/*.test.cjs，85 項通過。真機驗收仍維持 not-run。
- Spectra scope 在 Git linked worktree 解析到原共用工作目錄（HEAD 8cc71b3）；發版 commit 另外以該隔離目錄的原生 Git 狀態、完整差異與 blob 指紋確認，不把這筆 Spectra scope 當成 release tree 的證據。

下列兩個 SHA-256 取代前表的相同檔案；其餘來源內容未變：

- `internal/static/js/app.js`: `e2bafa4ac6c403a51ee9e589c8f6b5468f2a43cbd3d9e54d22db9b8a323855a3`
- `scripts/composer-attachment.test.py`: `4a8ee9be81e435bde85b5185a1010bf2e6a185e37bc51f8c12a980505882c0a3`
