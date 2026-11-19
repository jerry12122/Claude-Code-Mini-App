package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

const (
	roomNameMaxRunes = 100
	roomMaxMembers   = 50
	// roomCursorTimeLayout 與 messages.created_at 的儲存格式一致（UTC、固定寬度）。
	roomCursorTimeLayout = "2006-01-02 15:04:05"
)

// QuotaReader 是工作室對額度快取的唯一需求：只讀，不提供刷新，從型別上就保證 handler 不會觸發抓取。
type QuotaReader interface {
	Get(provider string) quota.Snapshot
}

// RoomsHandler 提供工作室、合併時間軸與人員送往專用 PM 的入口。
// 訪客不在 guestRouteAllowed 的放行清單內，由 authMiddleware 以 403 擋下，這裡不重複判斷。
type RoomsHandler struct {
	db    *db.DB
	quota QuotaReader
}

func NewRoomsHandler(database *db.DB, q QuotaReader) *RoomsHandler {
	return &RoomsHandler{db: database, quota: q}
}

type roomMemberBody struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
}

// roomBody 的欄位用指標區分「沒給」與「給了空值」：PUT 沒給的欄位不動。
type roomBody struct {
	Name         *string           `json:"name"`
	Members      *[]roomMemberBody `json:"members"`
	PreviousPMID *string           `json:"previous_pm_id"`
}

type roomQuotaJSON struct {
	Provider    string         `json:"provider"`
	DisplayText string         `json:"display_text"`
	Plan        string         `json:"plan,omitempty"`
	Windows     []quota.Window `json:"windows,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	// Stale 不用 omitempty：前端要能明確分辨「新鮮」與「過期」。從未取得過額度也視為 stale。
	Stale    bool   `json:"stale"`
	Fetching bool   `json:"fetching,omitempty"`
	Error    string `json:"error,omitempty"`
}

type roomMemberJSON struct {
	SessionID   string         `json:"session_id"`
	Role        string         `json:"role"`
	Position    int            `json:"position"`
	SessionName string         `json:"session_name"`
	AgentType   string         `json:"agent_type"`
	Status      string         `json:"status"`
	Quota       *roomQuotaJSON `json:"quota,omitempty"`
	QueueLength int            `json:"queue_length"`
	QueuePaused bool           `json:"queue_paused"`
	LastError   string         `json:"last_error,omitempty"`
	LastErrorAt string         `json:"last_error_at,omitempty"`
	Attention   string         `json:"attention,omitempty"`
}

type roomJSON struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	CreatedAt  string           `json:"created_at"`
	Members    []roomMemberJSON `json:"members"`
	PMID       string           `json:"pm_id,omitempty"`
	ObservedAt string           `json:"observed_at,omitempty"`
	Handoff    *roomHandoffJSON `json:"handoff,omitempty"`
}

type roomHandoffJSON struct {
	Name    string `json:"pm_name"`
	Inputs  int    `json:"inputs"`
	Reports int    `json:"reports"`
	Queue   int    `json:"queue_length"`
	Busy    bool   `json:"busy"`
	Paused  bool   `json:"paused"`
}

func (h *RoomsHandler) SendToPM(c *fiber.Ctx) error {
	var body struct {
		Text          string   `json:"text"`
		RequestID     string   `json:"request_id"`
		PMID          string   `json:"pm_id"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if err := c.BodyParser(&body); err != nil {
		return jsonErr(c, 400, "無效的工作室訊息")
	}
	body.Text = strings.TrimSpace(body.Text)
	if (body.Text == "" && len(body.AttachmentIDs) == 0) || len(body.AttachmentIDs) > 20 || len(body.Text) > 64000 || body.RequestID == "" || len(body.RequestID) > 128 {
		return jsonErr(c, 400, "請輸入訊息或加入附件（文字最多 64 KB，每則最多 20 個附件）")
	}
	if err := h.db.AcceptRoomInput(c.Params("id"), body.PMID, body.RequestID, body.Text, body.AttachmentIDs); err != nil {
		if errors.Is(err, db.ErrInvalidAttachment) {
			return jsonErr(c, 400, err.Error())
		}
		if errors.Is(err, db.ErrRoomPM) {
			return jsonErr(c, 409, "工作室的 PM 已變更或未設定，請確認成員後再送出")
		}
		if errors.Is(err, db.ErrInputConflict) {
			return jsonErr(c, 409, err.Error())
		}
		return roomsInternal(c, "send", err)
	}
	return c.SendStatus(fiber.StatusAccepted)
}

func (h *RoomsHandler) SaveMemo(c *fiber.Ctx) error {
	var body struct {
		Text     *string `json:"text"`
		Previous *string `json:"previous"`
	}
	if err := c.BodyParser(&body); err != nil || body.Text == nil || body.Previous == nil || len(*body.Text) > 32_000 {
		return jsonErr(c, 400, "請提供備忘，最多 32 KB")
	}
	if err := h.db.EditRoomMemo(c.Params("id"), *body.Previous, strings.TrimSpace(*body.Text)); err != nil {
		return h.writeErr(c, "memo", err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *RoomsHandler) Board(c *fiber.Ctx) error {
	board, err := h.db.RoomBoard(c.Params("id"))
	if err != nil {
		return h.writeErr(c, "board", err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(board)
}

type roomTimelineItemJSON struct {
	ID              int64           `json:"id"`
	SessionID       string          `json:"session_id"`
	SessionName     string          `json:"session_name"`
	MemberRole      string          `json:"member_role"`
	AgentType       string          `json:"agent_type"`
	Role            string          `json:"role"`
	Author          string          `json:"author"`
	Sender          string          `json:"sender,omitempty"`
	Recipient       string          `json:"recipient,omitempty"`
	Mine            bool            `json:"mine,omitempty"`
	SourceKind      string          `json:"source_kind,omitempty"`
	SourceSessionID string          `json:"source_session_id,omitempty"`
	SourceRoom      string          `json:"source_room,omitempty"`
	Body            string          `json:"body,omitempty"`
	Transfer        string          `json:"transfer,omitempty"`
	Attachments     []db.Attachment `json:"attachments,omitempty"`
	Text            string          `json:"text"`
	Status          string          `json:"status"`
	CreatedAt       string          `json:"created_at"`
}

// List GET /rooms
func (h *RoomsHandler) List(c *fiber.Ctx) error {
	rooms, err := h.db.ListRooms()
	if err != nil {
		return roomsInternal(c, "list", err)
	}
	out := make([]fiber.Map, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, fiber.Map{"id": r.ID, "name": r.Name, "created_at": r.CreatedAt, "member_count": r.MemberCount})
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(out)
}

// Create POST /rooms
func (h *RoomsHandler) Create(c *fiber.Ctx) error {
	var body roomBody
	if err := c.BodyParser(&body); err != nil {
		return jsonErr(c, 400, "無效的工作室資料")
	}
	if body.Name == nil {
		return jsonErr(c, 400, "工作室名稱不可為空")
	}
	name, msg := validateRoomName(*body.Name)
	if msg != "" {
		return jsonErr(c, 400, msg)
	}
	members := []db.RoomMember{}
	if body.Members != nil {
		if members, msg = validateRoomMembers(*body.Members); msg != "" {
			return jsonErr(c, 400, msg)
		}
	}
	room, err := h.db.CreateRoom(name, members)
	if err != nil {
		return h.writeErr(c, "create", err)
	}
	return c.Status(201).JSON(h.detail(room))
}

// Get GET /rooms/:id
func (h *RoomsHandler) Get(c *fiber.Ctx) error {
	room, err := h.db.GetRoom(c.Params("id"))
	if err != nil {
		return h.writeErr(c, "get", err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(h.detail(room))
}

// Update PUT /rooms/:id：name 與 members 都是選填，給了才改；members 是整批取代（增減成員與角色都走這裡）。
func (h *RoomsHandler) Update(c *fiber.Ctx) error {
	var body roomBody
	if err := c.BodyParser(&body); err != nil {
		return jsonErr(c, 400, "無效的工作室資料")
	}
	if body.Name == nil && body.Members == nil {
		return jsonErr(c, 400, "請提供要更新的 name 或 members")
	}
	var namePtr *string
	var membersPtr *[]db.RoomMember
	if body.Name != nil {
		name, msg := validateRoomName(*body.Name)
		if msg != "" {
			return jsonErr(c, 400, msg)
		}
		namePtr = &name
	}
	if body.Members != nil {
		members, msg := validateRoomMembers(*body.Members)
		if msg != "" {
			return jsonErr(c, 400, msg)
		}
		membersPtr = &members
	}
	id := c.Params("id")
	var expected []string
	if body.PreviousPMID != nil {
		expected = append(expected, *body.PreviousPMID)
	}
	if err := h.db.UpdateRoom(id, namePtr, membersPtr, expected...); err != nil {
		return h.writeErr(c, "update", err)
	}
	room, err := h.db.GetRoom(id)
	if err != nil {
		return h.writeErr(c, "update", err)
	}
	return c.JSON(h.detail(room))
}

// Delete DELETE /rooms/:id：只刪工作室，不刪任何 session。
func (h *RoomsHandler) Delete(c *fiber.Ctx) error {
	if err := h.db.DeleteRoom(c.Params("id")); err != nil {
		return h.writeErr(c, "delete", err)
	}
	return c.SendStatus(204)
}

// Timeline GET /rooms/:id/timeline?before=<游標>&limit=<預設 50、上限 200>
// 回傳新到舊；has_more 為 true 時，把 next_cursor 帶進下一次的 before 取更早的訊息。
func (h *RoomsHandler) Timeline(c *fiber.Ctx) error {
	limit := db.RoomTimelineDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return jsonErr(c, 400, "limit 必須是正整數")
		}
		limit = min(n, db.RoomTimelineMaxLimit)
	}
	var before *db.RoomCursor
	if raw := c.Query("before"); raw != "" {
		cur, ok := parseRoomCursor(raw)
		if !ok {
			return jsonErr(c, 400, "before 游標無效")
		}
		before = cur
	}
	page, err := h.db.RoomTimeline(c.Params("id"), before, limit)
	if err != nil {
		return h.writeErr(c, "timeline", err)
	}
	msgs := make([]roomTimelineItemJSON, 0, len(page.Items))
	for _, it := range page.Items {
		msgs = append(msgs, roomTimelineItemJSON{
			ID: it.MessageID, SessionID: it.SessionID, SessionName: it.SessionName, MemberRole: it.MemberRole,
			AgentType: it.AgentType, Role: it.Role, Author: db.RoomMessageAuthor(it.Author), Text: it.Content, Status: it.Status, CreatedAt: it.CreatedAt,
			Sender: it.Sender, Recipient: it.Recipient, Mine: it.Mine, Attachments: it.Attachments,
			SourceKind: it.SourceKind, SourceSessionID: it.SourceSessionID, SourceRoom: it.SourceRoom,
			Body: it.Body, Transfer: it.Transfer,
		})
	}
	next := ""
	if page.Next != nil {
		next = formatRoomCursor(page.Next)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"messages": msgs, "has_more": page.HasMore, "next_cursor": next})
}

func validateRoomName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "工作室名稱不可為空"
	}
	if utf8.RuneCountInString(name) > roomNameMaxRunes {
		return "", fmt.Sprintf("工作室名稱最多 %d 個字", roomNameMaxRunes)
	}
	return name, ""
}

// validateRoomMembers 驗證成員清單。role 只認小寫 pm／worker；同一個 session 不得出現兩次
// （拒絕而不是靜默去重：兩筆的角色可能互相矛盾，無法替使用者猜哪個才對）。
func validateRoomMembers(raw []roomMemberBody) ([]db.RoomMember, string) {
	if len(raw) > roomMaxMembers {
		return nil, fmt.Sprintf("成員最多 %d 位（上限）", roomMaxMembers)
	}
	members := make([]db.RoomMember, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, m := range raw {
		sid := strings.TrimSpace(m.SessionID)
		if sid == "" {
			return nil, "成員的 session_id 不可為空"
		}
		if m.Role != db.RoomRolePM && m.Role != db.RoomRoleWorker {
			return nil, "成員的 role 只能是 pm 或 worker"
		}
		if seen[sid] {
			return nil, "成員重複：" + sid
		}
		seen[sid] = true
		members = append(members, db.RoomMember{SessionID: sid, Role: m.Role})
	}
	return members, ""
}

func (h *RoomsHandler) detail(r *db.Room) roomJSON {
	out := roomJSON{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, Members: make([]roomMemberJSON, 0, len(r.Members)),
		ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	out.PMID, _ = h.db.RoomPM(r.ID)
	for _, m := range r.Members {
		out.Members = append(out.Members, roomMemberJSON{
			SessionID: m.SessionID, Role: m.Role, Position: m.Position,
			SessionName: m.SessionName, AgentType: m.AgentType, Status: m.Status,
			Quota: h.memberQuota(m.AgentType), QueueLength: m.QueueLength, QueuePaused: m.QueuePaused,
			LastError: m.LastError, LastErrorAt: m.LastErrorAt,
			Attention: db.RoomAttention(m.Status, m.ShellPending, m.PendingDenials),
		})
	}
	if out.PMID != "" {
		if preview, err := h.db.RoomHandoffPreview(r.ID, out.PMID); err == nil {
			out.Handoff = &roomHandoffJSON{
				Name: preview.Name, Inputs: preview.Inputs, Reports: preview.Reports,
				Queue: preview.Queue, Busy: preview.Busy, Paused: preview.Paused,
			}
		}
	}
	return out
}

// memberQuota 只讀快取。沒有快取（從未取得）時 UpdatedAt 為零值，一律標 stale。
func (h *RoomsHandler) memberQuota(provider string) *roomQuotaJSON {
	if h.quota == nil {
		return nil
	}
	snap := h.quota.Get(provider)
	q := &roomQuotaJSON{
		Provider: snap.Provider, DisplayText: snap.DisplayText, Plan: snap.Plan, Windows: snap.Windows,
		Stale: snap.Stale || snap.UpdatedAt.IsZero(), Fetching: snap.Fetching, Error: snap.Error,
	}
	if q.Provider == "" {
		q.Provider = provider
	}
	if !snap.UpdatedAt.IsZero() {
		q.UpdatedAt = snap.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return q
}

func (h *RoomsHandler) writeErr(c *fiber.Ctx, op string, err error) error {
	switch {
	case errors.Is(err, db.ErrRoomNotFound):
		return jsonErr(c, 404, err.Error())
	case errors.Is(err, db.ErrRoomSessionMissing):
		return jsonErr(c, 400, err.Error())
	case errors.Is(err, db.ErrRoomPM), errors.Is(err, db.ErrRoomPMChanged), errors.Is(err, db.ErrRoomHandoffBusy), errors.Is(err, db.ErrRoomMemoChanged), errors.Is(err, db.ErrInputConflict):
		return jsonErr(c, 409, err.Error())
	}
	return roomsInternal(c, op, err)
}

// roomsInternal 只回固定訊息，原始錯誤進日誌。
func roomsInternal(c *fiber.Ctx, op string, err error) error {
	slog.Warn("[rooms] 未預期的錯誤", "op", op, "err", err)
	return jsonErr(c, 500, "工作室操作失敗")
}

// 游標是不透明字串：base64url("<created_at>|<id>")，前端原樣帶回即可。
func formatRoomCursor(cur *db.RoomCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cur.CreatedAt + "|" + strconv.FormatInt(cur.ID, 10)))
}

func parseRoomCursor(raw string) (*db.RoomCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, false
	}
	at, idStr, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id < 1 {
		return nil, false
	}
	if _, err := time.Parse(roomCursorTimeLayout, at); err != nil {
		return nil, false
	}
	return &db.RoomCursor{CreatedAt: at, ID: id}, true
}
