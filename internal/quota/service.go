package quota

import (
	"context"

	"sync"
	"time"

	"fmt"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"log/slog"
)

const ManualCooldown = 60 * time.Second

var defaultTTL = map[string]time.Duration{
	agent.TypeClaude:  5 * time.Minute,
	agent.TypeCursor:  3 * time.Minute,
	agent.TypeKiro:    5 * time.Minute,
	agent.TypeKiroACP: 5 * time.Minute,
	agent.TypeCodex:   5 * time.Minute,
}

type cacheEntry struct {
	snap      Snapshot
	fetchedAt time.Time
}

type inflightCall struct {
	done chan struct{}
	snap Snapshot
	err  error
}

// Service 是 global per-provider quota 快取（帳戶級，非 session 級）。
type Service struct {
	mu         sync.RWMutex
	cache      map[string]*cacheEntry
	lastManual map[string]time.Time
	fetchers   map[string]Fetcher
	inflight   map[string]*inflightCall
}

func NewService() *Service {
	kiroFetcher := &KiroFetcher{}
	fetchers := map[string]Fetcher{
		agent.TypeClaude:      &ClaudeFetcher{},
		agent.TypeCursor:      &CursorFetcher{},
		agent.TypeKiro:        kiroFetcher,
		agent.TypeKiroACP:     kiroFetcher, // 同一 kiro-cli 帳號，用量共用
		agent.TypeCodex:       &CodexFetcher{},
		agent.TypeAntigravity: &AntigravityFetcher{},
	}
	return &Service{
		cache:      make(map[string]*cacheEntry),
		lastManual: make(map[string]time.Time),
		fetchers:   fetchers,
		inflight:   make(map[string]*inflightCall),
	}
}

func (s *Service) ttl(provider string) time.Duration {
	if d, ok := defaultTTL[provider]; ok {
		return d
	}
	return 5 * time.Minute
}

func normalizeProvider(p string) string {
	if p == "" {
		return agent.TypeClaude
	}
	if p == agent.TypeGemini {
		return agent.TypeAntigravity
	}
	return p
}

// Get 回傳 cache 快照（不觸發 fetch）。
func (s *Service) Get(provider string) Snapshot {
	provider = normalizeProvider(provider)
	s.mu.RLock()
	ent, ok := s.cache[provider]
	s.mu.RUnlock()
	if !ok {
		return Snapshot{Provider: provider, DisplayText: "—"}
	}
	snap := ent.snap
	snap.Stale = snap.Stale || time.Since(ent.fetchedAt) > s.ttl(provider)*2
	if snap.UpdatedAt.IsZero() && !ent.snap.Stale {
		snap.UpdatedAt = ent.fetchedAt
	}
	return snap
}

// GetAll 回傳已知 provider 的 cache。
func (s *Service) GetAll() map[string]Snapshot {
	out := make(map[string]Snapshot)
	for _, p := range []string{agent.TypeClaude, agent.TypeCursor, agent.TypeKiro, agent.TypeKiroACP, agent.TypeCodex, agent.TypeAntigravity} {
		out[p] = s.Get(p)
	}
	return out
}

// Warmup 冷啟動時背景 prefetch（各 provider 一次）。
func (s *Service) Warmup(ctx context.Context) {
	for _, p := range []string{agent.TypeClaude, agent.TypeCursor, agent.TypeKiro, agent.TypeKiroACP, agent.TypeCodex, agent.TypeAntigravity} {
		provider := p
		go func() {
			if _, err := s.refresh(ctx, provider, false); err != nil {
				slog.Info(fmt.Sprintf("[quota] warmup %s: %v", provider, err))
			}
		}()
	}
}

// RefreshAfterRun agent 完成後若 cache 過期則背景更新。
func (s *Service) RefreshAfterRun(ctx context.Context, provider string) (Snapshot, error) {
	provider = normalizeProvider(provider)
	s.mu.RLock()
	ent, ok := s.cache[provider]
	s.mu.RUnlock()
	if ok && time.Since(ent.fetchedAt) < s.ttl(provider) {
		return s.Get(provider), nil
	}
	return s.refresh(ctx, provider, false)
}

// RefreshManual 手動刷新（60s cooldown，force fetch）。
func (s *Service) RefreshManual(ctx context.Context, provider string) (Snapshot, error) {
	provider = normalizeProvider(provider)
	s.mu.Lock()
	if t, ok := s.lastManual[provider]; ok && time.Since(t) < ManualCooldown {
		s.mu.Unlock()
		snap := s.Get(provider)
		snap.Error = "請稍後再試（60 秒冷卻）"
		return snap, nil
	}
	s.lastManual[provider] = time.Now()
	s.mu.Unlock()
	return s.refresh(ctx, provider, true)
}

// UseFetch 把現有 provider 的擷取換成行程內函式。隔離的臨時程序用它避開真供應商。
func (s *Service) UseFetch(fetch func(context.Context, string) (Snapshot, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.fetchers))
	for name := range s.fetchers {
		names = append(names, name)
	}
	for _, name := range names {
		provider := name
		s.fetchers[provider] = funcFetcher{provider: provider, fetch: func(ctx context.Context) (Snapshot, error) {
			return fetch(ctx, provider)
		}}
	}
}

// Store 寫入快取，不向外擷取。守門讀的是這份快取。
func (s *Service) Store(provider string, snap Snapshot) {
	provider = normalizeProvider(provider)
	snap.Provider = provider
	s.mu.Lock()
	s.cache[provider] = &cacheEntry{snap: snap, fetchedAt: time.Now()}
	s.mu.Unlock()
}

type funcFetcher struct {
	provider string
	fetch    func(context.Context) (Snapshot, error)
}

func (f funcFetcher) Provider() string { return f.provider }

func (f funcFetcher) Fetch(ctx context.Context) (Snapshot, error) { return f.fetch(ctx) }

func (s *Service) refresh(ctx context.Context, provider string, force bool) (Snapshot, error) {
	f, ok := s.fetchers[provider]
	if !ok {
		return Snapshot{Provider: provider}, nil
	}
	if !force {
		s.mu.RLock()
		ent, has := s.cache[provider]
		s.mu.RUnlock()
		if has && time.Since(ent.fetchedAt) < s.ttl(provider) {
			return s.Get(provider), nil
		}
	}

	s.mu.Lock()
	if call, ok := s.inflight[provider]; ok {
		s.mu.Unlock()
		<-call.done
		if call.err != nil {
			snap := s.Get(provider)
			if snap.DisplayText == "—" {
				snap.Error = call.err.Error()
			}
			return snap, call.err
		}
		return call.snap, nil
	}
	call := &inflightCall{done: make(chan struct{})}
	s.inflight[provider] = call
	s.mu.Unlock()

	defer func() {
		close(call.done)
		s.mu.Lock()
		delete(s.inflight, provider)
		s.mu.Unlock()
	}()

	snap, err := f.Fetch(ctx)
	call.snap, call.err = snap, err
	if err != nil {
		slog.Info(fmt.Sprintf("[quota] fetch %s: %v", provider, err))
		prev := s.Get(provider)
		if prev.DisplayText != "—" {
			prev.Error = err.Error()
			prev.Fetching = false
			call.snap = prev
			return prev, err
		}
		snap.Error = err.Error()
		snap.Fetching = false
		call.snap = snap
		return snap, err
	}

	s.mu.Lock()
	s.cache[provider] = &cacheEntry{snap: snap, fetchedAt: time.Now()}
	s.mu.Unlock()
	call.snap = snap
	return snap, nil
}
