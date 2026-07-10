package model

import "time"

// Payload 是 WS / sync 用的精簡欄位（對齊 quota.Payload）。
type Payload struct {
	DisplayText string `json:"display_text,omitempty"`
	Source      string `json:"source,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func (i Info) ToPayload() Payload {
	p := Payload{
		DisplayText: i.DisplayText,
		Source:      string(i.Source),
	}
	if p.DisplayText == "" {
		p.DisplayText = "—"
	}
	return p
}

func (i Info) ToPayloadAt(t time.Time) Payload {
	p := i.ToPayload()
	if !t.IsZero() {
		p.UpdatedAt = t.UTC().Format(time.RFC3339)
	}
	return p
}

func PayloadFromStored(display, source, updatedAt string) Payload {
	p := Payload{
		DisplayText: display,
		Source:      source,
		UpdatedAt:   updatedAt,
	}
	if p.DisplayText == "" {
		p.DisplayText = "—"
	}
	return p
}
