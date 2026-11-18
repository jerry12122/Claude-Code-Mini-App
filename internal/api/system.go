package api

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/sysmon"
)

// statsSource 讓 handler 測試能注入固定結果；正式實作是 *sysmon.Sampler。
type statsSource interface {
	Snapshot() (sysmon.Stats, error)
}

type SystemHandler struct {
	src statsSource
}

func NewSystemHandler(src statsSource) *SystemHandler { return &SystemHandler{src: src} }

// Stats GET /system/stats — 整機 CPU／記憶體單次快照。
// 訪客不在 guestRouteAllowed 的放行清單內，由 authMiddleware 以 403 擋下，這裡不重複判斷。
func (h *SystemHandler) Stats(c *fiber.Ctx) error {
	st, err := h.src.Snapshot()
	if err != nil {
		if errors.Is(err, sysmon.ErrUnsupported) {
			return jsonErr(c, 501, err.Error())
		}
		slog.Warn("[sysmon] 取樣失敗", "err", err)
		return jsonErr(c, 500, "取得系統資源失敗")
	}
	return c.JSON(st)
}
