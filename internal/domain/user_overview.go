package domain

import "time"

type UserOverview struct {
	TelegramID        int64        `json:"telegram_id"`
	Eligible          bool         `json:"eligible"`
	Status            AccessStatus `json:"status"`
	Generation        uint64       `json:"generation"`
	PeriodStartedAt   time.Time    `json:"period_started_at"`
	LastVPNActivityAt time.Time    `json:"last_vpn_activity_at"`
	UsedBytes         int64        `json:"used_bytes"`
	LimitBytes        int64        `json:"limit_bytes"`
	QuotaBlocked      bool         `json:"quota_blocked"`
}
