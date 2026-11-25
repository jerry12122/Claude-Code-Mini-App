package mcp

import (
	"context"
	"net/http"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// NewHTTPHandler 建立掛在 /mcp 的 Streamable HTTP handler。
// reg 與自動接手共用 loopback 連線及狀態快取。
// maxHops 是 session 互問跳數上限；<=0 用 DefaultMaxHops。
func NewHTTPHandler(database *db.DB, quotaSvc *quota.Service, reg *Registry, maxHops int, botToken ...string) http.Handler {
	bt := ""
	if len(botToken) > 0 {
		bt = botToken[0]
	}
	d := &deps{
		db:       database,
		quota:    quotaSvc,
		maxHops:  maxHops,
		reg:      reg,
		botToken: bt,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "claude-miniapp", Version: "1.0.0"}, nil)
	server.AddReceivingMiddleware(toolCallLogMiddleware)
	registerTools(server, d)
	registerRoomTools(server, database, "room_", func(_ *gomcp.CallToolRequest, roomID string) (string, string, error) {
		pm, err := database.RoomPM(roomID)
		return roomID, pm, err
	})
	gomcp.AddTool(server, &gomcp.Tool{Name: "list_rooms", Description: "列出工作室名稱和 room_id，供手動接手或呼叫 room_ 工具"}, func(_ context.Context, _ *gomcp.CallToolRequest, _ struct{}) (*gomcp.CallToolResult, listRoomsOut, error) {
		rooms, err := database.ListRooms()
		return nil, listRoomsOut{Rooms: rooms}, err
	})

	return gomcp.NewStreamableHTTPHandler(func(*http.Request) *gomcp.Server { return server }, nil)
}

type listRoomsOut struct {
	Rooms []*db.RoomSummary `json:"rooms"`
}
