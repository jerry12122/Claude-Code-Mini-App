package quota

import "context"

// Fetcher 依 provider 擷取帳戶 quota（各實作自行決定 CLI / API）。
type Fetcher interface {
	Provider() string
	Fetch(ctx context.Context) (Snapshot, error)
}
