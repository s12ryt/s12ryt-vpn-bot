package domain

import (
	"errors"
	"time"
)

var (
	ErrNotEligible        = errors.New("user is not eligible")
	ErrApprovalRequired   = errors.New("administrator approval is required")
	ErrAlreadyActive      = errors.New("access is already active")
	ErrPermanentlyBlocked = errors.New("access is permanently blocked")
)

type AccessStatus string

const (
	AccessStatusUnclaimed          AccessStatus = "unclaimed"
	AccessStatusActive             AccessStatus = "active"
	AccessStatusPendingApproval    AccessStatus = "pending_approval"
	AccessStatusApprovalRejected   AccessStatus = "approval_rejected"
	AccessStatusSelfService        AccessStatus = "self_service"
	AccessStatusPermanentlyBlocked AccessStatus = "permanently_blocked"
)

type RevocationMode string

const (
	RevocationModeSelfService     RevocationMode = "self_service"
	RevocationModeRequireApproval RevocationMode = "require_approval"
	RevocationModePermanentBlock  RevocationMode = "permanent_block"
)

type AccessAccount struct {
	telegramID           int64
	eligible             bool
	status               AccessStatus
	credentialGeneration uint64
	periodStartedAt      time.Time
	lastVPNActivityAt    time.Time
}

type AccessChange struct {
	RevokeCredentialsImmediately bool
}

type Issuance struct {
	CredentialGeneration uint64
	PeriodStartedAt      time.Time
}

type AccessSnapshot struct {
	TelegramID           int64
	Eligible             bool
	Status               AccessStatus
	CredentialGeneration uint64
	PeriodStartedAt      time.Time
	LastVPNActivityAt    time.Time
}

func NewAccessAccount(telegramID int64) (*AccessAccount, error) {
	if telegramID <= 0 {
		return nil, errors.New("Telegram ID must be positive")
	}
	return &AccessAccount{
		telegramID: telegramID,
		status:     AccessStatusUnclaimed,
	}, nil
}

func RestoreAccessAccount(snapshot AccessSnapshot) (*AccessAccount, error) {
	if snapshot.TelegramID <= 0 {
		return nil, errors.New("Telegram ID must be positive")
	}
	switch snapshot.Status {
	case AccessStatusUnclaimed:
		if snapshot.CredentialGeneration != 0 || !snapshot.PeriodStartedAt.IsZero() || !snapshot.LastVPNActivityAt.IsZero() {
			return nil, errors.New("unclaimed access cannot have issuance state")
		}
	case AccessStatusActive, AccessStatusPendingApproval, AccessStatusApprovalRejected, AccessStatusSelfService, AccessStatusPermanentlyBlocked:
		if snapshot.CredentialGeneration == 0 || snapshot.PeriodStartedAt.IsZero() || snapshot.LastVPNActivityAt.IsZero() {
			return nil, errors.New("issued access requires complete issuance state")
		}
		if snapshot.LastVPNActivityAt.Before(snapshot.PeriodStartedAt) {
			return nil, errors.New("VPN activity cannot predate the access period")
		}
	default:
		return nil, errors.New("access status is invalid")
	}
	if snapshot.Status == AccessStatusActive && !snapshot.Eligible {
		return nil, errors.New("active access must be eligible")
	}
	return &AccessAccount{
		telegramID:           snapshot.TelegramID,
		eligible:             snapshot.Eligible,
		status:               snapshot.Status,
		credentialGeneration: snapshot.CredentialGeneration,
		periodStartedAt:      snapshot.PeriodStartedAt,
		lastVPNActivityAt:    snapshot.LastVPNActivityAt,
	}, nil
}

func (account *AccessAccount) SetEligibility(eligible bool) AccessChange {
	account.eligible = eligible
	if !eligible && account.status == AccessStatusActive {
		account.status = AccessStatusPendingApproval
		return AccessChange{RevokeCredentialsImmediately: true}
	}
	if !eligible && account.status == AccessStatusSelfService {
		account.status = AccessStatusPendingApproval
	}
	return AccessChange{}
}

func (account *AccessAccount) Claim(now time.Time) (Issuance, error) {
	if now.IsZero() {
		return Issuance{}, errors.New("issuance timestamp is required")
	}
	if !account.eligible {
		return Issuance{}, ErrNotEligible
	}
	switch account.status {
	case AccessStatusPermanentlyBlocked:
		return Issuance{}, ErrPermanentlyBlocked
	case AccessStatusPendingApproval, AccessStatusApprovalRejected:
		return Issuance{}, ErrApprovalRequired
	case AccessStatusActive:
		return Issuance{}, ErrAlreadyActive
	default:
		return account.issue(now), nil
	}
}

func (account *AccessAccount) Revoke(mode RevocationMode) (AccessChange, error) {
	if account.status != AccessStatusActive {
		return AccessChange{}, errors.New("only active access can be revoked")
	}

	switch mode {
	case RevocationModeSelfService:
		account.status = AccessStatusSelfService
	case RevocationModeRequireApproval:
		account.status = AccessStatusPendingApproval
	case RevocationModePermanentBlock:
		account.status = AccessStatusPermanentlyBlocked
	default:
		return AccessChange{}, errors.New("revocation mode is invalid")
	}
	return AccessChange{RevokeCredentialsImmediately: true}, nil
}

func (account *AccessAccount) Rotate(now time.Time, resetPeriod bool) (Issuance, error) {
	if account.status != AccessStatusActive {
		return Issuance{}, errors.New("only active access can be rotated")
	}
	if now.IsZero() || now.Before(account.periodStartedAt) {
		return Issuance{}, errors.New("rotation timestamp is invalid")
	}
	account.credentialGeneration++
	if resetPeriod {
		account.periodStartedAt = now
		account.lastVPNActivityAt = now
	}
	return Issuance{
		CredentialGeneration: account.credentialGeneration,
		PeriodStartedAt:      account.periodStartedAt,
	}, nil
}

func (account *AccessAccount) RecordVPNActivity(observedAt time.Time, bytes int64) error {
	if account.status != AccessStatusActive {
		return errors.New("VPN activity requires active access")
	}
	if bytes <= 0 {
		return errors.New("VPN activity bytes must be positive")
	}
	if observedAt.IsZero() || observedAt.Before(account.lastVPNActivityAt) {
		return errors.New("VPN activity timestamp is invalid")
	}
	account.lastVPNActivityAt = observedAt
	return nil
}

func (account *AccessAccount) IsInactiveAt(now time.Time, thresholdDays int) (bool, error) {
	if thresholdDays < 0 {
		return false, errors.New("inactivity threshold cannot be negative")
	}
	if thresholdDays == 0 || account.status != AccessStatusActive {
		return false, nil
	}
	if now.IsZero() || now.Before(account.lastVPNActivityAt) {
		return false, errors.New("inactivity evaluation timestamp is invalid")
	}
	const day = 24 * time.Hour
	if thresholdDays > int((1<<63-1)/day) {
		return false, errors.New("inactivity threshold is too large")
	}
	boundary := account.lastVPNActivityAt.Add(time.Duration(thresholdDays) * day)
	return !now.Before(boundary), nil
}

func (account *AccessAccount) RemoveForInactivity() (AccessChange, error) {
	return account.Revoke(RevocationModeSelfService)
}

func (account *AccessAccount) RejectApproval() error {
	if account.status != AccessStatusPendingApproval {
		return errors.New("access is not pending approval")
	}
	account.status = AccessStatusApprovalRejected
	return nil
}

func (account *AccessAccount) Approve(now time.Time) (Issuance, error) {
	if now.IsZero() {
		return Issuance{}, errors.New("approval timestamp is required")
	}
	if !account.eligible {
		return Issuance{}, ErrNotEligible
	}
	if account.status != AccessStatusPendingApproval && account.status != AccessStatusApprovalRejected {
		return Issuance{}, errors.New("access is not pending approval")
	}
	return account.issue(now), nil
}

func (account *AccessAccount) Snapshot() AccessSnapshot {
	return AccessSnapshot{
		TelegramID:           account.telegramID,
		Eligible:             account.eligible,
		Status:               account.status,
		CredentialGeneration: account.credentialGeneration,
		PeriodStartedAt:      account.periodStartedAt,
		LastVPNActivityAt:    account.lastVPNActivityAt,
	}
}

func (account *AccessAccount) issue(now time.Time) Issuance {
	account.status = AccessStatusActive
	account.credentialGeneration++
	account.periodStartedAt = now
	account.lastVPNActivityAt = now
	return Issuance{
		CredentialGeneration: account.credentialGeneration,
		PeriodStartedAt:      account.periodStartedAt,
	}
}
