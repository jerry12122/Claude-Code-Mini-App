package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

// RequestOperationApproval 重用使用者問答面板；不經模型的 autoAllow，也沒有 MCP 核准工具。
func RequestOperationApproval(ctx context.Context, database *db.DB, action, roomID, pmID, target string, expected ...string) (string, error) {
	if !hub.has(pmID) {
		return "", fmt.Errorf("尚未取得使用者批准；請先開啟 PM 會話，重試此工具並由使用者在確認面板批准；MCP 不可代答")
	}
	id, state, err := database.RequestOperationApproval(action, roomID, pmID, target, expected...)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	question := fmt.Sprintf("確認 %s？\n工作室：%s\nPM：%s\n目標：%s\n操作：%s\n狀態：%s\n%s", action, roomID, pmID, target, id, state, operationCleanup(action))
	input, _ := json.Marshal(map[string]any{"questions": []any{map[string]any{"question": question, "header": "清理確認", "options": []any{map[string]string{"label": "取消", "description": "保留資料"}, map[string]string{"label": "批准這次操作", "description": "只批准上列範圍與狀態，五分鐘內有效"}}}}})
	q, err := newQuestion(input)
	if err != nil {
		return "", err
	}
	q.ctx = ctx
	q.operation = true
	pendingQuestions.Lock()
	if pendingQuestions.m[pmID] != nil {
		pendingQuestions.Unlock()
		return "", fmt.Errorf("PM 已有待回答問題；請先在會話面板回答後重試")
	}
	pendingQuestions.m[pmID] = q
	pendingQuestions.Unlock()
	defer func() {
		pendingQuestions.Lock()
		if pendingQuestions.m[pmID] == q {
			delete(pendingQuestions.m, pmID)
		}
		pendingQuestions.Unlock()
		hub.Broadcast(pmID, serverMsg{Type: "question_resolved", RequestID: q.RequestID})
	}()
	hub.Broadcast(pmID, serverMsg{Type: "question_request", Question: q})
	select {
	case out := <-q.ch:
		var answer struct {
			Answers map[string]string `json:"answers"`
		}
		if json.Unmarshal(out, &answer) != nil || answer.Answers[question] != "批准這次操作" || ctx.Err() != nil {
			return "", db.ErrOperationApproval
		}
		if err := database.ApproveOperation(id); err != nil {
			return "", err
		}
		return id, nil
	case <-ctx.Done():
		return "", fmt.Errorf("尚未取得使用者批准，資料未改動；請開啟 PM 會話確認面板後重試：%w", ctx.Err())
	}
}

func operationCleanup(action string) string {
	switch action {
	case "room_delete":
		return "刪除成員關係、備忘、派工與本工作室佇列／批次；保留 session 與歷史。"
	case "reset_session":
		return "清除目標的續接、訊息、佇列，取消待處理批次與額度續跑；保留 session 與 provider 本機歷史。"
	default:
		return "刪除目標 session、訊息、佇列、輸入與批次紀錄、成員關係及無共用存取的附件 metadata，撤銷分享；不刪 provider 本機檔案。"
	}
}

// DeleteSessionApproved 與派工共用鎖，防止閒置檢查後才啟動任務。
func DeleteSessionApproved(database *db.DB, sessionID, roomID, pmID, approval string) error {
	lock := dispatchLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	if taskIsActive(sessionID) || shellTaskActive(sessionID) || peekShellPending(sessionID) != nil || questionPending(sessionID) != nil {
		return fmt.Errorf("session 忙碌，未刪除")
	}
	if _, waiting := permPending(sessionID); waiting {
		return fmt.Errorf("session 待授權，未刪除")
	}
	return database.DeleteSessionApproved(sessionID, roomID, pmID, approval)
}

// DB 狀態可能先回 idle、實際程序尚在收尾；鎖內連記憶體任務一起重驗。
func DeleteRoomApproved(database *db.DB, roomID, pmID, approval string) error {
	room, err := database.GetRoom(roomID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(room.Members))
	for _, member := range room.Members {
		ids = append(ids, member.SessionID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		dispatchLock(id).Lock()
	}
	defer func() {
		for i := len(ids) - 1; i >= 0; i-- {
			dispatchLock(ids[i]).Unlock()
		}
	}()
	for _, id := range ids {
		if id == pmID {
			continue
		}
		if taskIsActive(id) || shellTaskActive(id) || peekShellPending(id) != nil || questionPending(id) != nil {
			return fmt.Errorf("成員忙碌，未刪除工作室")
		}
		if _, waiting := permPending(id); waiting {
			return fmt.Errorf("成員待授權，未刪除工作室")
		}
	}
	return database.DeleteRoomAsPM(roomID, pmID, approval)
}
