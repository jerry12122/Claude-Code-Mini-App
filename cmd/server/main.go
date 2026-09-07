package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/claude"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/codex"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/config"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/cursor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/kiro"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/logging"
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

// syncModelOptions 啟動時抓各 agent 目前可用的模型清單並同步進 model_options 表。
// 任一 agent 取得失敗只記 log、不影響其他 agent，也不擋伺服器啟動。
func syncModelOptions(database *db.DB) {
	apply := func(agentType string, live []db.ModelOption, err error) {
		if err != nil {
			slog.Info(fmt.Sprintf("[startup] 取得 %s model 清單失敗，略過: %v", agentType, err))
			return
		}
		if err := database.SyncModelOptions(agentType, live); err != nil {
			slog.Info(fmt.Sprintf("[startup] SyncModelOptions(%s) 失敗: %v", agentType, err))
		}
	}

	claudeOpts := make([]db.ModelOption, 0)
	for _, e := range claude.ModelOptions() {
		claudeOpts = append(claudeOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeClaude, claudeOpts, nil)

	ctx := context.Background()

	cursorEntries, err := cursor.FetchModelOptions(ctx)
	cursorOpts := make([]db.ModelOption, 0, len(cursorEntries))
	for _, e := range cursorEntries {
		cursorOpts = append(cursorOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeCursor, cursorOpts, err)

	codexEntries, err := codex.FetchModelOptions()
	codexOpts := make([]db.ModelOption, 0, len(codexEntries))
	for _, e := range codexEntries {
		codexOpts = append(codexOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeCodex, codexOpts, err)

	// kiroacp（ACP 協定版）跟 kiro 共用同一顆 kiro-cli 二進位，模型清單視為相同。
	kiroEntries, err := kiro.FetchModelOptions(ctx)
	kiroOpts := make([]db.ModelOption, 0, len(kiroEntries))
	for _, e := range kiroEntries {
		kiroOpts = append(kiroOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeKiro, kiroOpts, err)
	apply(agent.TypeKiroACP, kiroOpts, err)
}

// webSessionToken Web 登入：Bearer、URL query（供 WebSocket）、或舊版 HttpOnly cookie。
func webSessionToken(c *fiber.Ctx) string {
	if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if t := c.Query("token"); t != "" {
		return t
	}
	return c.Cookies("session_token")
}

func main() {
	syncLogs := logging.Init()
	defer syncLogs()

	cfg, err := config.Load()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	// 解析允許的內網 CIDR
	allowedNets, err := auth.ParseCIDRs(cfg.Web.AllowedCIDRs)
	if err != nil {
		slog.Error(fmt.Sprintf("CIDR 設定錯誤: %v", err))
		os.Exit(1)
	}

	// 解析 session TTL
	sessionTTL, err := time.ParseDuration(cfg.Web.SessionTTL)
	if err != nil {
		slog.Error(fmt.Sprintf("web.session_ttl 格式錯誤 %q: %v", cfg.Web.SessionTTL, err))
		os.Exit(1)
	}
	sessions := auth.NewStore(sessionTTL)

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		slog.Error(fmt.Sprintf("DB 初始化失敗: %v", err))
		os.Exit(1)
	}
	defer database.Close()

	// 修復 crash 遺留的殘留狀態
	if err := database.ResetRunningSessions(); err != nil {
		slog.Info(fmt.Sprintf("[startup] ResetRunningSessions 失敗: %v", err))
	}
	if err := database.ResetPendingMessages(); err != nil {
		slog.Info(fmt.Sprintf("[startup] ResetPendingMessages 失敗: %v", err))
	}
	syncModelOptions(database)

	// 將 config.yaml 中的白名單寫入 DB
	for _, id := range cfg.WhitelistTgIDs {
		if err := database.AddUser(id, ""); err != nil {
			slog.Info(fmt.Sprintf("新增白名單使用者 %d 失敗: %v", id, err))
		}
	}
	if len(cfg.WhitelistTgIDs) > 0 {
		slog.Info(fmt.Sprintf("白名單已載入，共 %d 位使用者", len(cfg.WhitelistTgIDs)))
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})

	// 靜態 HTML（不需驗證）
	app.Static("/", "./internal/static")

	// --- 登入 / 登出端點 ---

	// POST /auth/login
	// Body: {"password": "...", "tg_user_id": 0}（tg_user_id 選填；省略時由 default_notify_tg_id 或單一白名單自動綁定）
	// 僅允許來自內網 IP；成功後設定 HttpOnly session cookie。
	app.Post("/auth/login", func(c *fiber.Ctx) error {
		if cfg.NoAuth {
			return c.Status(400).JSON(fiber.Map{"error": "no_auth 模式下不需登入"})
		}
		if cfg.Web.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "伺服器未設定 web 密碼"})
		}

		ip := auth.RealIP(c)
		if !auth.IsAllowed(ip, allowedNets) {
			slog.Warn("[auth] 拒絕登入請求，非內網 IP", "ip", ip)
			return c.Status(403).JSON(fiber.Map{"error": "僅允許內網存取"})
		}

		var body struct {
			Password string `json:"password"`
			TgUserID int64  `json:"tg_user_id"` // 選填：白名單內 Telegram ID，用於任務完成／授權通知
		}
		if err := c.BodyParser(&body); err != nil || body.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "請提供 password 欄位"})
		}
		if body.Password != cfg.Web.Password {
			slog.Warn("[auth] Web 密碼錯誤", "ip", ip)
			return c.Status(401).JSON(fiber.Map{"error": "密碼錯誤"})
		}

		var bindTgID int64
		if body.TgUserID != 0 {
			allowed, err := database.IsUserAllowed(body.TgUserID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
			}
			if !allowed {
				slog.Warn("[auth] Web 登入拒絕：不在白名單", "tg_user_id", body.TgUserID)
				return c.Status(403).JSON(fiber.Map{"error": "tg_user_id 不在白名單內"})
			}
			bindTgID = body.TgUserID
		} else {
			// 未手動指定時：設定檔預設 → 否則白名單僅一人時自動綁定
			if cfg.Web.DefaultNotifyTgID != 0 {
				ok, err := database.IsUserAllowed(cfg.Web.DefaultNotifyTgID)
				if err != nil {
					return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
				}
				if ok {
					bindTgID = cfg.Web.DefaultNotifyTgID
				} else {
					slog.Warn("[auth] default_notify_tg_id 不在白名單，略過", "tg_id", cfg.Web.DefaultNotifyTgID)
				}
			}
			if bindTgID == 0 {
				autoID, err := database.DefaultNotifyTgIDIfSingle()
				if err != nil {
					return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
				}
				bindTgID = autoID
			}
		}

		token, err := sessions.Create(bindTgID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "無法建立 session"})
		}
		slog.Info("[auth] Web 登入成功", "ip", ip)
		return c.JSON(fiber.Map{"ok": true, "token": token})
	})

	// POST /auth/logout
	// 清除 session cookie 與伺服器端 token。
	app.Post("/auth/logout", func(c *fiber.Ctx) error {
		token := webSessionToken(c)
		if token != "" {
			sessions.Delete(token)
		}
		c.Cookie(&fiber.Cookie{
			Name:     "session_token",
			Value:    "",
			MaxAge:   -1,
			HTTPOnly: true,
			SameSite: "Strict",
		})
		return c.JSON(fiber.Map{"ok": true})
	})

	// --- 驗證 Middleware ---
	authMiddleware := func(c *fiber.Ctx) error {
		if cfg.NoAuth {
			return c.Next()
		}

		// 方式一：Telegram initData（不限 IP）
		initData := c.Get("X-Telegram-Init-Data")
		if initData == "" {
			initData = c.Query("initData")
		}
		if initData != "" {
			user, err := tg.Verify(initData, cfg.BotToken)
			if err != nil {
				slog.Warn("[auth] TG 驗證失敗", "err", err)
				return c.Status(401).JSON(fiber.Map{"error": "Telegram 驗證失敗"})
			}
			allowed, err := database.IsUserAllowed(user.ID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
			}
			if !allowed {
				slog.Warn("[auth] 拒絕使用者", "tg_id", user.ID, "username", user.Username)
				return c.Status(403).JSON(fiber.Map{"error": "無存取權限"})
			}
			slog.Debug("[auth] TG 驗證通過", "tg_id", user.ID, "username", user.Username)
			c.Locals("tg_id", user.ID)
			return c.Next()
		}

		// 方式二：MCP service token（固定密鑰，供其他 agent 呼叫 /mcp，不限 IP、不過期）
		if cfg.McpToken != "" {
			if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
				if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.McpToken)) == 1 {
					return c.Next()
				}
			}
		}

		// 方式三：Web session cookie（限內網 IP）
		ip := auth.RealIP(c)
		if !auth.IsAllowed(ip, allowedNets) {
			slog.Warn("[auth] 拒絕非內網 IP", "ip", ip)
			return c.Status(403).JSON(fiber.Map{"error": "僅允許內網存取"})
		}

		token := webSessionToken(c)
		ok, webTgID := sessions.Validate(token)
		if !ok {
			return c.Status(401).JSON(fiber.Map{"error": "請先登入"})
		}
		if webTgID != 0 {
			c.Locals("tg_id", webTgID)
		}

		return c.Next()
	}

	// REST API
	sh := api.NewSessionHandler(database)
	app.Get("/sessions", authMiddleware, sh.List)
	app.Post("/sessions", authMiddleware, sh.Create)
	app.Post("/sessions/read-all", authMiddleware, sh.ReadAll)
	app.Patch("/sessions/:id", authMiddleware, sh.Patch)
	app.Delete("/sessions/:id", authMiddleware, sh.Delete)
	app.Get("/sessions/:id/messages", authMiddleware, sh.Messages)
	app.Get("/model-options/:agentType", authMiddleware, sh.ModelOptions)
	app.Get("/work-dirs", authMiddleware, sh.ListWorkDirs)

	sth := api.NewSettingsHandler(database)
	app.Get("/settings/appearance", authMiddleware, sth.GetAppearance)
	app.Put("/settings/appearance", authMiddleware, sth.PutAppearance)
	app.Get("/settings/general", authMiddleware, sth.GetGeneral)
	app.Put("/settings/general", authMiddleware, sth.PutGeneral)

	// GET /config — 前端用來判斷是否顯示「開啟 VSCode / 開啟目錄」等依賴伺服器端本機程序的功能。
	app.Get("/config", authMiddleware, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"shell_enabled": cfg.Shell.Enabled})
	})

	oh := api.NewOpenHandler(database, cfg.Shell.Enabled)
	app.Post("/sessions/:id/open-vscode", authMiddleware, oh.OpenVSCode)
	app.Post("/sessions/:id/open-folder", authMiddleware, oh.OpenFolder)

	quotaSvc := quota.NewService()
	go quotaSvc.Warmup(context.Background())
	qh := api.NewQuotaHandler(quotaSvc)
	app.Get("/quota", authMiddleware, qh.GetAll)
	app.Get("/quota/:provider", authMiddleware, qh.Get)
	app.Post("/quota/:provider/refresh", authMiddleware, qh.Refresh)

	// WebSocket
	app.Use("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	shellOpts := ws.ShellOpts{
		Enabled:         cfg.Shell.Enabled,
		Timeout:         cfg.Shell.Timeout,
		MaxOutputBytes:  cfg.Shell.MaxOutputBytes,
		AllowedCommands: cfg.Shell.AllowedCommands,
	}
	notifyCfg := tg.NotifyConfig{
		OnError:          cfg.Notify.OnError,
		OnCancel:         cfg.Notify.OnCancel,
		OnShellError:     cfg.Notify.OnShellError,
		ErrorPreviewLen:  cfg.Notify.ErrorPreviewLen,
		IncludePrompt:    cfg.Notify.IncludePrompt,
		PromptPreviewLen: cfg.Notify.PromptPreviewLen,
	}
	app.Get("/sessions/:id/ws", authMiddleware, fiberws.New(ws.NewHandler(database, cfg.BotToken, shellOpts, quotaSvc, notifyCfg)))

	// MCP：讓其他 agent 透過 Streamable HTTP 操作本服務（需設定 mcp_token 才會啟用）
	if cfg.McpToken != "" {
		mcpHandler := mymcp.NewHTTPHandler(database, quotaSvc, cfg.Server.Port, cfg.McpToken)
		app.Post("/mcp", authMiddleware, adaptor.HTTPHandler(mcpHandler))
	} else {
		slog.Info("[mcp] mcp_token 未設定，/mcp 停用")
	}

	if cfg.NoAuth {
		slog.Info("⚠️  no_auth: true，已跳過 Telegram 驗證（僅限開發環境）")
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info(fmt.Sprintf("Claude Code Mini App v%s listening on %s", version.Version, addr))
	if err := app.Listen(addr); err != nil {
		slog.Error(fmt.Sprintf("server 結束: %v", err))
		os.Exit(1)
	}
}
