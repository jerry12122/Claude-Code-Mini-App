package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestCreateSessionEffort(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "persist"
		if fail {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			database := testDB(t)
			if fail {
				_, err := database.Exec(`CREATE TRIGGER review_effort_failure BEFORE UPDATE OF effort ON sessions BEGIN SELECT RAISE(ABORT, 'forced effort failure'); END;`)
				if err != nil {
					t.Fatal(err)
				}
			}
			app := fiber.New()
			app.Post("/sessions", NewSessionHandler(database).Create)
			req := httptest.NewRequest("POST", "/sessions", strings.NewReader(`{"name":"接手","agent_type":"kiroacp","effort":"high","permission_mode":"default"}`))
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			rows, err := database.ListSessions()
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				if res.StatusCode != 500 || len(rows) != 0 {
					t.Fatalf("儲存失敗不應留下半成品: HTTP=%d sessions=%d", res.StatusCode, len(rows))
				}
				return
			}
			if res.StatusCode != 201 || len(rows) != 1 || rows[0].Effort != "high" || rows[0].AgentSessionID != "" {
				t.Fatalf("首則訊息前應已儲存 effort、原生 SID 為空: HTTP=%d sessions=%+v", res.StatusCode, rows)
			}
			var created db.Session
			if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			if created.Effort != "high" {
				t.Fatalf("建立回應 effort=%q", created.Effort)
			}
		})
	}
}

func TestSessionListReturnsUnreadCount(t *testing.T) {
	database := testDB(t)
	sess, err := database.CreateSession("unread_test", "", "", "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}

	// 模擬稍微過去的 last_read_at 並寫入 2 則訊息
	if _, err := database.Exec(`UPDATE sessions SET last_read_at = '2020-01-01 00:00:00' WHERE id = ?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.AddMessageWithID(sess.ID, "claude", "msg 1", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.AddMessageWithID(sess.ID, "claude", "msg 2", ""); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/sessions", NewSessionHandler(database).List)

	req := httptest.NewRequest("GET", "/sessions", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("expected HTTP 200, got %d", res.StatusCode)
	}

	var list []struct {
		ID          string `json:"id"`
		UnreadCount int    `json:"unread_count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}
	if list[0].UnreadCount != 2 {
		t.Fatalf("expected unread_count=2, got %d", list[0].UnreadCount)
	}
}

func TestCreateSessionTelegramDisguise(t *testing.T) {
	database := testDB(t)
	app := fiber.New()
	handler := NewSessionHandler(database)
	app.Post("/sessions", handler.Create)
	app.Get("/sessions", handler.List)

	// 使用 work_dir: "tg://@friend" 且 agent_type: "antigravity"
	payload := `{"name":"","work_dir":"tg://@friend","agent_type":"antigravity","permission_mode":"default"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 201 {
		t.Fatalf("expected HTTP 201, got %d", res.StatusCode)
	}

	var created db.Session
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	if created.AgentType != "telegram" {
		t.Fatalf("expected agent_type=telegram, got %s", created.AgentType)
	}
	if created.DisguiseAgent != "antigravity" {
		t.Fatalf("expected disguise_agent=antigravity, got %s", created.DisguiseAgent)
	}
	if created.Description != "tg_target:@friend" {
		t.Fatalf("expected description=tg_target:@friend, got %s", created.Description)
	}
	if created.WorkDir != "" {
		t.Fatalf("expected work_dir='', got %s", created.WorkDir)
	}
	if !strings.HasPrefix(created.Name, "Antigravity · ") {
		t.Fatalf("expected name to start with 'Antigravity · ', got %s", created.Name)
	}

	// 測試 GET /sessions 列表也有回傳 disguise_agent
	reqList := httptest.NewRequest("GET", "/sessions", nil)
	resList, err := app.Test(reqList)
	if err != nil {
		t.Fatal(err)
	}
	defer resList.Body.Close()

	var list []*db.Session
	if err := json.NewDecoder(resList.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}
	if list[0].DisguiseAgent != "antigravity" {
		t.Fatalf("expected list session disguise_agent=antigravity, got %s", list[0].DisguiseAgent)
	}
	if list[0].AgentType != "telegram" {
		t.Fatalf("expected list session agent_type=telegram, got %s", list[0].AgentType)
	}
}


