package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/auth"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/static"
)

const (
	defaultShareTTL = time.Hour
	minShareTTL     = time.Minute
	maxShareTTL     = 7 * 24 * time.Hour

	// joinRatePerMinute：每個來源 IP 每分鐘最多嘗試加入幾次（PIN 鎖定之外的第二道防線）。
	joinRatePerMinute = 20
)

// ShareHooks 是 API 層與 WebSocket 層（連線踢除、在線名單）之間的接點，避免 api 反向依賴 ws。
type ShareHooks struct {
	// Kick 關閉該分享所有訪客連線（結束分享時呼叫）；可為 nil。
	Kick func(shareID int64)
	// Online 回傳該分享目前在線的訪客暱稱；可為 nil。
	Online func(shareID int64) []string
}

// ShareHandler 處理分享相關 API。
type ShareHandler struct {
	db     *db.DB
	hooks  ShareHooks
	assets fs.FS // 前端公開資源（與首頁、JS、favicon 同一來源）
}

// NewShareHandler 的 assets 須含 index.html；分享頁由它讀取。
func NewShareHandler(database *db.DB, hooks ShareHooks, assets fs.FS) *ShareHandler {
	return &ShareHandler{db: database, hooks: hooks, assets: assets}
}

// shareTTL 同時接受秒數（數字）與 Go duration 字串（例如 "1h"、"30m"）。
type shareTTL time.Duration

func (t *shareTTL) UnmarshalJSON(b []byte) error {
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		*t = shareTTL(time.Duration(n * float64(time.Second)))
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("ttl 須為秒數或 duration 字串")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*t = 0
		return nil
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		*t = shareTTL(time.Duration(n * float64(time.Second)))
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("ttl 格式錯誤：%v", err)
	}
	*t = shareTTL(d)
	return nil
}

// shareView 是回給擁有者的分享資料（含明文 PIN 與連結）。
type shareView struct {
	*db.Share
	SessionName string   `json:"session_name"`
	URL         string   `json:"url"`
	Path        string   `json:"path"`
	Status      string   `json:"status"` // active / expired / revoked
	Locked      bool     `json:"locked"`
	Online      []string `json:"online"`
}

func (h *ShareHandler) view(c *fiber.Ctx, s *db.Share, sessionNames map[string]string) shareView {
	name, ok := sessionNames[s.SessionID]
	if !ok {
		if sess, err := h.db.GetSession(s.SessionID); err == nil {
			name = sess.Name
		}
		sessionNames[s.SessionID] = name
	}
	path := "/share/" + s.Token
	v := shareView{
		Share:       s,
		SessionName: name,
		Path:        path,
		URL:         c.Protocol() + "://" + c.Hostname() + path,
		Status:      s.Status(time.Now()),
		Locked:      s.Locked(),
		Online:      []string{},
	}
	if h.hooks.Online != nil && v.Status == "active" {
		if names := h.hooks.Online(s.ID); len(names) > 0 {
			sort.Strings(names)
			v.Online = names
		}
	}
	return v
}

// Create POST /sessions/:id/shares  body {ttl, role, mode}
func (h *ShareHandler) Create(c *fiber.Ctx) error {
	var body struct {
		TTL  shareTTL `json:"ttl"`
		Role string   `json:"role"`
		Mode string   `json:"mode"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return jsonErr(c, 400, err.Error())
		}
	}
	if body.Role == "" {
		body.Role = db.ShareRoleViewer
	}
	if body.Mode == "" {
		body.Mode = db.ShareModeLive
	}
	ttl := time.Duration(body.TTL)
	if ttl == 0 {
		ttl = defaultShareTTL
	}
	if ttl < minShareTTL || ttl > maxShareTTL {
		return jsonErr(c, 400, "ttl 須介於 1 分鐘到 7 天")
	}
	if err := db.ValidateShareOptions(body.Role, body.Mode); err != nil {
		return jsonErr(c, 400, err.Error())
	}
	sessionID := c.Params("id")
	sess, err := h.db.GetSession(sessionID)
	if err != nil {
		return jsonErr(c, 404, "session 不存在")
	}
	if sess.AgentType == agent.TypeTelegram {
		return jsonErr(c, 400, "Telegram 會話不支援分享")
	}
	s, err := h.db.CreateShare(sessionID, body.Role, body.Mode, ttl)
	if err != nil {
		if errors.Is(err, db.ErrInvalidShare) {
			return jsonErr(c, 400, err.Error())
		}
		slog.Warn("[share] 建立分享失敗", "err", err)
		return jsonErr(c, 500, "建立分享失敗")
	}
	slog.Info("[share] 建立分享", "share_id", s.ID, "session", sessionID, "role", s.Role, "mode", s.Mode, "expires_at", s.ExpiresAt)
	return c.Status(201).JSON(h.view(c, s, map[string]string{}))
}

// ListBySession GET /sessions/:id/shares
func (h *ShareHandler) ListBySession(c *fiber.Ctx) error {
	list, err := h.db.ListShares(c.Params("id"))
	if err != nil {
		return jsonErr(c, 500, err.Error())
	}
	return c.JSON(h.views(c, list))
}

// ListAll GET /shares
func (h *ShareHandler) ListAll(c *fiber.Ctx) error {
	list, err := h.db.ListShares("")
	if err != nil {
		return jsonErr(c, 500, err.Error())
	}
	return c.JSON(h.views(c, list))
}

func (h *ShareHandler) views(c *fiber.Ctx, list []*db.Share) []shareView {
	names := map[string]string{}
	out := make([]shareView, 0, len(list))
	for _, s := range list {
		out = append(out, h.view(c, s, names))
	}
	return out
}

// Revoke DELETE /shares/:id
func (h *ShareHandler) Revoke(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return jsonErr(c, 400, "無效的分享 id")
	}
	if err := h.db.RevokeShare(id); err != nil {
		if errors.Is(err, db.ErrShareNotFound) {
			return jsonErr(c, 404, err.Error())
		}
		return jsonErr(c, 500, err.Error())
	}
	h.kick(id)
	slog.Info("[share] 結束分享", "share_id", id)
	return c.SendStatus(204)
}

// RevokeAll DELETE /shares：結束全部有效分享。
func (h *ShareHandler) RevokeAll(c *fiber.Ctx) error {
	ids, err := h.db.RevokeActiveShares("")
	if err != nil {
		return jsonErr(c, 500, err.Error())
	}
	for _, id := range ids {
		h.kick(id)
	}
	slog.Info("[share] 結束全部分享", "count", len(ids))
	return c.JSON(fiber.Map{"revoked": len(ids)})
}

// ClearEnded DELETE /shares/ended[/:id]：僅清除已結束的紀錄，既有 DELETE /shares 仍只做撤銷。
func (h *ShareHandler) ClearEnded(c *fiber.Ctx) error {
	var id int64
	if raw := c.Params("id"); raw != "" {
		var err error
		id, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return jsonErr(c, 400, "無效的分享 id")
		}
		share, err := h.db.GetShare(id)
		if err != nil && !errors.Is(err, db.ErrShareNotFound) {
			return jsonErr(c, 500, err.Error())
		}
		if share != nil && share.Active(time.Now()) {
			return jsonErr(c, 409, "只能清除已結束或已到期的分享")
		}
	}
	n, err := h.db.ClearEndedShares(id)
	if err != nil {
		return jsonErr(c, 500, err.Error())
	}
	return c.JSON(fiber.Map{"cleared": n})
}

func (h *ShareHandler) kick(id int64) {
	if h.hooks.Kick != nil {
		h.hooks.Kick(id)
	}
}

// KickAll 供其他 handler（例如刪除 session）在撤銷分享後踢線。
func (h *ShareHandler) KickAll(ids []int64) {
	for _, id := range ids {
		h.kick(id)
	}
}

// Page GET /share/:token：回傳 SPA（index.html），由前端 app.js 依路徑進入訪客模式。
// 頁面以相對路徑載入資源，故注入 <base>，讓 /share/<token> 下仍能正確取得 js／favicon。
func (h *ShareHandler) Page(c *fiber.Ctx) error {
	raw, err := static.IndexHTML(h.assets)
	if err != nil {
		return jsonErr(c, 500, "無法載入頁面")
	}
	base := "../"
	if strings.HasSuffix(c.Path(), "/") {
		base = "../../"
	}
	html := strings.Replace(string(raw), "<head>", `<head><base href="`+base+`" />`, 1)
	c.Set("Cache-Control", "no-store")
	c.Set("Referrer-Policy", "no-referrer") // 避免 token 經 Referer 外洩給 CDN
	c.Type("html", "utf-8")
	return c.SendString(html)
}

// guestSessionInfo 是訪客可見的聊天室資訊（刻意不含 work_dir、git 分支等伺服器端資訊）。
func (h *ShareHandler) guestSessionInfo(sessionID string) (fiber.Map, error) {
	s, err := h.db.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return fiber.Map{
		"id":              s.ID,
		"name":            s.Name,
		"agent_type":      s.AgentType,
		"permission_mode": s.PermissionMode,
		"input_mode":      s.InputMode,
		"model":           s.Model,
		"effort":          s.Effort,
	}, nil
}

func (h *ShareHandler) accessPayload(ga *db.GuestAccess) (fiber.Map, error) {
	info, err := h.guestSessionInfo(ga.Share.SessionID)
	if err != nil {
		return nil, err
	}
	out := fiber.Map{
		"session_id": ga.Share.SessionID,
		"nickname":   ga.Guest.Nickname,
		"role":       ga.Share.Role,
		"mode":       ga.Share.Mode,
		"expires_at": ga.Share.ExpiresAt,
		"created_at": ga.Share.CreatedAt,
		"session":    info,
	}
	if ga.Share.SnapshotMsgID != nil {
		out["snapshot_msg_id"] = *ga.Share.SnapshotMsgID
	}
	return out, nil
}

// Join POST /share/:token/join  body {pin, nickname}
func (h *ShareHandler) Join(c *fiber.Ctx) error {
	var body struct {
		PIN      string `json:"pin"`
		Nickname string `json:"nickname"`
	}
	if err := c.BodyParser(&body); err != nil {
		return jsonErr(c, 400, "請求格式錯誤")
	}
	ga, err := h.db.TryJoin(c.Params("token"), body.PIN, body.Nickname)
	if err != nil {
		var pe *db.PINError
		switch {
		case errors.As(err, &pe):
			slog.Warn("[share] PIN 錯誤", "ip", auth.RealIP(c), "remaining", pe.Remaining)
			status := 401
			if pe.Remaining <= 0 {
				status = 423
			}
			return c.Status(status).JSON(fiber.Map{"error": pe.Error(), "remaining": pe.Remaining})
		case errors.Is(err, db.ErrShareNotFound):
			return jsonErr(c, 404, err.Error())
		case errors.Is(err, db.ErrShareEnded):
			return jsonErr(c, 410, err.Error())
		case errors.Is(err, db.ErrShareLocked):
			return jsonErr(c, 423, err.Error())
		case errors.Is(err, db.ErrInvalidNickname):
			return jsonErr(c, 400, err.Error())
		default:
			slog.Warn("[share] join 失敗", "err", err)
			return jsonErr(c, 500, "加入失敗")
		}
	}
	payload, err := h.accessPayload(ga)
	if err != nil {
		return jsonErr(c, 404, "聊天室不存在")
	}
	payload["guest_token"] = ga.Guest.Token
	slog.Info("[share] 訪客加入", "share_id", ga.Share.ID, "nickname", ga.Guest.Nickname, "ip", auth.RealIP(c))
	return c.JSON(payload)
}

// Me GET /guest/me：訪客用 guest token 重新取得自己的分享資訊（重新整理頁面後還原狀態）。
// 須掛在 authMiddleware 後（由它驗證 token 並寫入 Locals）。
func (h *ShareHandler) Me(c *fiber.Ctx) error {
	token, _ := c.Locals("share_guest_token").(string)
	ga, err := h.db.ResolveGuestToken(token)
	if err != nil {
		return jsonErr(c, 401, "分享不存在")
	}
	payload, err := h.accessPayload(ga)
	if err != nil {
		return jsonErr(c, 404, "聊天室不存在")
	}
	return c.JSON(payload)
}
