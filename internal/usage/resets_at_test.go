package usage

import (
	"testing"
	"time"
)

func TestParseResetsAtRFC3339(t *testing.T) {
	ref := time.Date(2026, 6, 30, 11, 45, 0, 0, time.FixedZone("CST", 8*3600))
	got, ok := ParseResetsAt("2026-06-30T15:09:59+08:00", ref)
	if !ok {
		t.Fatal("expected parse ok")
	}
	want := time.Date(2026, 6, 30, 15, 9, 59, 0, time.FixedZone("CST", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseResetsAtHuman(t *testing.T) {
	ref := time.Date(2026, 6, 30, 11, 45, 0, 0, time.FixedZone("CST", 8*3600))
	got, ok := ParseResetsAt("Jun 30, 3:10pm (Asia/Taipei)", ref)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if got.Hour() != 15 || got.Minute() != 10 {
		t.Fatalf("got %v", got.Format(time.RFC3339))
	}
}

func TestFormatDurationUntil(t *testing.T) {
	now := time.Date(2026, 6, 30, 11, 45, 0, 0, time.UTC)
	until := now.Add(3*time.Hour + 25*time.Minute)
	if got := FormatDurationUntil(until, now); got != "Resets in 3 hr 25 min" {
		t.Fatalf("got %q", got)
	}
	if got := FormatDurationUntil(until, until); got != "" {
		t.Fatalf("past reset got %q", got)
	}
}
