package quota

import "time"

// Snapshot 是給 API / WS 的帳戶 quota 快照（DisplayText 已組好，前端直接顯示）。
type Snapshot struct {
	Provider    string    `json:"provider"`
	DisplayText string    `json:"display_text"`
	UpdatedAt   time.Time `json:"updated_at"`
	Stale       bool      `json:"stale,omitempty"`
	Fetching    bool      `json:"fetching,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Payload 是 WS / sync 用的精簡欄位。
type Payload struct {
	DisplayText string `json:"display_text,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Stale       bool   `json:"stale,omitempty"`
	Fetching    bool   `json:"fetching,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (s Snapshot) ToPayload() Payload {
	p := Payload{
		DisplayText: s.DisplayText,
		Stale:       s.Stale,
		Fetching:    s.Fetching,
		Error:       s.Error,
	}
	if !s.UpdatedAt.IsZero() {
		p.UpdatedAt = s.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return p
}
