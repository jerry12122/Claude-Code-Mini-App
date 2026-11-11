package tg

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyFreshness(t *testing.T) {
	const token = "test-secret"
	sign := func(v url.Values) string {
		var pairs []string
		for k, values := range v {
			pairs = append(pairs, k+"="+values[0])
		}
		sort.Strings(pairs)
		key := hmac.New(sha256.New, []byte("WebAppData"))
		key.Write([]byte(token))
		mac := hmac.New(sha256.New, key.Sum(nil))
		mac.Write([]byte(strings.Join(pairs, "\n")))
		v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
		return v.Encode()
	}
	for _, tc := range []struct {
		name                      string
		age                       time.Duration
		missing, duplicate, valid bool
	}{
		{name: "fresh", valid: true},
		{name: "clock skew", age: -30 * time.Second, valid: true},
		{name: "expired", age: MaxInitDataAge + time.Second},
		{name: "future", age: -2 * time.Minute},
		{name: "missing", missing: true},
		{name: "duplicate", duplicate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := url.Values{"user": {`{"id":123}`}}
			if !tc.missing {
				v.Set("auth_date", strconv.FormatInt(time.Now().Add(-tc.age).Unix(), 10))
			}
			if tc.duplicate {
				v.Add("auth_date", "1")
			}
			raw := sign(v)
			user, err := Verify(raw, token)
			if (err == nil) != tc.valid {
				t.Fatalf("user=%+v err=%v", user, err)
			}
			if _, err := Verify(raw, ""); err == nil {
				t.Fatal("empty bot token accepted")
			}
		})
	}
}
