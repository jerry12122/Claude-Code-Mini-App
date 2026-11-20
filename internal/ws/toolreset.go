package ws

import (
	"errors"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// resetRoomMember 只在 dispatchLock 內呼叫，所以記憶體中的任務不會在檢查後才啟動。
// 資料庫前提（閒置、僅本室、仍是 PM）與清除由 ResetRoomMemberSession 在同一交易內完成。
func resetRoomMember(database *db.DB, sessionID, roomID, pmID string, approval ...string) error {
	if taskIsActive(sessionID) || shellTaskActive(sessionID) || peekShellPending(sessionID) != nil || questionPending(sessionID) != nil {
		return errors.New("session 忙碌，未重置")
	}
	if _, waiting := permPending(sessionID); waiting {
		return errors.New("session 忙碌，未重置")
	}
	return database.ResetRoomMemberSession(sessionID, roomID, pmID, approval...)
}
