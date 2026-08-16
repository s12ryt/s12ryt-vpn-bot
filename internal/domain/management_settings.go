package domain

import "errors"

type ManagementSettings struct {
	QualificationMode        QualificationMode `json:"qualification_mode"`
	RecheckIntervalMinutes   int               `json:"recheck_interval_minutes"`
	RecheckRequestsPerSecond int               `json:"recheck_requests_per_second"`
	RecheckBatchSize         int               `json:"recheck_batch_size"`
	InactivityThresholdDays  int               `json:"inactivity_threshold_days"`
	QuotaLimitBytes          int64             `json:"quota_limit_bytes"`
}

func (settings ManagementSettings) Validate() error {
	if settings.QualificationMode != QualificationAny && settings.QualificationMode != QualificationAll {
		return errors.New("qualification mode is invalid")
	}
	if settings.RecheckIntervalMinutes < 1 || settings.RecheckIntervalMinutes > 10080 {
		return errors.New("recheck interval must be between 1 and 10080 minutes")
	}
	if settings.RecheckRequestsPerSecond < 1 || settings.RecheckRequestsPerSecond > 20 {
		return errors.New("recheck rate must be between 1 and 20 requests per second")
	}
	if settings.RecheckBatchSize < 10 || settings.RecheckBatchSize > 200 {
		return errors.New("recheck batch size must be between 10 and 200")
	}
	if settings.InactivityThresholdDays < 0 {
		return errors.New("inactivity threshold cannot be negative")
	}
	if settings.QuotaLimitBytes <= 0 {
		return errors.New("quota limit must be positive")
	}
	return nil
}

type QualificationRuleOverview struct {
	ChatID                 int64  `json:"chat_id"`
	ChatType               string `json:"chat_type"`
	Title                  string `json:"title"`
	Enabled                bool   `json:"enabled"`
	BotAdministratorPassed bool   `json:"bot_administrator_passed"`
}

func (rule QualificationRuleOverview) Validate() error {
	if rule.ChatID == 0 {
		return errors.New("qualification rule chat ID is invalid")
	}
	if rule.ChatType != "supergroup" && rule.ChatType != "channel" {
		return errors.New("qualification rule chat type is invalid")
	}
	if rule.Enabled && !rule.BotAdministratorPassed {
		return errors.New("enabled qualification rule was not administrator verified")
	}
	return nil
}
