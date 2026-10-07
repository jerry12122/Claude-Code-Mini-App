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
