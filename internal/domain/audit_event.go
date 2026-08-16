package domain

import (
	"encoding/json"
	"time"
)

type AuditEvent struct {
	ID              int64           `json:"id"`
	ActorTelegramID *int64          `json:"actor_telegram_id"`
	Action          string          `json:"action"`
	TargetType      string          `json:"target_type"`
	TargetID        string          `json:"target_id"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       time.Time       `json:"created_at"`
}
