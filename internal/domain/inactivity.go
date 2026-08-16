package domain

import (
	"errors"
	"sort"
	"time"
)

func PreviewInactiveAccounts(accounts []*AccessAccount, now time.Time, thresholdDays int) ([]int64, error) {
	if thresholdDays < 0 {
		return nil, errors.New("inactivity threshold cannot be negative")
	}
	if thresholdDays == 0 {
		return []int64{}, nil
	}

	telegramIDs := make([]int64, 0)
	for _, account := range accounts {
		if account == nil {
			return nil, errors.New("access account is required")
		}
		inactive, err := account.IsInactiveAt(now, thresholdDays)
		if err != nil {
			return nil, err
		}
		if inactive {
			telegramIDs = append(telegramIDs, account.telegramID)
		}
	}
	sort.Slice(telegramIDs, func(i, j int) bool {
		return telegramIDs[i] < telegramIDs[j]
	})
	return telegramIDs, nil
}
