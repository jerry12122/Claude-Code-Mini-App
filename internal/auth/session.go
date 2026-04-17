package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// sessionEntry 單一 Web session 的過期時間與對應的 Telegram 使用者（供通知用）。
type sessionEntry struct {
	exp  time.Time
	tgID int64
}

// Store 是記憶體內的 session token 儲存區。
type Store struct {
	mu       sync.Mutex
	sessions map[string]sessionEntry
	ttl      time.Duration
}

func NewStore(ttl time.Duration) *Store {
	s := &Store{
		sessions: make(map[string]sessionEntry),
		ttl:      ttl,
	}
	go s.cleanup()
	return s
}

// Create 產生一個新的 session token。tgID 為登入時綁定的 Telegram 使用者（白名單內），0 表示未綁定（不發 TG 通知）。
func (s *Store) Create(tgID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = sessionEntry{exp: time.Now().Add(s.ttl), tgID: tgID}
	s.mu.Unlock()
	return token, nil
}

// Validate 檢查 token 是否有效且未過期，並回傳綁定的 tg_id（可能為 0）。
func (s *Store) Validate(token string) (valid bool, tgID int64) {
	if token == "" {
		return false, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.sessions[token]
	if !ok {
		return false, 0
	}
	if time.Now().After(ent.exp) {
		delete(s.sessions, token)
		return false, 0
	}
	return true, ent.tgID
}

// Delete 登出時移除 token。
func (s *Store) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// cleanup 每小時清除過期的 session。
func (s *Store) cleanup() {
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, ent := range s.sessions {
			if now.After(ent.exp) {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}
