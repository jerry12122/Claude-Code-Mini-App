package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

const roomWakeHeader = "X-Miniapp-Room-Wake"

type roomRuntime struct {
	db           *db.DB
	address      string
	wakeToken    string
	toolKey      string
	quotaRefresh func(context.Context, string) (quota.Snapshot, error)
}

func newRoomRuntime(database *db.DB, port int) roomRuntime {
	return roomRuntime{db: database, address: fmt.Sprintf("127.0.0.1:%d", port), wakeToken: rand.Text(), toolKey: rand.Text()}
}

func (r roomRuntime) registerRoutes(app *fiber.App, auth fiber.Handler, quotaSvc *quota.Service) {
	h := api.NewRoomsHandler(r.db, quotaSvc)
	app.Get("/rooms", auth, h.List)
	app.Post("/rooms", auth, h.Create)
	app.Get("/rooms/:id", auth, h.Get)
	app.Put("/rooms/:id", auth, h.Update)
	app.Delete("/rooms/:id", auth, h.Delete)
	app.Get("/rooms/:id/timeline", auth, h.Timeline)
	app.Get("/rooms/:id/board", auth, h.Board)
	app.Put("/rooms/:id/board", auth, h.SaveMemo)
	app.Post("/rooms/:id/messages", auth, h.SendToPM)
	app.Post("/mcp/room", r.mcpAuth, adaptor.HTTPHandler(mymcp.NewRoomHTTPHandler(r.db, quotaSvc)))
}

func (r roomRuntime) listen(ctx context.Context, app *fiber.App, ln net.Listener) error {
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.watch(watchCtx)
	}()
	quotaDone := make(chan struct{})
	go func() {
		defer close(quotaDone)
		r.watchQuota(watchCtx)
	}()
	defer func() {
		stop()
		<-done
		<-quotaDone
	}()
	return app.Listener(ln)
}

func (r roomRuntime) pmToken(roomID, sessionID string) string {
	mac := hmac.New(sha256.New, []byte(r.toolKey))
	mac.Write([]byte(roomID + ":" + sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (r roomRuntime) bridge(sessionID string) (string, string) {
	roomID, err := r.db.RoomForPM(sessionID)
	if err != nil {
		return "", ""
	}
	return "http://" + r.address + "/mcp/room", r.pmToken(roomID, sessionID)
}

func (r roomRuntime) mcpAuth(c *fiber.Ctx) error {
	sessionID := c.Get(mymcp.PermSessionHeader)
	if sessionID == "" || !c.Context().RemoteIP().IsLoopback() || c.Get("Authorization") == "" {
		return fiber.ErrUnauthorized
	}
	roomID, err := r.db.RoomForPM(sessionID)
	if err != nil || subtle.ConstantTimeCompare([]byte(c.Get("Authorization")), []byte("Bearer "+r.pmToken(roomID, sessionID))) != 1 {
		return fiber.ErrForbidden
	}
	if err := readJSONBody(c, 256<<10); err != nil {
		return err
	}
	return c.Next()
}

func (r roomRuntime) wsAuth(normal fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Get(roomWakeHeader)
		if token == "" {
			return normal(c)
		}
		if !c.Context().RemoteIP().IsLoopback() || subtle.ConstantTimeCompare([]byte(token), []byte(r.wakeToken)) != 1 {
			return fiber.ErrUnauthorized
		}
		c.Locals("room_wake", true)
		return c.Next()
	}
}

// The clock observes state without starting an idle PM. Only a queued user message or report can wake it.
func (r roomRuntime) watch(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.db.DeliverRoomReports(); err != nil {
				slog.Error("[room] monitor", "err", err)
				continue
			}
			ids, err := r.db.RoomSessionsToWake()
			if err != nil {
				slog.Error("[room] pending input", "err", err)
				continue
			}
			for _, id := range ids {
				if ctx.Err() != nil {
					return
				}
				if err := r.wake(ctx, id); err != nil {
					slog.Warn("[room] wake will retry", "session", id, "err", err)
				}
			}
		}
	}
}

// wakeSession 以內部喚醒身分連上該 session 的 WS，送出 msg，讀到 doneType 事件後回傳該事件原文。
func (r roomRuntime) wakeSession(ctx context.Context, sessionID string, timeout time.Duration, msg any, doneType string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws://"+r.address+"/sessions/"+sessionID+"/ws", http.Header{roomWakeHeader: {r.wakeToken}})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetWriteDeadline(deadline)
	_ = conn.SetReadDeadline(deadline)
	if err := conn.WriteJSON(msg); err != nil {
		return nil, err
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &event) == nil && event.Type == doneType {
			return payload, nil
		}
	}
}

func (r roomRuntime) wake(ctx context.Context, sessionID string) error {
	_, err := r.wakeSession(ctx, sessionID, 2*time.Second, map[string]string{"type": "room_drain"}, "room_drained")
	return err
}
