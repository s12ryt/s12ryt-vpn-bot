package domain

import "errors"

// BackupSettings contains the owner-adjustable backup retention policy.
type BackupSettings struct {
	RetentionDays int `json:"retention_days"`
}

func (settings BackupSettings) Validate() error {
	if settings.RetentionDays < 1 || settings.RetentionDays > 3650 {
		return errors.New("backup retention days must be between 1 and 3650")
	}
	return nil
}
