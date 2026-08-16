package domain

type UserStatistics struct {
	Total          int64
	Active         int64
	Pending        int64
	Blocked        int64
	TotalUsedBytes int64
}
