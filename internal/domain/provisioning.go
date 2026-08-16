package domain

import (
	"errors"
	"fmt"
	"time"
)

type BundleIssuer interface {
	Issue() (CredentialBundle, error)
}

type ProvisionedAccess struct {
	Issuance    Issuance
	Credentials CredentialBundle
}

type ProvisioningService struct {
	issuer BundleIssuer
}

func NewProvisioningService(issuer BundleIssuer) ProvisioningService {
	return ProvisioningService{issuer: issuer}
}

func (service ProvisioningService) Claim(account *AccessAccount, now time.Time) (ProvisionedAccess, error) {
	return service.provision(account, func() (Issuance, error) {
		return account.Claim(now)
	})
}

func (service ProvisioningService) Approve(account *AccessAccount, now time.Time) (ProvisionedAccess, error) {
	return service.provision(account, func() (Issuance, error) {
		return account.Approve(now)
	})
}

func (service ProvisioningService) Rotate(account *AccessAccount, now time.Time, resetPeriod bool) (ProvisionedAccess, error) {
	return service.provision(account, func() (Issuance, error) {
		return account.Rotate(now, resetPeriod)
	})
}

func (service ProvisioningService) provision(account *AccessAccount, issueAccess func() (Issuance, error)) (ProvisionedAccess, error) {
	if account == nil {
		return ProvisionedAccess{}, errors.New("access account is required")
	}
	if service.issuer == nil {
		return ProvisionedAccess{}, errors.New("credential issuer is required")
	}

	credentials, err := service.issuer.Issue()
	if err != nil {
		return ProvisionedAccess{}, fmt.Errorf("issue credentials: %w", err)
	}
	issuance, err := issueAccess()
	if err != nil {
		return ProvisionedAccess{}, err
	}
	return ProvisionedAccess{Issuance: issuance, Credentials: credentials}, nil
}
