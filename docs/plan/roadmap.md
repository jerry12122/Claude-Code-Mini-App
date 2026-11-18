# Roadmap

> 定位：廠商中立的 coding agent 控制台——手機遠端操作 + 跨廠商 agent 協作。
> 差異化（相對 Paseo 等）：免安裝（TG Mini App）、單一 Go binary、Kiro ACP。
> 規模 S/M/L 為相對大小。細項進 `todo.md`，完成移 `done/`。

## Phase 0 — 零／極小程式碼
- [x] 計畫文件整理
- [ ] `AGENTS.md` 單一來源（取代 CLAUDE.md / .cursor/rules / .kiro/steering 三份）— S
- [ ] handoff / advisor / committee skill（改寫自 Paseo，保留 Apache-2.0 聲明）— S
- [ ] Provider 診斷 endpoint（`LookPath` + `--version`）— S

## Phase 1 — 跨 agent 地基
- [ ] ① MCP 權限收斂：預設不含 shell、per-session scoped token（**② 的前提**）— M
- [ ] ② 各 runner 自動注入 miniapp MCP — M
- [ ] ③ spawn 帶 `MINIAPP_SESSION_ID`，sessions 加 `parent_id` — S
- [x] ④ `ask_session` 阻塞等待 + timeout — M
- [x] ⑤ envelope hop 計數 — S

## Phase 2 — 手機收尾閉環（可與 Phase 1 平行）
- [ ] 唯讀 diff 檢視器 — M
- [x] 上傳圖片／檔案 — M
- [x] 執行中排隊下一則訊息 — S
- [ ] 列表依狀態分組 + `+N -M` — S

## Phase 3 — 跨廠商協作
- [ ] Agent profiles（provider/model/mode/notes）+ MCP `list_profiles` — S
- [ ] Handoff UI（原 agent 產交接摘要 + git 狀態）— M
- [ ] git worktree 隔離 — M
- [ ] commit / discard（寫入，需二次確認）— S
- [ ] 額度將盡建議換手 — S
- [ ] Supervisor 工作室：虛擬群組檢視 + @ 路由 + 共享脈絡（見 `todo/supervisor-room.md`；第一期為零程式碼）— M

## Phase 4 — 自動化
- [ ] TG 通知 inline 授權（callback_query + 白名單驗證）— M
- [ ] 排程（額度低於門檻自動暫停）— M
- [ ] 結構化輸出驗收迴圈（有迭代上限）— M

## 需要時再做
- 桌面視窗殼（Wails，見 `todo/desktop-window.md`）：同一行程多開一個視窗，不取代網頁／Mini App；自我更新與發佈已於 2026-10-16 完成（見 `docs/spectra/specs/desktop-self-update`）
- 統一 ACP（見 `todo/codex-acp-migration.md`，暫緩）
- 歷史訊息全文搜尋（SQLite FTS5）
- 更多 CLI

## 明確不做
原生 App、E2E relay、plugin 系統、把外部網站嵌進視窗或 port 轉發、DAG 編排引擎。

## 依賴
```
Phase 0 → Phase 1 ①→②→③→④⑤ → Phase 3 → Phase 4
          Phase 2（平行）── diff ──┘
```
