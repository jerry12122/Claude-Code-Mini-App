package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/sysmon"
)

type fakeStats struct {
	stats sysmon.Stats
	err   error
}

func (f fakeStats) Snapshot() (sysmon.Stats, error) { return f.stats, f.err }

func statsApp(src statsSource) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/system/stats", NewSystemHandler(src).Stats)
	return app
}

func TestSystemStats_OK(t *testing.T) {
	cpu := 12.5
	at := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
	res, err := statsApp(fakeStats{stats: sysmon.Stats{CPUPercent: &cpu, MemUsedBytes: 3, MemTotalBytes: 8, SampledAt: at}}).
		Test(httptest.NewRequest("GET", "/system/stats", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var got struct {
		CPU     *float64 `json:"cpu_percent"`
		Used    uint64   `json:"mem_used_bytes"`
		Total   uint64   `json:"mem_total_bytes"`
		Sampled string   `json:"sampled_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.CPU == nil || *got.CPU != 12.5 || got.Used != 3 || got.Total != 8 || got.Sampled != "2026-11-18T01:02:03Z" {
		t.Fatalf("got %+v", got)
	}
}

func TestSystemStats_Unsupported501(t *testing.T) {
	res, err := statsApp(fakeStats{err: sysmon.ErrUnsupported}).Test(httptest.NewRequest("GET", "/system/stats", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 501 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if readErr(t, res.Body) == "" {
		t.Fatal("501 要附明確訊息")
	}
}

func TestSystemStats_OtherError500(t *testing.T) {
	res, err := statsApp(fakeStats{err: errors.New("boom")}).Test(httptest.NewRequest("GET", "/system/stats", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatalf("status=%d", res.StatusCode)
	}
}
