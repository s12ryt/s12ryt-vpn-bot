package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type AdminUserManager interface {
	ListUsers(ctx context.Context, after int64, limit int) ([]domain.UserOverview, error)
	Statistics(ctx context.Context) (domain.UserStatistics, error)
	Revoke(ctx context.Context, actorID, telegramID int64, mode domain.RevocationMode, now time.Time) error
}

type AdminProvisioner interface {
	Rotate(ctx context.Context, telegramID int64, now time.Time, resetPeriod bool) (domain.ProvisionedAccess, error)
}

type AdminCommands struct {
	administrators auth.AdministratorLookup
	users          AdminUserManager
	provisioner    AdminProvisioner
	now            func() time.Time
}

func NewAdminCommands(administrators auth.AdministratorLookup, users AdminUserManager, provisioner AdminProvisioner, now func() time.Time) (*AdminCommands, error) {
	if administrators == nil || users == nil || provisioner == nil {
		return nil, errors.New("administrator command dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &AdminCommands{administrators: administrators, users: users, provisioner: provisioner, now: now}, nil
}

func (service *AdminCommands) Execute(ctx context.Context, actorID int64, command string) (string, error) {
	if service == nil || actorID <= 0 {
		return "", auth.ErrAdministratorUnauthorized
	}
	administrator, err := service.administrators.FindActive(ctx, actorID)
	if err != nil || administrator.TelegramID != actorID || !administrator.Active || !administrator.Role.Allows(auth.PermissionManageUsers) {
		return "", auth.ErrAdministratorUnauthorized
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", errors.New("administrator command is invalid")
	}
	switch fields[0] {
	case "/adminusers":
		if len(fields) != 1 {
			return "", errors.New("administrator user list command is invalid")
		}
		return service.listUsers(ctx)
	case "/adminstats":
		if len(fields) != 1 {
			return "", errors.New("administrator statistics command is invalid")
		}
		return service.statistics(ctx)
	case "/adminrevoke":
		return service.revoke(ctx, actorID, fields)
	case "/adminrotate":
		return service.rotate(ctx, fields)
	default:
		return "", errors.New("administrator command is invalid")
	}
}

func (service *AdminCommands) listUsers(ctx context.Context) (string, error) {
	users, err := service.users.ListUsers(ctx, 0, 20)
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "目前沒有已知 VPN 使用者。", nil
	}
	lines := []string{"VPN 使用者（前 20 筆）："}
	for _, user := range users {
		lines = append(lines, fmt.Sprintf("%d｜%s｜%.2f GB / %.2f GB", user.TelegramID, statusLabel(user.Status), float64(user.UsedBytes)/1_000_000_000, float64(user.LimitBytes)/1_000_000_000))
	}
	return strings.Join(lines, "\n"), nil
}

func (service *AdminCommands) statistics(ctx context.Context) (string, error) {
	statistics, err := service.users.Statistics(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("總使用者：%d\n使用中：%d\n待審：%d\n封鎖：%d\n總流量：%.2f GB", statistics.Total, statistics.Active, statistics.Pending, statistics.Blocked, float64(statistics.TotalUsedBytes)/1_000_000_000), nil
}

func (service *AdminCommands) revoke(ctx context.Context, actorID int64, fields []string) (string, error) {
	if len(fields) != 3 {
		return "", errors.New("administrator revoke command is invalid")
	}
	telegramID, err := parseCanonicalTelegramID(fields[1])
	if err != nil {
		return "", err
	}
	modes := map[string]domain.RevocationMode{
		"self": domain.RevocationModeSelfService, "approval": domain.RevocationModeRequireApproval, "block": domain.RevocationModePermanentBlock,
	}
	mode, ok := modes[fields[2]]
	if !ok {
		return "", errors.New("administrator revoke mode is invalid")
	}
	if err := service.users.Revoke(ctx, actorID, telegramID, mode, service.now()); err != nil {
		return "", err
	}
	description := map[domain.RevocationMode]string{
		domain.RevocationModeSelfService:     "可自行重新領取",
		domain.RevocationModeRequireApproval: "重新領取需核准",
		domain.RevocationModePermanentBlock:  "已永久封鎖",
	}[mode]
	return fmt.Sprintf("已撤銷使用者 %d，%s。", telegramID, description), nil
}

func (service *AdminCommands) rotate(ctx context.Context, fields []string) (string, error) {
	if len(fields) != 3 {
		return "", errors.New("administrator rotate command is invalid")
	}
	telegramID, err := parseCanonicalTelegramID(fields[1])
	if err != nil {
		return "", err
	}
	if fields[2] != "keep" && fields[2] != "reset" {
		return "", errors.New("administrator rotate mode is invalid")
	}
	reset := fields[2] == "reset"
	if _, err := service.provisioner.Rotate(ctx, telegramID, service.now(), reset); err != nil {
		return "", err
	}
	description := "並保留目前週期。"
	if reset {
		description = "並重置週期。"
	}
	return fmt.Sprintf("已輪替使用者 %d 的憑證%s", telegramID, description), nil
}

func parseCanonicalTelegramID(value string) (int64, error) {
	telegramID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || telegramID <= 0 || strconv.FormatInt(telegramID, 10) != value {
		return 0, errors.New("Telegram ID is invalid")
	}
	return telegramID, nil
}
