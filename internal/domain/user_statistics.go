package domain

type UserStatistics struct {
	Total          int64 `json:"total_users"`
	Active         int64 `json:"active_users"`
	Pending        int64 `json:"pending_approvals"`
	Blocked        int64 `json:"blocked_users"`
	TotalUsedBytes int64 `json:"total_used_bytes"`
}
