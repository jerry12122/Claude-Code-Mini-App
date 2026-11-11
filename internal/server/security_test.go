package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
)

func loginForSecurityTest(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Post(base+"/auth/login", "application/json", strings.NewReader(`{"password":"security-test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var login struct{ Token string }
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil || login.Token == "" {
		t.Fatalf("login: %v status=%d", err, resp.StatusCode)
	}
	return login.Token
}

func TestWebLoginFailureLimitKeepsExistingSession(t *testing.T) {
	base := startTestServer(t, "web:\n  password: security-test-password\n  trusted_proxies: [192.0.2.0/24]\n")
	token := loginForSecurityTest(t, base)
	login := func(password string, want int) {
		t.Helper()
		req, _ := http.NewRequest("POST", base+"/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		// Untrusted forwarding headers must not bypass the source-IP limit.
		req.Header.Set("X-Forwarded-For", "192.0.2.99")
		req.Header.Set("CF-Connecting-IP", "192.0.2.99")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("login status=%d, want %d", resp.StatusCode, want)
		}
		if want == 429 && resp.Header.Get("Retry-After") == "" {
			t.Fatal("limited login missing Retry-After")
		}
	}
	for i := 0; i < 4; i++ {
		login("wrong", 401)
	}
	login("security-test-password", 200)
	for i := 0; i < 5; i++ {
		login("wrong", 401)
	}
	login("security-test-password", 429)
	if got := sessionsStatus(t, base+"/sessions?token="+token); got != 200 {
		t.Fatalf("existing session status=%d", got)
	}
}

func waitSecuritySocketClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("socket remained authorized")
			}
			return
		}
	}
}

func TestLogoutClosesAllWebSockets(t *testing.T) {
	base := startTestServer(t, "web:\n  password: security-test-password\n")
	token := loginForSecurityTest(t, base)
	req, _ := http.NewRequest("POST", base+"/sessions", strings.NewReader(`{"name":"security","work_dir":"."}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var session struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	if session.ID == "" {
		t.Fatal("session not created")
	}
	for _, route := range []string{"/sessions/" + session.ID + "/ws", "/events", "/logs/ws"} {
		t.Run(route, func(t *testing.T) {
			token := loginForSecurityTest(t, base)
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+route+"?token="+token, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			req, _ := http.NewRequest("POST", base+"/auth/logout", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			waitSecuritySocketClosed(t, c)
			if code := sessionsStatus(t, base+"/sessions?token="+token); code != 401 {
				t.Fatalf("revoked token status=%d", code)
			}
		})
	}
}

func TestWebSocketExpiresAndRejectsLargeFrames(t *testing.T) {
	base := startTestServer(t, "web:\n  password: security-test-password\n  session_ttl: 500ms\n")
	token := loginForSecurityTest(t, base)
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/events?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitSecuritySocketClosed(t, c)

	token = loginForSecurityTest(t, base)
	c, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/events?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The size rejection must happen well before the login's expiry.
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_ = c.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), apiBodyLimit+1))
	_, _, err = c.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("large frame error: %v", err)
	}
}

func TestHTTPBodyLimits(t *testing.T) {
	base := startTestServer(t, "no_auth: true\n")
	conn1, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	conn1.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn1, "POST /auth/login HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n", apiBodyLimit+1)
	conn1.Write(bytes.Repeat([]byte("x"), 8<<10))
	oversized, err := http.ReadResponse(bufio.NewReader(conn1), nil)
	if err != nil {
		t.Fatal(err)
	}
	oversized.Body.Close()
	conn1.Close()
	if oversized.StatusCode != 413 {
		t.Fatalf("oversized status=%d", oversized.StatusCode)
	}
	// Declare a chunk larger than the limit without sending its payload.
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "POST /auth/login HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n", smallBodyLimit+1)
	conn.Write(bytes.Repeat([]byte("x"), smallBodyLimit+1))
	conn.Write([]byte("\r\n0\r\n\r\n"))
	chunked, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	chunked.Body.Close()
	if chunked.StatusCode != 413 {
		t.Fatalf("chunked status=%d", chunked.StatusCode)
	}
	req, _ := http.NewRequest("POST", base+"/auth/login", strings.NewReader("small"))
	req.Header.Set("Content-Encoding", "br")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 415 {
		t.Fatalf("compressed status=%d", resp.StatusCode)
	}

	resp, err = http.Post(base+"/sessions", "application/json", strings.NewReader(`{"name":"upload","work_dir":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	var session struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "two-MiB.bin")
	file.Write(bytes.Repeat([]byte("x"), 2<<20))
	form.Close()
	resp, err = http.Post(base+"/sessions/"+session.ID+"/uploads", form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, raw)
	}
}

func TestTelegramSessionSurvivesInitDataAgeAndRevokes(t *testing.T) {
	base := startTestServer(t, "bot_token: bot-secret\nwhitelist_tg_ids: [123]\nweb:\n  allowed_cidrs: [192.0.2.0/24]\n  session_ttl: 300ms\n")
	raw := signInitData("bot-secret", `{"id":123}`, time.Now().Add(-24*time.Hour+2*time.Second))
	req, _ := http.NewRequest("POST", base+"/auth/telegram", nil)
	req.Header.Set("X-Telegram-Init-Data", raw)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var login struct{ Token string }
	json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if login.Token == "" {
		t.Fatalf("exchange status=%d", resp.StatusCode)
	}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/events?token="+login.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				closed <- err
				return
			}
		}
	}()
	time.Sleep(2300 * time.Millisecond)
	if got := sessionsStatusHeader(t, base, raw); got != 401 {
		t.Fatalf("expired initData status=%d", got)
	}
	if got := sessionsStatus(t, base+"/sessions?token="+login.Token); got != 200 {
		t.Fatalf("active Telegram session status=%d", got)
	}
	req, _ = http.NewRequest("POST", base+"/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Telegram socket not revoked")
	}
	if got := sessionsStatus(t, base+"/sessions?token="+login.Token); got != 401 {
		t.Fatalf("revoked Telegram token status=%d", got)
	}
	t.Run("offline socket cannot renew its session", func(t *testing.T) {
		req, _ := http.NewRequest("POST", base+"/auth/telegram", nil)
		req.Header.Set("X-Telegram-Init-Data", signInitData("bot-secret", `{"id":123}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var offline struct{ Token string }
		json.NewDecoder(resp.Body).Decode(&offline)
		resp.Body.Close()
		if offline.Token == "" {
			t.Fatalf("exchange status=%d", resp.StatusCode)
		}
		socket, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/events?token="+offline.Token, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer socket.Close()
		// Do not read: no native pong is sent, just like an unreachable client.
		time.Sleep(500 * time.Millisecond)
		if got := sessionsStatus(t, base+"/sessions?token="+offline.Token); got != 401 {
			t.Fatalf("offline Telegram token status=%d", got)
		}
	})
}

type repeatedUploadByte struct{}

func (repeatedUploadByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func TestLargeUploadStreamsAfterAuthentication(t *testing.T) {
	base := startTestServer(t, "web:\n  password: security-test-password\n")
	token := loginForSecurityTest(t, base)
	req, _ := http.NewRequest("POST", base+"/sessions", strings.NewReader(`{"name":"large","work_dir":"."}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var session struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	pipe, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		f, err := form.CreateFormFile("file", "40MiB.bin")
		if err == nil {
			_, err = io.CopyN(f, repeatedUploadByte{}, 40<<20)
		}
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
		writeDone <- err
	}()
	req, _ = http.NewRequest("POST", base+"/sessions/"+session.ID+"/uploads", pipe)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Attachment struct{ Size int64 }
		Error      string
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != 200 || result.Attachment.Size != 40<<20 {
		t.Fatalf("status=%d result=%+v", resp.StatusCode, result)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	// Reject a large unauthenticated body without waiting for its data.
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	fmt.Fprintf(conn, "POST /sessions/%s/uploads HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1073741824\r\n\r\n", session.ID)
	conn.Write(bytes.Repeat([]byte("x"), 8<<10))
	denied, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatalf("unauthorized upload status=%d", denied.StatusCode)
	}
}
