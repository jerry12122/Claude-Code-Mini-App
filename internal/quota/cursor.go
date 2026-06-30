package quota

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
	_ "modernc.org/sqlite"
)

type CursorFetcher struct{}

func (f *CursorFetcher) Provider() string { return agent.TypeCursor }

func (f *CursorFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	token, err := readCursorAccessToken()
	if err != nil {
		return Snapshot{Provider: agent.TypeCursor}, err
	}
	body, err := fetchCursorPeriodUsage(ctx, token)
	if err != nil {
		return Snapshot{Provider: agent.TypeCursor}, err
	}
	info, err := usage.FromCursorPeriodUsageJSON(body)
	if err != nil {
		return Snapshot{Provider: agent.TypeCursor}, err
	}
	return Snapshot{
		Provider:    agent.TypeCursor,
		DisplayText: FormatDisplay(agent.TypeCursor, info),
		UpdatedAt:   time.Now(),
	}, nil
}

func cursorStateDBPath() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "Cursor", "User", "globalStorage", "state.vscdb")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
}

func readCursorAccessToken() (string, error) {
	dbPath := cursorStateDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("cursor state.vscdb not found")
	}
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()
	var token string
	err = db.QueryRow(`SELECT value FROM ItemTable WHERE key='cursorAuth/accessToken'`).Scan(&token)
	if err != nil || strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("cursorAuth/accessToken not found")
	}
	return token, nil
}

func fetchCursorPeriodUsage(ctx context.Context, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage",
		bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &errBody)
		if errBody.Message != "" {
			return nil, fmt.Errorf("cursor usage http %d: %s", resp.StatusCode, errBody.Message)
		}
		return nil, fmt.Errorf("cursor usage http %d", resp.StatusCode)
	}
	return body, nil
}
