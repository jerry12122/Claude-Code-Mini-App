package server

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRobotsTxtAndAntiIndexingHeaders(t *testing.T) {
	base := startTestServer(t, "web:\n  password: test-pass\n")

	// 1. 測試 /robots.txt 端點
	resp, err := http.Get(base + "/robots.txt")
	if err != nil {
		t.Fatalf("GET /robots.txt failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	expected := "User-agent: *\nDisallow: /\n"
	if string(body) != expected {
		t.Errorf("body = %q, want %q", string(body), expected)
	}

	// 2. 測試防索引與隱私安全標頭
	tests := []struct {
		path string
	}{
		{"/robots.txt"},
		{"/"},
		{"/setup/status"},
	}
	for _, tt := range tests {
		r, err := http.Get(base + tt.path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tt.path, err)
		}
		r.Body.Close()

		if got := r.Header.Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive, nosnippet" {
			t.Errorf("path %s: X-Robots-Tag = %q, want %q", tt.path, got, "noindex, nofollow, noarchive, nosnippet")
		}
		if got := r.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("path %s: X-Content-Type-Options = %q, want nosniff", tt.path, got)
		}
		if got := r.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("path %s: Referrer-Policy = %q, want no-referrer", tt.path, got)
		}
	}
}

func TestPWAManifestAndServiceWorker(t *testing.T) {
	base := startTestServer(t, "web:\n  password: test-pass\n")

	// 1. 測試 /manifest.webmanifest
	resp, err := http.Get(base + "/manifest.webmanifest")
	if err != nil {
		t.Fatalf("GET /manifest.webmanifest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/manifest+json") {
		t.Errorf("Content-Type = %q, want application/manifest+json", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read manifest body: %v", err)
	}
	if !strings.Contains(string(body), `"standalone"`) {
		t.Errorf("manifest body missing standalone display: %s", string(body))
	}
	if !strings.Contains(string(body), `"Claude Code"`) {
		t.Errorf("manifest body missing Claude Code: %s", string(body))
	}

	// 2. 測試 /sw.js
	swResp, err := http.Get(base + "/sw.js")
	if err != nil {
		t.Fatalf("GET /sw.js failed: %v", err)
	}
	defer swResp.Body.Close()

	if swResp.StatusCode != http.StatusOK {
		t.Fatalf("sw status = %d, want %d", swResp.StatusCode, http.StatusOK)
	}
	if got := swResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("sw Content-Type = %q, want text/javascript", got)
	}
}

