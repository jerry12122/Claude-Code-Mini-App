package main

import (
	"fmt"
	"log"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/config"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// 解析允許的內網 CIDR
	allowedNets, err := auth.ParseCIDRs(cfg.Web.AllowedCIDRs)
	if err != nil {
		log.Fatal("CIDR 設定錯誤:", err)
	}

	// 解析 session TTL
	sessionTTL, err := time.ParseDuration(cfg.Web.SessionTTL)
	if err != nil {
		log.Fatalf("web.session_ttl 格式錯誤 %q: %v", cfg.Web.SessionTTL, err)
	}
	sessions := auth.NewStore(sessionTTL)

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		log.Fatal("DB 初始化失敗:", err)
	}
	defer database.Close()

	// 將 config.yaml 中的白名單寫入 DB
	for _, id := range cfg.WhitelistTgIDs {
		if err := database.AddUser(id, ""); err != nil {
			log.Printf("新增白名單使用者 %d 失敗: %v", id, err)
		}
	}
	if len(cfg.WhitelistTgIDs) > 0 {
		log.Printf("白名單已載入，共 %d 位使用者", len(cfg.WhitelistTgIDs))
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
			log.Printf("[auth] 拒絕登入請求，非內網 IP: %s", ip)
			return c.Status(403).JSON(fiber.Map{"error": "僅允許內網存取"})
		}

		var body struct {
			Password   string `json:"password"`
			TgUserID   int64  `json:"tg_user_id"` // 選填：白名單內 Telegram ID，用於任務完成／授權通知
		}
		if err := c.BodyParser(&body); err != nil || body.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "請提供 password 欄位"})
		}
		if body.Password != cfg.Web.Password {
			log.Printf("[auth] Web 密碼錯誤（來源: %s）", ip)
			return c.Status(401).JSON(fiber.Map{"error": "密碼錯誤"})
		}

		var bindTgID int64
		if body.TgUserID != 0 {
			allowed, err := database.IsUserAllowed(body.TgUserID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
			}
			if !allowed {
				log.Printf("[auth] Web 登入拒絕：tg_user_id=%d 不在白名單", body.TgUserID)
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
					log.Printf("[auth] web.default_notify_tg_id=%d 不在白名單，略過", cfg.Web.DefaultNotifyTgID)
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

		c.Cookie(&fiber.Cookie{
			Name:     "session_token",
			Value:    token,
			MaxAge:   int(sessionTTL.Seconds()),
			HTTPOnly: true,
			SameSite: "Strict",
		})
		log.Printf("[auth] Web 登入成功（來源: %s）", ip)
		return c.JSON(fiber.Map{"ok": true})
	})

	// POST /auth/logout
	// 清除 session cookie 與伺服器端 token。
	app.Post("/auth/logout", func(c *fiber.Ctx) error {
		token := c.Cookies("session_token")
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
				log.Printf("[auth] TG 驗證失敗: %v", err)
				return c.Status(401).JSON(fiber.Map{"error": "Telegram 驗證失敗"})
			}
			allowed, err := database.IsUserAllowed(user.ID)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "DB 錯誤"})
			}
			if !allowed {
				log.Printf("[auth] 拒絕使用者 tg_id=%d (@%s)", user.ID, user.Username)
				return c.Status(403).JSON(fiber.Map{"error": "無存取權限"})
			}
			log.Printf("[auth] TG 驗證通過: tg_id=%d (@%s)", user.ID, user.Username)
			c.Locals("tg_id", user.ID)
			return c.Next()
		}

		// 方式二：Web session cookie（限內網 IP）
		ip := auth.RealIP(c)
		if !auth.IsAllowed(ip, allowedNets) {
			log.Printf("[auth] 拒絕非內網 IP: %s", ip)
			return c.Status(403).JSON(fiber.Map{"error": "僅允許內網存取"})
		}

		token := c.Cookies("session_token")
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
	app.Patch("/sessions/:id", authMiddleware, sh.Rename)
	app.Delete("/sessions/:id", authMiddleware, sh.Delete)
	app.Get("/sessions/:id/messages", authMiddleware, sh.Messages)

	// WebSocket
	app.Use("/sessions/:id/ws", func(c *fiber.Ctx) error {
		if fiberws.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/sessions/:id/ws", authMiddleware, fiberws.New(ws.NewHandler(database, cfg.BotToken)))

	if cfg.NoAuth {
		log.Println("⚠️  no_auth: true，已跳過 Telegram 驗證（僅限開發環境）")
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Fatal(app.Listen(addr))
}
