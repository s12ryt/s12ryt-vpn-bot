package domain

import "testing"

func TestManagementSettingsValidation(t *testing.T) {
	valid := ManagementSettings{
		QualificationMode:        QualificationAny,
		RecheckIntervalMinutes:   60,
		RecheckRequestsPerSecond: 10,
		RecheckBatchSize:         50,
		InactivityThresholdDays:  0,
		QuotaLimitBytes:          50_000_000_000,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ManagementSettings)
	}{
		{"mode", func(settings *ManagementSettings) { settings.QualificationMode = "invalid" }},
		{"interval low", func(settings *ManagementSettings) { settings.RecheckIntervalMinutes = 0 }},
		{"interval high", func(settings *ManagementSettings) { settings.RecheckIntervalMinutes = 10081 }},
		{"rate low", func(settings *ManagementSettings) { settings.RecheckRequestsPerSecond = 0 }},
		{"rate high", func(settings *ManagementSettings) { settings.RecheckRequestsPerSecond = 21 }},
		{"batch low", func(settings *ManagementSettings) { settings.RecheckBatchSize = 9 }},
		{"batch high", func(settings *ManagementSettings) { settings.RecheckBatchSize = 201 }},
		{"inactivity negative", func(settings *ManagementSettings) { settings.InactivityThresholdDays = -1 }},
		{"quota non-positive", func(settings *ManagementSettings) { settings.QuotaLimitBytes = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := valid
			test.mutate(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestQualificationRuleOverviewValidation(t *testing.T) {
	valid := QualificationRuleOverview{ChatID: -100123, ChatType: "supergroup", Title: "Members", Enabled: true, BotAdministratorPassed: true}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	valid.ChatType = "private"
	if err := valid.Validate(); err == nil {
		t.Fatal("invalid chat type accepted")
	}
}
