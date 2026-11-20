# 下一版發布計畫

狀態：規劃，尚未改版號、建置正式產物、打 tag 或 push。
規劃基準：main `d02a6226b192ef63a671770d7819db7239b03f50`；目前版本 `0.21.1`。
已核對 origin：`github.com:jerry12122/Claude-Code-Mini-App.git`；執行發布時再核對遠端。尚無本機 `v0.22.0` tag，不代表遠端名稱已預留。
建議下一版：`0.22.0`，因為新增工作室、佇列能力及 MCP 契約變更。版號待使用者定案。

## 發布範圍

- 工作室／PM：整合時間軸、成員篩選、搜尋加入、草稿與 @、附件、看板及交接。
- 工作紀錄：job ID、工作標題、原指示、派工時姓名與結果快照；改名或刪 worker 後仍可查。
- 普通與 Room 佇列：編輯、獨立 FIFO 面板、一起送、取消與重整恢復；一起送等目前回合正常結束，後到訊息不入舊批。
- 工作流守門：權威版本、完成通知去重、清理操作的使用者批准。
- 一般使用改善：滿寬與緊湊介面、全文展開去重、設定頁、額度自動繼續與可調守門、Claude 授權選項。
- quota/Cockpit：指定 auth 失敗時唯讀歷史估值，明示非即時並保留 stale，不冒充即時額度。
- 效能與資料：預編譯 CSS、靜態壓縮、版本化 DB 遷移。
- 隨附 PM skill 撰寫指南與可修改範例，入口已在 README。

RunNow 程式已在 main，最後一項真 Claude Windows 驗證尚未完成。發布前須取得結果，不把 fake CLI 當替代。
既存 `event-dispatch-seams` 三項重構不納入本次，不增加其他功能。
本機的正式 supervisor-workflow 與 runner junction 是個人設定；不打包個人 skill、帳號、token 或暫存證據。公開教學範例隨 docs 打包。

## 順序與放行關卡

### 1. 關閉 RunNow 驗證缺口

- 先核對真 provider 額度與授權，再由單一執行者做受控驗證；不碰現有工作中的會話或正式服務。
- 以隔離 MiniApp、臨時 DB／埠、專用測試會話走真 Claude 回合與 queue_run_now；驗證舊程序樹結束後新回合才啟動、resume 成功、其餘佇列保留、transcript 無互相覆寫或未配對錯誤。
- 保存原始碼版本、PID／時間序列、指令與脫敏結果；PM 獨立核對後才勾 `docs/spectra/changes/queue-run-now/tasks.md` 的 5.1。
- 遇到 quota、授權或產品失敗即記錄具體原因；不連續盲試、不宣稱 12/12。未取得有效證據則暫停發布，由使用者決定延後或另定縮減範圍。

### 2. 整理發布來源與說明

- 版號定案後同步 VERSION、internal/version/version.go、兩份 README badge；依現有工具產生 Windows 版本資源。
- 補 CHANGELOG 的完整版本段落，按一般聊天、工作室、效能／資料及相容性分類。寫清非即時 Cockpit、額度自動續跑限制與預設值。
- MCP 相容性說明：指定寫入需要 expected_version；dispatch 需要 title；對現任 PM 的 send_message 需要來源、intent，正式 worker 另帶 job_id。提示重載工具 schema 與核對自訂 skill。
- 說明新增 DB migration 的升級與還原方式。先取得一致性 SQLite 備份（包含 WAL 的一致性，或使用 backup API），在副本上升級演練；回退不能只換舊 binary 讀新 schema。
- 發布日期採實際發布日。完成來源與文件後，記下最終 release commit F；tag、檔案版本及建置來源都必須對應 F。

### 3. 同來源候選建置與驗收

- 重用仍有效的來源／測試證據；版本資源、測試、依賴或環境變動時只補受影響檢查。專案發布流程要求的完整檢查仍須完成。
- 依 `.github/workflows/release.yml` 與既有 scripts 驗證：桌面版本資源／啟動、漏洞掃描（Windows server／desktop 與 Linux server）、internal 正常與 production 測試、CSS --check、產物契約、隔離安裝／移除測試。
- 使用專屬、全新的輸出目錄打包，不覆蓋現有候選或共用 dist。既有打包腳本會清輸出目錄內同名資產，執行前先核對目標路徑及內容。
- 驗證桌面／server 啟動、使用者資料保留、0.21.1 升級與 migration 副本；檢查佇列、PM 確認面板及工具契約的關鍵路徑。真機或外部平台未驗的限制分開記錄。
- 現有工具只接受 `X.Y.Z`；候選以本機資料夾、來源 commit 與 hash 識別，不另造會被流程拒絕的 `v0.22.0-rc.1` tag。

若採 0.22.0，應有以下五項資產：

| 資產 | 用途 |
| --- | --- |
| claude-miniapp-v0.22.0-windows-amd64.zip | Windows server／桌面可攜包 |
| claude-miniapp-v0.22.0-linux-amd64.tar.gz | Linux server |
| claude-miniapp-desktop-v0.22.0-windows-amd64.exe | 桌面自動更新 |
| claude-miniapp-v0.22.0-windows-amd64-setup.exe | Windows 目前使用者安裝包 |
| checksums.txt | 精確涵蓋上述四個程式資產 |

ZIP 內 desktop 與獨立更新 EXE 必須是同一份 bytes。舊 review binary 建於較早來源且未啟動，不拿它冒充本次最終候選。

### 4. 使用者批准公開發布

前述準備完成後，提供可審閱的最終 commit F、版本／tag、CHANGELOG、驗證摘要、產物與 checksum，再請使用者確認：

- 推送到本專案的確切 remote 與 main commit F；執行前重新核對 origin／遠端狀態，不 force push。
- 建立並推送指向 F 的 `v0.22.0`（或實際定案版本）tag。
- 允許既有 release workflow 在驗證成功後公開 Release 並設為 latest，讓桌面 updater 可見。

**推送 v* tag 已等於觸發公開發布流程。** 現有 CI 先建草稿、上傳並下載比對資產，成功後自動 `--draft=false --latest`；草稿階段沒有第二次人工批准關卡。若需要「先只跑遠端草稿」，應另經授權調整流程，不能假設現在已有這項能力。

本次「規劃發布」不授權上述遠端寫入或正式服務更新；不推 tag 來試跑。

### 5. 發布後查核

- 核對公開 tag/F、Release 五資產、checksums 與 updater 解析到的版本。
- 對公開下載取得的安裝／更新產物做最小驗證，再回報公開連結與已知限制。
- CI 中途失敗依既有草稿復用流程處理；不擅自移 tag、換 commit、覆寫已公開資產或降回舊 DB。
- 發版不代表自動清理 worker、工作樹或工作室。若要清理，另列具體目標／後果並取得確認。

## 分工與用量

發布準備指定一位執行者，PM 負責範圍、驗收與最終放行。每個工作包只有一份 final 回報；缺陷集中退修，避免排多筆補充。新成果用新 job／來源版本，重用仍有效的檢查，不重做無關功能。

## 現在狀態

本文件完成後仍停在規劃階段，未派真 provider 驗證、未改版本、未建正式產物、未打 tag／push，也未重啟服務或清理資料。
