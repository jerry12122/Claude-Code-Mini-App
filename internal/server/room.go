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
	"net/http"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	mymcp "github.com/jerry12122/Claude-Code-Mini-App/internal/mcp"
)

const roomWakeHeader = "X-Miniapp-Room-Wake"

type roomRuntime struct {
	db        *db.DB
	address   string
	wakeToken string
	toolKey   string
}

func newRoomRuntime(database *db.DB, port int) roomRuntime {
	return roomRuntime{db: database, address: fmt.Sprintf("127.0.0.1:%d", port), wakeToken: rand.Text(), toolKey: rand.Text()}
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

func (r roomRuntime) wake(ctx context.Context, sessionID string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws://"+r.address+"/sessions/"+sessionID+"/ws", http.Header{roomWakeHeader: {r.wakeToken}})
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetWriteDeadline(deadline)
	_ = conn.SetReadDeadline(deadline)
	if err := conn.WriteJSON(map[string]string{"type": "room_drain"}); err != nil {
		return err
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var event struct{ Type string }
		if json.Unmarshal(payload, &event) == nil && event.Type == "room_drained" {
			return nil
		}
	}
}
