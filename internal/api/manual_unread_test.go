package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestManualUnreadMarkPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marker.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	notes := 0
	database.OnSessionChange = func() { notes++ }
	readSess := seedManualSession(t, database, "read", "claude", 1)
	emptySess := seedManualSession(t, database, "empty", "claude", -1)
	unreadSess := seedManualSession(t, database, "three", "claude", 0)
	tgSess := seedManualSession(t, database, "tg", "telegram", 0)
	cat, err := database.CreateSessionCategory("A", "Review")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetSessionCategory(readSess.ID, &cat.ID); err != nil {
		t.Fatal(err)
	}
	notes = 0
	app := fiber.New()
	h := NewSessionHandler(database)
	app.Get("/sessions", h.List)
	app.Patch("/sessions/:id", h.Patch)
	app.Post("/sessions/read-all", h.ReadAll)

	code, body := patchSession(t, app, readSess.ID, `{"manual_unread":true,"manual_unread_revision":99}`)
	if code != 200 || !strings.Contains(string(body), `"manual_unread":true`) || !strings.Contains(string(body), `"manual_unread_revision":1`) {
		t.Fatalf("舊 API 沒有可標記的 manual_unread，got %d %s", code, body)
	}
	listBody := listManualRaw(t, app)
	if !strings.Contains(listBody, `"manual_unread":false`) || !strings.Contains(listBody, `"manual_unread_revision":0`) {
		t.Fatalf("owner 列表沒有 marker 欄：%s", listBody)
	}
	if notes != 1 {
		t.Fatalf("成功標記應通知 1 次，got %d", notes)
	}
	got := manualByID(t, app, readSess.ID)
	if !got.ManualUnread || got.ManualUnreadRevision != 1 || got.UnreadCount != 0 || got.LastActive != "2020-01-02 03:04:05" || got.LastReadAt != "2020-01-02 03:04:06" || got.Status != "idle" || !got.Pinned || got.CategoryID == nil || *got.CategoryID != cat.ID || got.WorkDir != "A" {
		t.Fatalf("標記後狀態 %+v", got)
	}
	assertManualSideEffects(t, database, readSess.ID, 1, "digest-1", "queued-body")

	code, body = patchSession(t, app, readSess.ID, `{"manual_unread":true}`)
	if code != 200 || !strings.Contains(string(body), `"manual_unread_revision":2`) || notes != 2 {
		t.Fatalf("再次標記 %d notes=%d %s", code, notes, body)
	}
	if rev := manualByID(t, app, readSess.ID).ManualUnreadRevision; rev != 2 {
		t.Fatalf("client 不該把 revision 停在灌入值，got %d", rev)
	}

	code, body = patchSession(t, app, emptySess.ID, `{"manual_unread":true}`)
	empty := decodeManual(t, body)
	if code != 200 || !empty.ManualUnread || empty.ManualUnreadRevision != 1 || empty.UnreadCount != 0 || empty.LastActive != "2020-01-02 03:04:05" {
		t.Fatalf("空會話 %d %+v %s", code, empty, body)
	}

	var msgIDs []int64
	rows, err := database.Query(`SELECT id FROM messages WHERE session_id=? ORDER BY id`, unreadSess.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		msgIDs = append(msgIDs, id)
	}
	rows.Close()
	if len(msgIDs) != 3 {
		t.Fatalf("三則未讀訊息 got %d", len(msgIDs))
	}
	beforeNotes := notes
	code, body = patchSession(t, app, unreadSess.ID, `{"manual_unread":true}`)
	three := decodeManual(t, body)
	if code != 200 || !three.ManualUnread || three.UnreadCount != 3 || notes != beforeNotes+1 {
		t.Fatalf("三則未讀 %d notes=%d %+v", code, notes, three)
	}
	assertManualSideEffects(t, database, unreadSess.ID, 0, "digest-1", "queued-body")

	code, body = patchSession(t, app, tgSess.ID, `{"manual_unread":true}`)
	tg := decodeManual(t, body)
	if code != 200 || !tg.ManualUnread || tg.UnreadCount != 0 || tg.AgentType != "telegram" {
		t.Fatalf("Telegram %d %+v %s", code, tg, body)
	}

	before := notes
	code, body = patchSession(t, app, readSess.ID, `{"mark_read":false}`)
	if code != 200 || notes != before {
		t.Fatalf("省略 marker %d notes=%d %s", code, notes, body)
	}
	kept := manualByID(t, app, readSess.ID)
	if !kept.ManualUnread || kept.ManualUnreadRevision != 2 || kept.LastActive != "2020-01-02 03:04:05" {
		t.Fatalf("省略不該改 marker %+v", kept)
	}

	before = notes
	code, body = patchSession(t, app, readSess.ID, `{"manual_unread":false}`)
	if code != 400 || notes != before {
		t.Fatalf("無 revision 的 false 不該清除 %d notes=%d %s", code, notes, body)
	}
	if still := manualByID(t, app, readSess.ID); !still.ManualUnread || still.ManualUnreadRevision != 2 {
		t.Fatalf("false 改到了 marker %+v", still)
	}

	for _, raw := range []string{
		`{"manual_unread":null}`,
		`{"manual_unread":"true"}`,
		`{"manual_unread":1}`,
		`{"manual_unread":[]}`,
		`{"manual_unread":{}}`,
	} {
		before = notes
		code, body = patchSession(t, app, readSess.ID, raw)
		if code != 400 || notes != before {
			t.Fatalf("壞型別 %s → %d notes=%d %s", raw, code, notes, body)
		}
	}
	if still := manualByID(t, app, readSess.ID); !still.ManualUnread || still.ManualUnreadRevision != 2 {
		t.Fatalf("壞型別改到 marker %+v", still)
	}

	before = notes
	code, _ = patchSession(t, app, "missing", `{"manual_unread":true}`)
	if code != 404 || notes != before {
		t.Fatalf("缺會話 %d notes=%d", code, notes)
	}

	before = notes
	code, body = patchSession(t, app, readSess.ID, `{"name":"  ","manual_unread":true}`)
	if code != 400 || notes != before || manualByID(t, app, readSess.ID).ManualUnreadRevision != 2 || manualByID(t, app, readSess.ID).Name != "read" {
		t.Fatalf("空白名稱先改了 marker %d notes=%d %s", code, notes, body)
	}
	code, body = patchSession(t, app, readSess.ID, `{"category_id":1,"manual_unread":true}`)
	if code != 400 || manualByID(t, app, readSess.ID).ManualUnreadRevision != 2 {
		t.Fatalf("壞 category 先改了 marker %d %s", code, body)
	}
	code, body = patchSession(t, app, readSess.ID, `{"category_id":"missing","manual_unread":true}`)
	if code != 404 || manualByID(t, app, readSess.ID).ManualUnreadRevision != 2 || manualByID(t, app, readSess.ID).CategoryID == nil || *manualByID(t, app, readSess.ID).CategoryID != cat.ID {
		t.Fatalf("不存在的分類先改了 marker %d %s", code, body)
	}
	longArg := strings.Repeat("x", 4097)
	code, body = patchSession(t, app, readSess.ID, `{"cli_extra_args":["`+longArg+`"],"manual_unread":true}`)
	if code != 400 || manualByID(t, app, readSess.ID).ManualUnreadRevision != 2 {
		t.Fatalf("壞 cli 先改了 marker %d %s", code, body)
	}
	code, body = patchSession(t, app, unreadSess.ID, `{"mark_read":true,"read_message_ids":[],"manual_unread":true}`)
	if code != 400 || manualByID(t, app, unreadSess.ID).ManualUnreadRevision != 1 {
		t.Fatalf("壞 read_message_ids 先改了 marker %d %s", code, body)
	}
	var stillUnread int
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=0`, unreadSess.ID).Scan(&stillUnread); err != nil || stillUnread != 3 {
		t.Fatalf("壞 ACK 改到已讀數 %d %v", stillUnread, err)
	}

	before = notes
	code, body = patchSession(t, app, unreadSess.ID, `{"mark_read":true,"read_message_ids":[`+strconv.FormatInt(msgIDs[0], 10)+`]}`)
	if code != 200 {
		t.Fatalf("舊 ACK %d %s", code, body)
	}
	acked := manualByID(t, app, unreadSess.ID)
	if !acked.ManualUnread || acked.ManualUnreadRevision != 1 || acked.UnreadCount != 2 {
		t.Fatalf("舊 ACK 清掉了 marker 或計數不對 %+v", acked)
	}
	var oneRead int
	if err := database.QueryRow(`SELECT is_read FROM messages WHERE id=?`, msgIDs[0]).Scan(&oneRead); err != nil || oneRead != 1 {
		t.Fatalf("舊 ACK 應把該訊息標已讀 read=%d err=%v", oneRead, err)
	}

	beforeActive := manualByID(t, app, emptySess.ID).LastActive
	before = notes
	code, body = patchSession(t, app, emptySess.ID, `{"name":" renamed ","manual_unread":true}`)
	renamed := decodeManual(t, body)
	if code != 200 || renamed.Name != "renamed" || !renamed.ManualUnread || renamed.ManualUnreadRevision != 2 || renamed.LastActive == beforeActive {
		t.Fatalf("合法改名應更新 last_active 並標記 %+v before=%s %s", renamed, beforeActive, body)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions/read-all", nil)
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatalf("ReadAll %d", res.StatusCode)
	}
	afterAll := manualByID(t, app, readSess.ID)
	afterThree := manualByID(t, app, unreadSess.ID)
	if !afterAll.ManualUnread || afterAll.ManualUnreadRevision != 2 || !afterThree.ManualUnread || afterThree.ManualUnreadRevision != 1 || afterThree.UnreadCount != 0 {
		t.Fatalf("ReadAll 清掉了 marker %+v %+v", afterAll, afterThree)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var manual, revision, unread int
	var lastActive string
	if err := reopened.QueryRow(`SELECT manual_unread, manual_unread_revision, last_active,
		(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.is_read = 0 AND m.status != 'pending' AND m.role != 'user' AND s.agent_type != 'telegram')
		FROM sessions s WHERE id=?`, readSess.ID).Scan(&manual, &revision, &lastActive, &unread); err != nil || manual != 1 || revision != 2 || unread != 0 || lastActive != "2020-01-02 03:04:05" {
		t.Fatalf("重開 manual=%d rev=%d unread=%d active=%s err=%v", manual, revision, unread, lastActive, err)
	}
}

func TestManualUnreadClosedDatabaseIs500(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/closed.db")
	if err != nil {
		t.Fatal(err)
	}
	sess := seedManualSession(t, database, "s", "claude", 1)
	notes := 0
	database.OnSessionChange = func() { notes++ }
	app := fiber.New()
	app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	code, body := patchSession(t, app, sess.ID, `{"manual_unread":true}`)
	if code != 500 || notes != 0 || !strings.Contains(string(body), "手動標記資料庫錯誤") {
		t.Fatalf("關閉的 DB 應 500 且不通知，got %d notes=%d %s", code, notes, body)
	}
}

func TestManualUnreadParallelIncrements(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/parallel.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var mu sync.Mutex
	notes := 0
	database.OnSessionChange = func() {
		mu.Lock()
		notes++
		mu.Unlock()
	}
	sess := seedManualSession(t, database, "p", "claude", 1)
	app := fiber.New()
	app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = patchSession(t, app, sess.ID, `{"manual_unread":true}`)
		}(i)
	}
	wg.Wait()
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("舊 API 拒絕並行標記 codes=%v", codes)
	}
	var manual, revision int
	if err := database.QueryRow(`SELECT manual_unread, manual_unread_revision FROM sessions WHERE id=?`, sess.ID).Scan(&manual, &revision); err != nil || manual != 1 || revision != 2 || notes != 2 {
		t.Fatalf("並行標記 manual=%d rev=%d notes=%d err=%v", manual, revision, notes, err)
	}
}

func seedManualSession(t *testing.T, database *db.DB, name, agentType string, readFlag int) *db.Session {
	t.Helper()
	sess, err := database.CreateSession(name, "", "A", "default", agentType, nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE sessions SET last_active='2020-01-02 03:04:05', last_read_at='2020-01-02 03:04:06', status='idle', pinned=1 WHERE id=?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if readFlag >= 0 {
		n := 1
		if readFlag == 0 {
			n = 3
		}
		for i := 0; i < n; i++ {
			if _, err := database.Exec(`INSERT INTO messages (session_id, role, content, status, is_read) VALUES (?, 'claude', 'body', 'done', ?)`, sess.ID, readFlag); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := database.Exec(`INSERT INTO queued_messages (session_id, content) VALUES (?, 'queued-body')`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO input_receipts (session_id, actor, request_id, digest) VALUES (?, 'owner', 'req-1', 'digest-1')`, sess.ID); err != nil {
		t.Fatal(err)
	}
	return sess
}

func assertManualSideEffects(t *testing.T, database *db.DB, id string, wantRead int, wantDigest, wantQueue string) {
	t.Helper()
	var readCount int
	var digest, queue, lastActive, status, workDir string
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=? AND is_read=?`, id, wantRead).Scan(&readCount); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id=?`, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if readCount != total {
		t.Fatalf("is_read=%d 的訊息 %d，總數 %d", wantRead, readCount, total)
	}
	if err := database.QueryRow(`SELECT digest FROM input_receipts WHERE session_id=?`, id).Scan(&digest); err != nil || digest != wantDigest {
		t.Fatalf("receipt %q %v", digest, err)
	}
	if err := database.QueryRow(`SELECT content FROM queued_messages WHERE session_id=?`, id).Scan(&queue); err != nil || queue != wantQueue {
		t.Fatalf("queue %q %v", queue, err)
	}
	if err := database.QueryRow(`SELECT last_active, status, work_dir FROM sessions WHERE id=?`, id).Scan(&lastActive, &status, &workDir); err != nil || lastActive != "2020-01-02 03:04:05" || status != "idle" || workDir != "A" {
		t.Fatalf("session 副作用 active=%s status=%s dir=%s err=%v", lastActive, status, workDir, err)
	}
}

func listManualRaw(t *testing.T, app *fiber.App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("list %d %s", res.StatusCode, b)
	}
	return string(b)
}

type manualView struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	AgentType            string  `json:"agent_type"`
	WorkDir              string  `json:"work_dir"`
	LastActive           string  `json:"last_active"`
	LastReadAt           string  `json:"last_read_at"`
	Status               string  `json:"status"`
	Pinned               bool    `json:"pinned"`
	CategoryID           *string `json:"category_id"`
	UnreadCount          int     `json:"unread_count"`
	ManualUnread         bool    `json:"manual_unread"`
	ManualUnreadRevision int     `json:"manual_unread_revision"`
}

func decodeManual(t *testing.T, body []byte) manualView {
	t.Helper()
	var s manualView
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return s
}

func manualByID(t *testing.T, app *fiber.App, id string) manualView {
	t.Helper()
	var list []manualView
	if err := json.Unmarshal([]byte(listManualRaw(t, app)), &list); err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("列表沒有 %s", id)
	return manualView{}
}
