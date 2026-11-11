package server

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

const apiBodyLimit = 16 << 20
const smallBodyLimit = 64 << 10

// Stream after 64 KiB; authentication runs before consuming large bodies.
func requestLimits(h *fasthttp.RequestHeader) fasthttp.RequestConfig {
	return fasthttp.RequestConfig{MaxRequestBodySize: smallBodyLimit}
}

func readJSONBody(c *fiber.Ctx, limit int) error {
	if c.Method() == "GET" || c.Method() == "HEAD" || (c.Method() == "POST" && strings.HasSuffix(c.Path(), "/uploads")) {
		return nil
	}
	if c.Context().Request.Header.ContentLength() > limit {
		c.Context().SetConnectionClose()
		return fiber.NewError(413, "請求內容過大，請分段或改用附件")
	}
	if stream := c.Context().RequestBodyStream(); stream != nil {
		body, err := io.ReadAll(io.LimitReader(stream, int64(limit)+1))
		if err != nil {
			c.Context().SetConnectionClose()
			return fiber.NewError(400, "讀取請求失敗")
		}
		if len(body) > limit {
			c.Context().SetConnectionClose()
			return fiber.NewError(413, "請求內容過大，請分段或改用附件")
		}
		c.Request().SetBodyRaw(body)
	} else if len(c.Request().Body()) > limit {
		return fiber.NewError(413, "請求內容過大，請分段或改用附件")
	}
	return nil
}

func smallJSONBody(c *fiber.Ctx) error {
	if err := readJSONBody(c, smallBodyLimit); err != nil {
		return err
	}
	return c.Next()
}

func rejectCompressedBody(c *fiber.Ctx) error {
	if encoding := c.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(strings.TrimSpace(encoding), "identity") {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "Compressed request bodies are not supported")
	}
	return c.Next()
}

// One guard covers chat, events and logs. Wait for the watcher before Fiber
// recycles the connection; timers must never close a later user's connection.
func guardedSocket(handler func(*fiberws.Conn)) func(*fiberws.Conn) {
	return func(c *fiberws.Conn) {
		limit, _ := c.Locals("max_message_bytes").(int)
		if limit == 0 {
			limit = apiBodyLimit
		}
		c.SetReadLimit(int64(limit))
		revoked, _ := c.Locals("auth_revoked").(<-chan struct{})
		expiry, _ := c.Locals("auth_expires").(time.Time)
		refresh, _ := c.Locals("auth_refresh").(func() bool)
		interval, _ := c.Locals("auth_refresh_interval").(time.Duration)
		var lastPong atomic.Int64
		lastPong.Store(time.Now().UnixNano())
		if refresh != nil {
			c.SetPongHandler(func(string) error {
				if !refresh() {
					return fmt.Errorf("login expired")
				}
				lastPong.Store(time.Now().UnixNano())
				return nil
			})
		}
		var refreshTick <-chan time.Time
		if refresh != nil && interval > 0 {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			refreshTick = ticker.C
		}
		var timeout <-chan time.Time
		if !expiry.IsZero() && refresh == nil {
			timer := time.NewTimer(time.Until(expiry))
			defer timer.Stop()
			timeout = timer.C
		}
		stopped, exited := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(exited)
		watch:
			for {
				select {
				case <-revoked:
					break watch
				case <-timeout:
					break watch
				case <-refreshTick:
					if time.Since(time.Unix(0, lastPong.Load())) > 2*interval {
						break watch
					}
					if err := c.WriteControl(fiberws.PingMessage, nil, time.Now().Add(min(interval, time.Second))); err != nil {
						break watch
					}
				case <-stopped:
					return
				}
			}
			_ = c.WriteControl(fiberws.CloseMessage, fiberws.FormatCloseMessage(fiberws.ClosePolicyViolation, "authentication ended"), time.Now().Add(time.Second))
			// fasthttp's hijack Close is a no-op until the handler returns.
			// Interrupt both reader and writer so the handler can actually return.
			_ = c.NetConn().SetDeadline(time.Now())
		}()
		defer func() { close(stopped); <-exited }()
		if !expiry.IsZero() && !expiry.After(time.Now()) {
			return
		}
		select {
		case <-revoked:
			return
		default:
		}
		handler(c)
	}
}
