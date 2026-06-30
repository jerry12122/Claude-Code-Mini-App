package quota

import (
	"context"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

type AntigravityFetcher struct{}

func (f *AntigravityFetcher) Provider() string { return agent.TypeAntigravity }

func (f *AntigravityFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	return Snapshot{
		Provider:    agent.TypeAntigravity,
		DisplayText: "未實作",
		UpdatedAt:   time.Now(),
	}, nil
}
