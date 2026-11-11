package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// signInitData 用與 tg.Verify 相同的算法簽出 initData：
// secret = HMAC_SHA256(key=WebAppData, msg=botToken)，hash = HMAC_SHA256(secret, data_check_string)。
func signInitData(botToken, userJSON string, dates ...time.Time) string {
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(botToken))
	secret := mac.Sum(nil)
	mac2 := hmac.New(sha256.New, secret)
	now := time.Now()
	if len(dates) > 0 {
		now = dates[0]
	}
	stamp := strconv.FormatInt(now.Unix(), 10)
	mac2.Write([]byte("auth_date=" + stamp + "\nuser=" + userJSON))
	v := url.Values{}
	v.Set("user", userJSON)
	v.Set("auth_date", stamp)
	v.Set("hash", hex.EncodeToString(mac2.Sum(nil)))
	return v.Encode()
}

func sessionsStatus(t *testing.T, rawURL string) int {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func sessionsStatusHeader(t *testing.T, base, initData string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Telegram-Init-Data", initData)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestEmptyBotTokenIgnoresInitData(t *testing.T) {
	base := startTestServer(t, "bot_token: \"\"\nwhitelist_tg_ids: [123456789]\nweb:\n  password: correct-horse\n")
	initData := signInitData("", `{"id":123456789}`)
	// 偽造的 initData 要被當成沒帶：沒有 session cookie 的本機請求得到 401，不是 200 也不是其他錯誤。
	if code := sessionsStatusHeader(t, base, initData); code != 401 {
		t.Fatalf("空 token 時標頭 initData 得到 %d，預期 401", code)
	}
	if code := sessionsStatus(t, base+"/sessions?initData="+url.QueryEscape(initData)); code != 401 {
		t.Fatalf("空 token 時查詢參數 initData 得到 %d，預期 401", code)
	}
}

func TestBotTokenSetStillVerifiesInitData(t *testing.T) {
	const token = "123456:TEST"
	base := startTestServer(t, "bot_token: \"123456:TEST\"\nwhitelist_tg_ids: [123456789]\nweb:\n  password: correct-horse\n")
	initData := signInitData(token, `{"id":123456789}`)
	if code := sessionsStatusHeader(t, base, initData); code != 200 {
		t.Fatalf("正確 token 的 initData 標頭得到 %d，預期 200", code)
	}
}
