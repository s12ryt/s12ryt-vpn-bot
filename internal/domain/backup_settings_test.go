package domain

import "testing"

func TestBackupSettingsValidateRetentionRange(t *testing.T) {
	for _, days := range []int{1, 7, 3650} {
		if err := (BackupSettings{RetentionDays: days}).Validate(); err != nil {
			t.Fatalf("Validate(%d) error = %v", days, err)
		}
	}
	for _, days := range []int{0, -1, 3651} {
		if err := (BackupSettings{RetentionDays: days}).Validate(); err == nil {
			t.Fatalf("Validate(%d) accepted invalid retention", days)
		}
	}
}
