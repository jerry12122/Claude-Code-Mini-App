// Package server 組裝 Fiber（靜態頁、REST、WebSocket、MCP）並聽 port。
// cmd/server 與 cmd/desktop 共用這份啟動流程。
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
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
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

// Server 是已綁定 port 的 Fiber 服務。
// Wait 與 Shutdown 擇一呼叫：前者阻塞到行程結束，後者關掉監聽。
type Server struct {
	app  *fiber.App
	url  string
	done chan error
}

// URL 是本機視窗要開的位址，例如 http://127.0.0.1:8080。
// 監聽仍是 :port（所有介面），瀏覽器與 Mini App 可以從區網連。
func (s *Server) URL() string {
	return s.url
}

// Wait 阻塞到 HTTP 服務結束。
func (s *Server) Wait() error {
	return <-s.done
}

// Shutdown 停止接受連線，並等 Listener 返回或 ctx 逾時。
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.app.ShutdownWithContext(ctx)
	select {
	case <-s.done:
	case <-ctx.Done():
	}
	return err
}

// Start 讀取工作目錄的 config.yaml、開啟 DB、綁定 port。
// 綁定成功才回傳；呼叫端需已呼叫 logging.Init。
func Start(ctx context.Context) (*Server, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	allowedNets, err := auth.ParseCIDRs(cfg.Web.AllowedCIDRs)
	if err != nil {
		return nil, fmt.Errorf("CIDR 設定錯誤: %v", err)
	}
	if _, err := auth.ParseCIDRs(cfg.Web.TrustedProxies); err != nil {
		return nil, fmt.Errorf("web.trusted_proxies 設定錯誤: %v", err)
	}

	sessionTTL, err := time.ParseDuration(cfg.Web.SessionTTL)
	if err != nil {
		return nil, fmt.Errorf("web.session_ttl 格式錯誤 %q: %v", cfg.Web.SessionTTL, err)
	}
	sessions := auth.NewStore(sessionTTL)

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("DB 初始化失敗: %v", err)
	}

	if err := database.ResetRunningSessions(); err != nil {
		slog.Info(fmt.Sprintf("[startup] ResetRunningSessions 失敗: %v", err))
	}
	if err := database.ResetPendingMessages(); err != nil {
		slog.Info(fmt.Sprintf("[startup] ResetPendingMessages 失敗: %v", err))
	}
	// 背景執行：各 CLI 抓清單各要 1 秒上下，而且失敗本來就不擋啟動。
	go syncModelOptions(database)

	for _, id := range cfg.WhitelistTgIDs {
		if err := database.AddUser(id, ""); err != nil {
			slog.Info(fmt.Sprintf("新增白名單使用者 %d 失敗: %v", id, err))
		}
	}
	if len(cfg.WhitelistTgIDs) > 0 {
		slog.Info(fmt.Sprintf("白名單已載入，共 %d 位使用者", len(cfg.WhitelistTgIDs)))
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage:   false,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          cfg.Web.TrustedProxies,
		// 預設 4MB；使用者上傳不限制大小，故放寬到 fasthttp 可表示的最大值（約 2GB）。
		// ponytail: 全域放寬，只有上傳需要；若要收緊，改成只對 /sessions/:id/uploads 放寬。
		BodyLimit: math.MaxInt32,
	})

	// 沒帶 Cache-Control 時 Chromium 會依 Last-Modified 啟發式快取，改了前端檔案後 F5 仍吃舊的（桌面版又沒有強制重載）。
	// no-cache = 每次向 server 驗證，沒變動回 304，不是不快取。
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-cache")
		return c.Next()
	})
	app.Static("/", "./internal/static")

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
			TgUserID int64  `json:"tg_user_id"`
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

	authMiddleware := func(c *fiber.Ctx) error {
		if cfg.NoAuth {
			// no_auth 下帶訪客 token 的請求仍須受角色／範圍限制，否則開發時無法驗證訪客模式。
			if t := guestToken(c); t != "" {
				return guestAuth(database, c, t)
			}
			return c.Next()
		}

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

		// 臨時分享的訪客（guest token）：在 TG 驗證之後、內網 IP 檢查之前；訪客來自外網，不受內網限制，
		// 改由 guestAuth 做 token 有效性（撤銷／到期）與路由範圍檢查。
		if t := guestToken(c); t != "" {
			return guestAuth(database, c, t)
		}

		if cfg.McpToken != "" {
			if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
				if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.McpToken)) == 1 {
					return c.Next()
				}
			}
		}

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

	shareH := api.NewShareHandler(database, api.ShareHooks{Kick: ws.KickShare, Online: ws.ShareOnline}, "./internal/static")
	sh := api.NewSessionHandler(database)
	sh.OnSharesRevoked = shareH.KickAll
	app.Get("/sessions", authMiddleware, sh.List)
	app.Post("/sessions", authMiddleware, sh.Create)
	app.Post("/sessions/read-all", authMiddleware, sh.ReadAll)
	app.Patch("/sessions/:id", authMiddleware, sh.Patch)
	app.Delete("/sessions/:id", authMiddleware, sh.Delete)
	app.Get("/sessions/:id/messages", authMiddleware, sh.Messages)
	app.Get("/model-options/:agentType", authMiddleware, sh.ModelOptions)
	// 不必重啟就重新抓各 CLI 的模型清單；會等到抓完才回（數秒），failed 為抓取失敗的 agent_type。
	app.Post("/model-options/refresh", authMiddleware, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"failed": syncModelOptions(database)})
	})
	app.Get("/work-dirs", authMiddleware, sh.ListWorkDirs)

	// 臨時共享聊天室：擁有者端走 authMiddleware；訪客加入頁與 join 不需驗證（join 靠 PIN＋限流＋鎖定）。
	app.Post("/sessions/:id/shares", authMiddleware, shareH.Create)
	app.Get("/sessions/:id/shares", authMiddleware, shareH.ListBySession)
	app.Get("/shares", authMiddleware, shareH.ListAll)
	app.Delete("/shares/:id", authMiddleware, shareH.Revoke)
	app.Delete("/shares", authMiddleware, shareH.RevokeAll)
	app.Get("/guest/me", authMiddleware, shareH.Me)
	app.Get("/share/:token", shareH.Page)
	app.Post("/share/:token/join", api.JoinLimiter(), shareH.Join)

	sth := api.NewSettingsHandler(database)
	app.Get("/settings/appearance", authMiddleware, sth.GetAppearance)
	app.Put("/settings/appearance", authMiddleware, sth.PutAppearance)
	app.Get("/settings/general", authMiddleware, sth.GetGeneral)
	app.Put("/settings/general", authMiddleware, sth.PutGeneral)

	app.Get("/config", authMiddleware, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"shell_enabled": cfg.Shell.Enabled})
	})

	oh := api.NewOpenHandler(database, cfg.Shell.Enabled)
	app.Post("/sessions/:id/open-vscode", authMiddleware, oh.OpenVSCode)
	app.Post("/sessions/:id/open-folder", authMiddleware, oh.OpenFolder)

	uh := api.NewUploadHandler(database)
	app.Post("/sessions/:id/uploads", authMiddleware, uh.Upload)
	app.Get("/sessions/:id/uploads/:attachmentId/details", authMiddleware, uh.Details)
	app.Get("/sessions/:id/uploads/:attachmentId", authMiddleware, uh.Content)

	quotaSvc := quota.NewService()
	go quotaSvc.Warmup(context.Background())
	qh := api.NewQuotaHandler(quotaSvc)
	app.Get("/quota", authMiddleware, qh.GetAll)
	app.Get("/quota/:provider", authMiddleware, qh.Get)
	app.Post("/quota/:provider/refresh", authMiddleware, qh.Refresh)

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
	database.OnSessionChange = ws.NotifySessionsChanged
	app.Use("/events", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/events", authMiddleware, fiberws.New(ws.NewEventsHandler()))
	app.Use("/logs/ws", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/logs/ws", authMiddleware, fiberws.New(ws.NewLogStreamHandler()))
	app.Get("/logs/level", authMiddleware, api.GetLogLevel)
	app.Put("/logs/level", authMiddleware, api.PutLogLevel)
	app.Get("/sessions/:id/ws", authMiddleware, fiberws.New(ws.NewHandler(database, cfg.BotToken, shellOpts, quotaSvc, notifyCfg)))

	if cfg.McpToken != "" {
		header := http.Header{"Authorization": {"Bearer " + cfg.McpToken}}
		reg := mymcp.NewRegistry(func(sessionID string) string {
			return fmt.Sprintf("ws://127.0.0.1:%d/sessions/%s/ws", cfg.Server.Port, sessionID)
		}, header)
		mcpHandler := mymcp.NewHTTPHandler(database, quotaSvc, reg, cfg.McpMaxHops)
		app.Post("/mcp", authMiddleware, adaptor.HTTPHandler(mcpHandler))
		// agent session 失效時的自動交接要靠 MCP 讀舊會話，所以跟著 mcp_token 一起啟用。
		ws.SetHandoffStarter(reg.SendMessage)
		// Claude 的 --permission-prompt-tool 專用端點（只有一個工具）。沿用 mcp_token 的 Bearer 認證，不另開 token。
		ws.SetClaudePermMCP(fmt.Sprintf("http://127.0.0.1:%d/mcp/perm", cfg.Server.Port), cfg.McpToken)
		app.Post("/mcp/perm", authMiddleware, adaptor.HTTPHandler(mymcp.NewPermHTTPHandler(ws.AskClaudePermission)))
	} else {
		slog.Info("[mcp] mcp_token 未設定，/mcp 停用")
	}

	if cfg.NoAuth {
		slog.Info("⚠️  no_auth: true，已跳過 Telegram 驗證（僅限開發環境）")
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info(fmt.Sprintf("Claude Code Mini App v%s listening on %s", version.Version, addr))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("無法監聽 %s: %w", addr, err)
	}

	s := &Server{
		app:  app,
		url:  fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port),
		done: make(chan error, 1),
	}
	go func() {
		serveErr := app.Listener(ln)
		_ = database.Close()
		s.done <- serveErr
	}()
	return s, nil
}

// modelSyncMu 讓啟動時的背景同步與使用者手動重新抓取不會同時跑（兩邊都會啟動 CLI、寫同一張表）。
var modelSyncMu sync.Mutex

// syncModelOptions 抓各 agent 目前可用的模型清單並同步進 model_options 表（啟動時與手動重新抓取共用）。
// 任一 agent 取得失敗只記 log、不影響其他 agent，也不擋伺服器啟動；回傳失敗的 agent_type。
func syncModelOptions(database *db.DB) (failed []string) {
	modelSyncMu.Lock()
	defer modelSyncMu.Unlock()
	failed = []string{}
	apply := func(agentType string, live []db.ModelOption, err error) {
		if err != nil {
			slog.Info(fmt.Sprintf("[startup] 取得 %s model 清單失敗，略過: %v", agentType, err))
			failed = append(failed, agentType)
			return
		}
		if err := database.SyncModelOptions(agentType, live); err != nil {
			slog.Info(fmt.Sprintf("[startup] SyncModelOptions(%s) 失敗: %v", agentType, err))
			failed = append(failed, agentType)
		}
	}

	ctx := context.Background()

	claudeEntries, err := claude.FetchModelOptions(ctx)
	if err != nil {
		// 抓不到時不動 DB（同其他 agent），免得暫時性失敗（例如 PATH 找不到 claude）把已抓到的新模型洗掉；
		// 只有 DB 還沒有任何 Claude 選項（首次安裝）時，才用內建清單當種子。
		if existing, lerr := database.ListModelOptions(agent.TypeClaude); lerr == nil && len(existing) == 0 {
			slog.Info(fmt.Sprintf("[startup] 取得 claude model 清單失敗，改用內建清單當種子: %v", err))
			claudeEntries, err = claude.ModelOptions(), nil
		}
	}
	claudeOpts := make([]db.ModelOption, 0, len(claudeEntries))
	for _, e := range claudeEntries {
		claudeOpts = append(claudeOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeClaude, claudeOpts, err)

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

	kiroEntries, err := kiro.FetchModelOptions(ctx)
	kiroOpts := make([]db.ModelOption, 0, len(kiroEntries))
	for _, e := range kiroEntries {
		kiroOpts = append(kiroOpts, db.ModelOption{ModelID: e.ModelID, Label: e.Label})
	}
	apply(agent.TypeKiro, kiroOpts, err)
	apply(agent.TypeKiroACP, kiroOpts, err)
	return failed
}

func webSessionToken(c *fiber.Ctx) string {
	if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if t := c.Query("token"); t != "" {
		return t
	}
	return c.Cookies("session_token")
}
