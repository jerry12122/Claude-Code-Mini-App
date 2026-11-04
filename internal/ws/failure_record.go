package ws

import (
	"fmt"
	"log/slog"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// recordSessionFailure 把 run 失敗的原始錯誤文字存進 session，供外部 agent 經 MCP list_sessions 查詢。
// 寫入失敗只記 log，不影響原本的失敗處理流程。
func recordSessionFailure(database *db.DB, sessionID, text string) {
	if err := database.SetSessionLastError(sessionID, text); err != nil {
		slog.Info(fmt.Sprintf("[ws] SetSessionLastError: %v", err))
	}
}

// clearSessionFailure 在 run 成功後清掉失敗紀錄。
func clearSessionFailure(database *db.DB, sessionID string) {
	if err := database.ClearSessionLastError(sessionID); err != nil {
		slog.Info(fmt.Sprintf("[ws] ClearSessionLastError: %v", err))
	}
}
