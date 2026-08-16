package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestAdminCommandsListsUsersAndStatisticsForActiveAdministrator(t *testing.T) {
	administrators := &adminLookupStub{administrator: auth.Administrator{TelegramID: 9001, Role: auth.RoleAdministrator, Active: true}}
	users := &adminUserManagerStub{
		users:      []domain.UserOverview{{TelegramID: 12345, Status: domain.AccessStatusActive, UsedBytes: 25_000_000_000, LimitBytes: 50_000_000_000}},
		statistics: domain.UserStatistics{Total: 10, Active: 7, Pending: 2, Blocked: 1, TotalUsedBytes: 80_000_000_000},
	}
	service, err := NewAdminCommands(administrators, users, &adminProvisionerStub{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	listing, err := service.Execute(context.Background(), 9001, "/adminusers")
	if err != nil || !strings.Contains(listing, "12345") || !strings.Contains(listing, "25.00 GB / 50.00 GB") {
		t.Fatalf("adminusers = %q, %v", listing, err)
	}
	statistics, err := service.Execute(context.Background(), 9001, "/adminstats")
	if err != nil || !strings.Contains(statistics, "總使用者：10") || !strings.Contains(statistics, "總流量：80.00 GB") {
		t.Fatalf("adminstats = %q, %v", statistics, err)
	}
}

func TestAdminCommandsExecutesCanonicalRevokeAndRotate(t *testing.T) {
	now := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	users := &adminUserManagerStub{}
	provisioner := &adminProvisionerStub{}
	service, _ := NewAdminCommands(
		&adminLookupStub{administrator: auth.Administrator{TelegramID: 9001, Role: auth.RoleOwner, Active: true}},
		users, provisioner, func() time.Time { return now },
	)

	if reply, err := service.Execute(context.Background(), 9001, "/adminrevoke 12345 approval"); err != nil || reply != "已撤銷使用者 12345，重新領取需核准。" {
		t.Fatalf("adminrevoke = %q, %v", reply, err)
	}
	if users.revokedID != 12345 || users.revokedMode != domain.RevocationModeRequireApproval || users.actorID != 9001 {
		t.Fatalf("revoke = actor %d target %d mode %q", users.actorID, users.revokedID, users.revokedMode)
	}
	if reply, err := service.Execute(context.Background(), 9001, "/adminrotate 12345 reset"); err != nil || reply != "已輪替使用者 12345 的憑證並重置週期。" {
		t.Fatalf("adminrotate = %q, %v", reply, err)
	}
	if provisioner.telegramID != 12345 || !provisioner.resetPeriod {
		t.Fatalf("rotate = ID %d reset %v", provisioner.telegramID, provisioner.resetPeriod)
	}
}

func TestAdminCommandsRejectsUnauthorizedAndMalformedCommandsBeforeMutation(t *testing.T) {
	users := &adminUserManagerStub{}
	provisioner := &adminProvisionerStub{}
	service, _ := NewAdminCommands(&adminLookupStub{err: auth.ErrAdministratorUnauthorized}, users, provisioner, time.Now)
	if _, err := service.Execute(context.Background(), 9001, "/adminrevoke 12345 block"); !errors.Is(err, auth.ErrAdministratorUnauthorized) {
		t.Fatalf("unauthorized error = %v", err)
	}
	if users.revokedID != 0 {
		t.Fatal("unauthorized command mutated users")
	}

	service, _ = NewAdminCommands(&adminLookupStub{administrator: auth.Administrator{TelegramID: 9001, Role: auth.RoleAdministrator, Active: true}}, users, provisioner, time.Now)
	for _, command := range []string{"/adminrevoke", "/adminrevoke 01 self", "/adminrevoke 1 unknown", "/adminrotate 1", "/adminrotate 1 RESET"} {
		if _, err := service.Execute(context.Background(), 9001, command); err == nil {
			t.Fatalf("Execute(%q) error = nil", command)
		}
	}
}

type adminLookupStub struct {
	administrator auth.Administrator
	err           error
}

func (stub *adminLookupStub) FindActive(context.Context, int64) (auth.Administrator, error) {
	return stub.administrator, stub.err
}

type adminUserManagerStub struct {
	users       []domain.UserOverview
	statistics  domain.UserStatistics
	actorID     int64
	revokedID   int64
	revokedMode domain.RevocationMode
}

func (stub *adminUserManagerStub) ListUsers(context.Context, int64, int) ([]domain.UserOverview, error) {
	return stub.users, nil
}
func (stub *adminUserManagerStub) Statistics(context.Context) (domain.UserStatistics, error) {
	return stub.statistics, nil
}
func (stub *adminUserManagerStub) Revoke(_ context.Context, actorID, telegramID int64, mode domain.RevocationMode, _ time.Time) error {
	stub.actorID, stub.revokedID, stub.revokedMode = actorID, telegramID, mode
	return nil
}

type adminProvisionerStub struct {
	telegramID  int64
	resetPeriod bool
}

func (stub *adminProvisionerStub) Rotate(_ context.Context, telegramID int64, _ time.Time, resetPeriod bool) (domain.ProvisionedAccess, error) {
	stub.telegramID, stub.resetPeriod = telegramID, resetPeriod
	return domain.ProvisionedAccess{}, nil
}
