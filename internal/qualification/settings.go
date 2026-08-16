package qualification

import (
	"errors"
	"time"
)

type RecheckSettings struct {
	Interval          time.Duration
	RequestsPerSecond int
	BatchSize         int
}

func (settings RecheckSettings) Validate() error {
	if settings.Interval < time.Minute || settings.Interval > 7*24*time.Hour {
		return errors.New("recheck interval must be between one minute and seven days")
	}
	if settings.RequestsPerSecond < 1 || settings.RequestsPerSecond > 20 {
		return errors.New("recheck requests per second must be between 1 and 20")
	}
	if settings.BatchSize < 10 || settings.BatchSize > 200 {
		return errors.New("recheck batch size must be between 10 and 200")
	}
	return nil
}
