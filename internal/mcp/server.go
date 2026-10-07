package mcp

import (
	"net/http"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// NewHTTPHandler 建立掛在 /mcp 的 Streamable HTTP handler。
// reg 與自動接手共用 loopback 連線及狀態快取。
// maxHops 是 session 互問跳數上限；<=0 用 DefaultMaxHops。
func NewHTTPHandler(database *db.DB, quotaSvc *quota.Service, reg *Registry, maxHops int) http.Handler {
	d := &deps{
		db:      database,
		quota:   quotaSvc,
		maxHops: maxHops,
		reg:     reg,
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "claude-miniapp", Version: "1.0.0"}, nil)
	registerTools(server, d)

	return gomcp.NewStreamableHTTPHandler(func(*http.Request) *gomcp.Server { return server }, nil)
}
