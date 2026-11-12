package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestCreateSessionInstructionValidation(t *testing.T) {
	type instructionCase struct {
		name                  string
		value                 any
		runner, mode, workdir string
		flags                 []string
		status                int
	}
	cases := []instructionCase{
		{"claude", "  回答使用繁體中文\n路徑 C:\\work  ", "claude", "agent", "", nil, 201},
		{"codex", "role", "codex", "agent", "", nil, 201},
		{"agy", "role", "antigravity", "agent", "", nil, 201},
		{"default", "role", "", "", "", nil, 201},
		{"ascii limit", strings.Repeat("a", 16384), "claude", "agent", "", nil, 201},
		{"ascii overflow", strings.Repeat("a", 16385), "claude", "agent", "", nil, 400},
		{"unicode limit", strings.Repeat("中", 5461) + "a", "claude", "agent", "", nil, 201},
		{"unicode overflow", strings.Repeat("中", 5461) + "ab", "claude", "agent", "", nil, 400},
		{"null", nil, "claude", "agent", "", nil, 400},
		{"number", 12, "claude", "agent", "", nil, 400},
		{"bool", true, "claude", "agent", "", nil, 400},
		{"object", map[string]string{"role": "secret"}, "claude", "agent", "", nil, 400},
		{"array", []string{"secret"}, "claude", "agent", "", nil, 400},
		{"NUL", "secret\x00value", "claude", "agent", "", nil, 400},
		{"cursor", "role", "cursor", "agent", "", nil, 400},
		{"kiro", "role", "kiro", "agent", "", nil, 400},
		{"kiroacp", "role", "kiroacp", "agent", "", nil, 400},
		{"telegram", "role", "telegram", "agent", "", nil, 400},
		{"telegram disguise", "role", "claude", "agent", "tg://someone", nil, 400},
		{"shell", "role", "claude", "shell", "", nil, 400},
		{"empty unsupported", " \u3000\n", "cursor", "agent", "", nil, 201},
	}
	for _, flag := range []string{"--system-prompt", "--system-prompt-file", "--append-system-prompt", "--append-system-prompt-file"} {
		cases = append(cases, instructionCase{flag, "role", "claude", "agent", "", []string{flag, "secret"}, 400})
		cases = append(cases, instructionCase{flag + " equals", "role", "claude", "agent", "", []string{flag + "=secret"}, 400})
		cases = append(cases, instructionCase{flag + " empty compatible", " \n", "claude", "agent", "", []string{flag + "=secret"}, 201})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.Open(t.TempDir() + "/test.db")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			h := NewSessionHandler(database)
			app := fiber.New()
			app.Post("/sessions", h.Create)
			payload, _ := json.Marshal(map[string]any{"name": "new", "agent_type": tc.runner, "input_mode": tc.mode, "work_dir": tc.workdir, "system_instruction": tc.value, "cli_extra_args": tc.flags})
			req := httptest.NewRequest("POST", "/sessions", strings.NewReader(string(payload)))
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.status {
				t.Fatalf("status %d want %d: %s", res.StatusCode, tc.status, body)
			}
			list, err := database.ListSessions()
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == 201 {
				var s db.Session
				if err := json.Unmarshal(body, &s); err != nil {
					t.Fatal(err)
				}
				want := strings.TrimSpace(tc.value.(string))
				if len(list) != 1 || s.SystemInstruction != want || list[0].SystemInstruction != want {
					t.Fatal("instruction not persisted/returned")
				}
			} else {
				if len(list) != 0 {
					t.Fatal("rejected create left a session")
				}
				var failure map[string]string
				if json.Unmarshal(body, &failure) != nil || failure["error"] == "" {
					t.Fatalf("missing error field: %s", body)
				}
			}
		})
	}
}

func TestSessionInstructionValidationDoesNotEcho(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/private.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	app := fiber.New()
	app.Post("/sessions", NewSessionHandler(database).Create)
	for _, payload := range []string{
		`{"system_instruction":{"PRIVATE_INSTRUCTION":"secret"}}`,
		`{"system_instruction":"PRIVATE_INSTRUCTION\u0000"}`,
		`{"system_instruction":"PRIVATE_INSTRUCTION","agent_type":"cursor"}`,
		`{"system_instruction":"PRIVATE_INSTRUCTION","cli_extra_args":["--append-system-prompt=PRIVATE_INSTRUCTION"]}`,
		`{"system_instruction":"PRIVATE_INSTRUCTION","agent_type":{}}`,
	} {
		req := httptest.NewRequest("POST", "/sessions", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 400 || strings.Contains(string(body), "PRIVATE_INSTRUCTION") {
			t.Fatalf("unsafe validation response %d %s", res.StatusCode, body)
		}
	}
}

func TestPatchSessionInstructionImmutable(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/patch.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, err := database.CreateSessionWithInstruction("old", "", "", "", "claude", []string{"--verbose"}, "agent", "", "original")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Patch("/sessions/:id", NewSessionHandler(database).Patch)
	for _, value := range []string{`"new"`, `null`, `""`, `123`} {
		req := httptest.NewRequest("PATCH", "/sessions/"+s.ID, strings.NewReader(`{"name":"changed","cli_extra_args":[],"system_instruction":`+value+`}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("status = %d", res.StatusCode)
		}
		got, err := database.GetSession(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "old" || got.SystemInstruction != "original" || len(got.CliExtraArgs) != 1 {
			t.Fatalf("partial patch %+v", got)
		}
	}
}

func TestCreateSessionInstructionStorageFailure(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/fail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`CREATE TRIGGER fail_instruction BEFORE INSERT ON sessions WHEN NEW.system_instruction != '' BEGIN SELECT RAISE(ABORT, 'storage failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Post("/sessions", NewSessionHandler(database).Create)
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(`{"system_instruction":"private role"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	list, err := database.ListSessions()
	if err != nil || len(list) != 0 {
		t.Fatalf("partial row %+v %v", list, err)
	}
}
