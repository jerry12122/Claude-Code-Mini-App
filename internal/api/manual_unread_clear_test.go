package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestManualUnreadClearAdvancesRevision(t *testing.T) {
	database, app := manualClearApp(t)
	sess := seedManualSession(t, database, "read", "claude", 1)
	for i := 0; i < 4; i++ {
		code, body := patchSession(t, app, sess.ID, `{"manual_unread":true}`)
		if code != 200 {
			t.Fatalf("第 %d 次標記 %d %s", i+1, code, body)
		}
	}
	code, body := patchSession(t, app, sess.ID, `{"manual_unread":false,"manual_unread_revision":4}`)
	if code != 200 || !strings.Contains(string(body), `"manual_unread":false`) || !strings.Contains(string(body), `"manual_unread_revision":5`) {
		t.Fatalf("帶觀察 revision 的清除應成功並把版本推進到 5，got %d %s", code, body)
	}
	got := manualByID(t, app, sess.ID)
	if got.ManualUnread || got.ManualUnreadRevision != 5 || got.UnreadCount != 0 || got.LastActive != "2020-01-02 03:04:05" || got.Status != "idle" {
		t.Fatalf("清除後 %+v", got)
	}
	var isRead int
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE session_id=?`, sess.ID).Scan(&isRead); err != nil || isRead != 1 {
		t.Fatalf("只清標記不該改已讀訊息 read=%d err=%v", isRead, err)
	}

	code, body = patchSession(t, app, sess.ID, `{"manual_unread":true,"manual_unread_revision":1}`)
	if code != 200 || !strings.Contains(string(body), `"manual_unread_revision":6`) {
		t.Fatalf("true 仍應由伺服器加 1，got %d %s", code, body)
	}

	code, body = patchSession(t, app, sess.ID, `{"manual_unread":false}`)
	if code != 400 || manualByID(t, app, sess.ID).ManualUnreadRevision != 6 || manualByID(t, app, sess.ID).ManualUnread != true {
		t.Fatalf("沒有 revision 的 false 不該清除 %d %s", code, body)
	}
	for _, raw := range []string{
		`{"manual_unread":false,"manual_unread_revision":null}`,
		`{"manual_unread":false,"manual_unread_revision":"4"}`,
		`{"manual_unread":false,"manual_unread_revision":-1}`,
		`{"manual_unread":false,"manual_unread_revision":4.5}`,
		`{"manual_unread":false,"manual_unread_revision":[]}`,
	} {
		code, body = patchSession(t, app, sess.ID, raw)
		if code != 400 {
			t.Fatalf("壞 revision %s → %d %s", raw, code, body)
		}
	}
	if still := manualByID(t, app, sess.ID); still.ManualUnread != true || still.ManualUnreadRevision != 6 {
		t.Fatalf("壞 revision 改到了標記 %+v", still)
	}

	code, _ = patchSession(t, app, "missing", `{"manual_unread":false,"manual_unread_revision":6}`)
	if code != 404 {
		t.Fatalf("缺會話 %d", code)
	}
}

func TestManualUnreadClearRejectsStaleRevision(t *testing.T) {
	database, app := manualClearApp(t)
	sess := seedManualSession(t, database, "stale", "claude", 1)
	var doneID, pendingID int64
	if err := database.QueryRow(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'old-done', 'done', 0) RETURNING id`, sess.ID).Scan(&doneID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'still-pending', 'pending', 0) RETURNING id`, sess.ID).Scan(&pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=5, last_active='2020-01-02 03:04:05' WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	code, body := patchSession(t, app, sess.ID, `{"manual_unread":false,"manual_unread_revision":4,"read_message_ids":[`+strconv.FormatInt(doneID, 10)+`,`+strconv.FormatInt(pendingID, 10)+`]}`)
	if code != 409 || !strings.Contains(string(body), `"manual_unread":true`) || !strings.Contains(string(body), `"manual_unread_revision":5`) {
		t.Fatalf("revision 4 不該清掉 5，got %d %s", code, body)
	}
	var doneRead, pendingRead int
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, doneID).Scan(&doneRead); err != nil || doneRead != 1 {
		t.Fatalf("過期清除仍應 ACK 已完成的觀察訊息 read=%d err=%v", doneRead, err)
	}
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, pendingID).Scan(&pendingRead); err != nil || pendingRead != 0 {
		t.Fatalf("pending 不該被 ACK read=%d err=%v", pendingRead, err)
	}
	if got := manualByID(t, app, sess.ID); got.ManualUnread != true || got.ManualUnreadRevision != 5 || got.LastActive != "2020-01-02 03:04:05" {
		t.Fatalf("過期清除改到了標記或時間 %+v", got)
	}

	hold, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hold.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=5 WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		code int
		body string
	}
	ch := make(chan result, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID, strings.NewReader(`{"manual_unread":false,"manual_unread_revision":4}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			ch <- result{code: -1, body: err.Error()}
			return
		}
		defer res.Body.Close()
		buf, _ := io.ReadAll(res.Body)
		ch <- result{code: res.StatusCode, body: string(buf)}
	}()
	select {
	case early := <-ch:
		hold.Rollback()
		t.Fatalf("清除在寫入鎖放開前就返回，沒有在同一交易比對 revision：%d %s", early.code, early.body)
	case <-time.After(300 * time.Millisecond):
	}
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	late := <-ch
	if late.code != 409 || !strings.Contains(late.body, `"manual_unread":true`) || !strings.Contains(late.body, `"manual_unread_revision":5`) {
		t.Fatalf("鎖放開後應看到 revision 5 並拒絕，got %d %s", late.code, late.body)
	}
}

func TestReadAllEmptyEntriesDoesNotAckAll(t *testing.T) {
	database, app := manualClearApp(t)
	sess := seedManualSession(t, database, "empty-batch", "claude", 0)
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=4 WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	code, body := postRaw(t, app, "/sessions/read-all", `{"sessions":[]}`)
	if code != 200 || strings.TrimSpace(body) == "" {
		t.Fatalf("新版空清單應回可觀察結果，got %d %s", code, body)
	}
	var unread int
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=0`, sess.ID).Scan(&unread); err != nil || unread != 3 {
		t.Fatalf("空清單不該把 3 則未讀讀掉，剩 %d err=%v", unread, err)
	}
	if got := manualByID(t, app, sess.ID); !got.ManualUnread || got.ManualUnreadRevision != 4 {
		t.Fatalf("空清單不該改標記 %+v", got)
	}

	code, body = postRaw(t, app, "/sessions/read-all", `{"sessions":[{"id":"`+sess.ID+`","manual_unread_revision":4,"read_message_ids":[]}]}`)
	if code != 200 {
		t.Fatalf("觀察到零則訊息的清除 %d %s", code, body)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=0`, sess.ID).Scan(&unread); err != nil || unread != 3 {
		t.Fatalf("空 ID 不該讀完整個會話，剩 %d err=%v", unread, err)
	}
	if got := manualByID(t, app, sess.ID); got.ManualUnread || got.ManualUnreadRevision != 5 {
		t.Fatalf("空 ID 仍應清掉對上的標記 %+v", got)
	}
	for _, raw := range []string{
		`{"sessions":null}`,
		`{}`,
		`{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":null,"read_message_ids":[]}]}`,
		`{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":-1,"read_message_ids":[]}]}`,
		`{"sessions":[{"id":"` + sess.ID + `","read_message_ids":[]}]}`,
		`{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":"5","read_message_ids":[]}]}`,
		`{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":5,"read_message_ids":[]},{"id":"` + sess.ID + `","manual_unread_revision":5,"read_message_ids":[]}]}`,
	} {
		code, body = postRaw(t, app, "/sessions/read-all", raw)
		if code != 400 {
			t.Fatalf("壞 ReadAll %s → %d %s", raw, code, body)
		}
	}
	code, body = postRaw(t, app, "/sessions/read-all", `{"sessions":[{"id":"`+sess.ID+`","manual_unread_revision":5,"read_message_ids":[]},{"id":"missing","manual_unread_revision":0,"read_message_ids":[]}]}`)
	if code != 404 {
		t.Fatalf("缺會話應整批拒絕 %d %s", code, body)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=0`, sess.ID).Scan(&unread); err != nil || unread != 3 {
		t.Fatalf("拒絕後不該讀訊息，剩 %d err=%v", unread, err)
	}
	if got := manualByID(t, app, sess.ID); got.ManualUnread || got.ManualUnreadRevision != 5 {
		t.Fatalf("拒絕後標記被改 %+v", got)
	}
}

func TestReadAllKeepsLaterCompletedSmallerID(t *testing.T) {
	database, app := manualClearApp(t)
	sess := seedManualSession(t, database, "snap", "claude", -1)
	var pendingID, doneID int64
	if err := database.QueryRow(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'pending-small', 'pending', 0) RETURNING id`, sess.ID).Scan(&pendingID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'done-large', 'done', 0) RETURNING id`, sess.ID).Scan(&doneID); err != nil {
		t.Fatal(err)
	}
	if pendingID >= doneID {
		t.Fatalf("fixture 應讓 pending ID 更小，pending=%d done=%d", pendingID, doneID)
	}
	snap := snapshotIDs(t, listManualRaw(t, app), sess.ID)
	if len(snap) != 1 || snap[0] != doneID {
		t.Fatalf("列表快照應只有已完成的 %d，got %v，pending=%d", doneID, snap, pendingID)
	}
	if _, err := database.Exec(`UPDATE messages SET status='done' WHERE id=?`, pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=5, last_active='2020-01-02 03:04:05' WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	var otherID int64
	var otherRest int
	other := seedManualSession(t, database, "other", "claude", 0)
	if err := database.QueryRow(`SELECT id FROM messages WHERE session_id=? ORDER BY id LIMIT 1`, other.ID).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=4 WHERE id=?`, other.ID); err != nil {
		t.Fatal(err)
	}
	raw := `{"sessions":[` +
		`{"id":"` + sess.ID + `","manual_unread_revision":4,"read_message_ids":[` + strconv.FormatInt(doneID, 10) + `]},` +
		`{"id":"` + other.ID + `","manual_unread_revision":4,"read_message_ids":[` + strconv.FormatInt(otherID, 10) + `]}` +
		`]}`
	code, body := postRaw(t, app, "/sessions/read-all", raw)
	if code != 409 || !strings.Contains(body, `"manual_unread":true`) || !strings.Contains(body, `"manual_unread_revision":5`) {
		t.Fatalf("舊 revision 4 不該清掉已變成 5 的標記，got %d %s", code, body)
	}
	var smallRead, largeRead, otherRead int
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, pendingID).Scan(&smallRead); err != nil || smallRead != 0 {
		t.Fatalf("較小且不在快照的新完成訊息應保持未讀 read=%d err=%v", smallRead, err)
	}
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, doneID).Scan(&largeRead); err != nil || largeRead != 1 {
		t.Fatalf("快照內的舊完成訊息應 ACK read=%d err=%v", largeRead, err)
	}
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, otherID).Scan(&otherRead); err != nil || otherRead != 1 {
		t.Fatalf("revision 對上的另一會話應 ACK 觀察到的那則 read=%d err=%v", otherRead, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND id!=? AND is_read=0`, other.ID, otherID).Scan(&otherRest); err != nil || otherRest != 2 {
		t.Fatalf("沒放進快照的 2 則應保持未讀，got %d err=%v", otherRest, err)
	}
	stale := manualByID(t, app, sess.ID)
	cleared := manualByID(t, app, other.ID)
	if !stale.ManualUnread || stale.ManualUnreadRevision != 5 || stale.LastActive != "2020-01-02 03:04:05" || cleared.ManualUnread || cleared.ManualUnreadRevision != 5 {
		t.Fatalf("部分過期 stale=%+v cleared=%+v", stale, cleared)
	}
}

func TestManualUnreadClearClosedDatabaseIs500(t *testing.T) {
	database, app := manualClearApp(t)
	sess := seedManualSession(t, database, "closed", "claude", 1)
	if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=2 WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	notes := 0
	database.OnSessionChange = func() { notes++ }
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	code, body := patchSession(t, app, sess.ID, `{"manual_unread":false,"manual_unread_revision":2}`)
	if code != 500 || notes != 0 {
		t.Fatalf("關閉的 DB 應 500 且不通知，got %d notes=%d %s", code, notes, body)
	}
}

func manualClearApp(t *testing.T) (*db.DB, *fiber.App) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/clear.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	app := fiber.New()
	h := NewSessionHandler(database)
	app.Get("/sessions", h.List)
	app.Patch("/sessions/:id", h.Patch)
	app.Post("/sessions/read-all", h.ReadAll)
	return database, app
}

func snapshotIDs(t *testing.T, raw, id string) []int64 {
	t.Helper()
	var rows []struct {
		ID             string  `json:"id"`
		ReadMessageIDs []int64 `json:"read_message_ids"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row.ReadMessageIDs
		}
	}
	t.Fatalf("列表沒有 %s", id)
	return nil
}

func postRaw(t *testing.T, app *fiber.App, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(buf)
}

func TestReadAllRejectsTrailingJSON(t *testing.T) {
	for _, suffix := range []string{"}", "]", "}garbage"} {
		t.Run(suffix, func(t *testing.T) {
			database, app := manualClearApp(t)
			sess := seedManualSession(t, database, "trailing", "claude", 0)
			if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=4 WHERE id=?`, sess.ID); err != nil {
				t.Fatal(err)
			}
			notes := 0
			database.OnSessionChange = func() { notes++ }
			valid := `{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":4,"read_message_ids":[]}]}`
			code, body := postRaw(t, app, "/sessions/read-all", valid+suffix)
			got, err := database.GetSession(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if code != 400 || !got.ManualUnread || got.ManualUnreadRevision != 4 || got.UnreadCount != 3 || notes != 0 {
				t.Fatalf("尾端 %q 應 400 且不寫入，got %d manual=%v rev=%d unread=%d notes=%d %s", suffix, code, got.ManualUnread, got.ManualUnreadRevision, got.UnreadCount, notes, body)
			}
		})
	}
	t.Run("trailing-space", func(t *testing.T) {
		database, app := manualClearApp(t)
		sess := seedManualSession(t, database, "space", "claude", 0)
		if _, err := database.Exec(`UPDATE sessions SET manual_unread=1, manual_unread_revision=4 WHERE id=?`, sess.ID); err != nil {
			t.Fatal(err)
		}
		notes := 0
		database.OnSessionChange = func() { notes++ }
		valid := `{"sessions":[{"id":"` + sess.ID + `","manual_unread_revision":4,"read_message_ids":[]}]}`
		code, body := postRaw(t, app, "/sessions/read-all", valid+" \t\n")
		got, err := database.GetSession(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 || got.ManualUnread || got.ManualUnreadRevision != 5 || got.UnreadCount != 3 || notes != 1 {
			t.Fatalf("尾端空白仍應清除，got %d manual=%v rev=%d unread=%d notes=%d %s", code, got.ManualUnread, got.ManualUnreadRevision, got.UnreadCount, notes, body)
		}
	})
}
