package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type roomMemoIn struct {
	RoomID string `json:"room_id,omitempty"`
	Text   string `json:"text" jsonschema:"完整備忘，可用 Markdown 待辦。先讀現有備忘再更新。"`
}

type roomWorkerIn struct {
	RoomID    string `json:"room_id,omitempty"`
	Name      string `json:"name"`
	AgentType string `json:"agent_type,omitempty"`
}

type roomDispatchIn struct {
	RoomID    string `json:"room_id,omitempty"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

type roomRenameIn struct {
	RoomID    string `json:"room_id,omitempty"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

type roomLookup struct {
	RoomID string `json:"room_id,omitempty"`
}

func NewRoomHTTPHandler(database *db.DB) http.Handler {
	s := gomcp.NewServer(&gomcp.Implementation{Name: "miniapp-room", Version: "1.0.0"}, nil)
	registerRoomTools(s, database, "", func(req *gomcp.CallToolRequest, requested string) (string, string, error) {
		extra := req.GetExtra()
		if extra == nil {
			return "", "", db.ErrRoomPM
		}
		pm := extra.Header.Get(PermSessionHeader)
		room, err := database.RoomForPM(pm)
		if requested != "" && requested != room {
			return "", "", db.ErrRoomPM
		}
		return room, pm, err
	})
	return gomcp.NewStreamableHTTPHandler(func(*http.Request) *gomcp.Server { return s }, &gomcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func registerRoomTools(s *gomcp.Server, database *db.DB, prefix string, scope func(*gomcp.CallToolRequest, string) (string, string, error)) {

	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "get_board", Description: "查看工作室備忘、派工狀態與成員"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomLookup) (*gomcp.CallToolResult, any, error) {
		roomID, _, err := scope(req, in.RoomID)
		if err != nil {
			return nil, nil, err
		}
		board, err := database.RoomBoard(roomID)
		if err != nil {
			return nil, nil, err
		}
		room, err := database.GetRoom(roomID)
		if err != nil {
			return nil, nil, err
		}
		members := make([]map[string]string, 0, len(room.Members))
		for _, m := range room.Members {
			members = append(members, map[string]string{"session_id": m.SessionID, "name": m.SessionName, "role": m.Role, "status": m.Status, "agent_type": m.AgentType})
		}
		// Results are explicit text so clients also work without structured tool output support.
		content, err := json.Marshal(map[string]any{"room": room.Name, "room_id": room.ID, "memo": board.Memo, "jobs": board.Jobs, "members": members})
		if err != nil {
			return nil, nil, err
		}
		return &gomcp.CallToolResult{Content: []gomcp.Content{&gomcp.TextContent{Text: string(content)}}}, nil, nil
	})
	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "save_memo", Description: "保存工作室備忘全文，支援 Markdown 與待辦清單"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomMemoIn) (*gomcp.CallToolResult, okOut, error) {
		roomID, pm, err := scope(req, in.RoomID)
		if err != nil {
			return nil, okOut{}, err
		}
		if len(in.Text) > 32_000 {
			return nil, okOut{}, errors.New("備忘最多 32 KB")
		}
		err = database.SaveRoomMemo(roomID, pm, strings.TrimSpace(in.Text))
		return nil, okOut{OK: err == nil}, err
	})
	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "create_worker", Description: "在工作室建立執行者會話，使用 PM 的工作目錄與預設權限"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomWorkerIn) (*gomcp.CallToolResult, createdWorker, error) {
		roomID, pm, err := scope(req, in.RoomID)
		if err != nil {
			return nil, createdWorker{}, err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" || utf8.RuneCountInString(name) > 100 {
			return nil, createdWorker{}, errors.New("執行者名稱須為 1 到 100 字")
		}
		at := strings.TrimSpace(in.AgentType)
		if at == "" {
			at = agent.TypeClaude
		}
		if !agent.CanCreate(at) || at == agent.TypeTelegram {
			return nil, createdWorker{}, errors.New("不支援這個執行者類型")
		}
		id, err := database.CreateRoomWorker(roomID, pm, name, at)
		return nil, createdWorker{SessionID: id}, err
	})
	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "dispatch", Description: "交辦訊息給本工作室的閒置執行者，完成後回報給目前的 PM"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomDispatchIn) (*gomcp.CallToolResult, dispatchedJob, error) {
		roomID, pm, err := scope(req, in.RoomID)
		if err != nil {
			return nil, dispatchedJob{}, err
		}
		worker, err := database.GetSession(in.SessionID)
		if err != nil || !agent.CanCreate(worker.AgentType) {
			return nil, dispatchedJob{}, db.ErrRoomWorker
		}
		text := strings.TrimSpace(in.Text)
		if text == "" || len(text) > 64_000 {
			return nil, dispatchedJob{}, errors.New("交辦內容須為 1 到 64000 bytes")
		}
		id, err := database.DispatchRoomJob(roomID, pm, worker.ID, text)
		return nil, dispatchedJob{JobID: id}, err
	})
	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "rename_session", Description: "修改本工作室成員的會話名稱，包含 PM 自己"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomRenameIn) (*gomcp.CallToolResult, okOut, error) {
		roomID, pm, err := scope(req, in.RoomID)
		if err != nil {
			return nil, okOut{}, err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" || utf8.RuneCountInString(name) > 100 {
			return nil, okOut{}, errors.New("會話名稱須為 1 到 100 字")
		}
		err = database.RenameRoomSession(roomID, pm, in.SessionID, name)
		return nil, okOut{OK: err == nil}, err
	})
	gomcp.AddTool(s, &gomcp.Tool{Name: prefix + "get_history", Description: "讀取工作室最近 20 則訊息的發送者、收件者、文字與附件"}, func(_ context.Context, req *gomcp.CallToolRequest, in roomLookup) (*gomcp.CallToolResult, roomHistoryOut, error) {
		roomID, _, err := scope(req, in.RoomID)
		if err != nil {
			return nil, roomHistoryOut{}, err
		}
		page, err := database.RoomTimeline(roomID, nil, 20)
		if err != nil {
			return nil, roomHistoryOut{}, err
		}
		out := roomHistoryOut{Messages: make([]roomHistoryMessage, 0, len(page.Items))}
		for _, item := range page.Items {
			text, truncated := truncateRunes(item.Content, 4000)
			out.Messages = append(out.Messages, roomHistoryMessage{Sender: item.Sender, Recipient: item.Recipient, Text: text, Truncated: truncated, Attachments: item.Attachments})
		}
		return nil, out, nil
	})
}

type roomHistoryOut struct {
	Messages []roomHistoryMessage `json:"messages"`
}

type roomHistoryMessage struct {
	Sender      string          `json:"sender"`
	Recipient   string          `json:"recipient,omitempty"`
	Text        string          `json:"text"`
	Truncated   bool            `json:"truncated,omitempty"`
	Attachments []db.Attachment `json:"attachments,omitempty"`
}

type createdWorker struct {
	SessionID string `json:"session_id"`
}

type dispatchedJob struct {
	JobID int64 `json:"job_id"`
}
