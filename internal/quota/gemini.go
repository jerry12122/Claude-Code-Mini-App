package quota

import (
	"context"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

type GeminiFetcher struct{}

func (f *GeminiFetcher) Provider() string { return agent.TypeGemini }

func (f *GeminiFetcher) Fetch(ctx context.Context) (Snapshot, error) {
	return Snapshot{
		Provider:    agent.TypeGemini,
		DisplayText: "未實作",
		UpdatedAt:   time.Now(),
	}, nil
}
