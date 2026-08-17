package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/config"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/coreworker"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/subscription"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/vpn"
)

func TestBuildApplicationConnectsTelegramLoginCodeToWebSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{cancel: cancel}
	membership := &applicationMembershipHandlerStub{}
	vpnAccess := &applicationVPNAccessStub{access: vpn.Access{SubscriptionURL: "https://vpn.example.com/sub/private", NewlyIssued: true}}
	vpnStatus := &applicationVPNStatusStub{status: vpn.Status{Overview: domain.UserOverview{TelegramID: 12345, Status: domain.AccessStatusActive}}}
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	randomSource := bytes.NewReader(make([]byte, 256))

	application, err := buildApplication(ctx, configuration, readinessStub{}, store, bot, randomSource, func() time.Time { return now }, vpnAccess, vpnStatus, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsStub{}, applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{}, membership)
	if err != nil {
		t.Fatalf("buildApplication() error = %v", err)
	}
	if err := application.bot.Run(ctx); err != nil {
		t.Fatalf("bot.Run() error = %v", err)
	}
	if len(bot.sent) != 2 || len(bot.sent[0]) != 8 || !strings.Contains(bot.sent[1], vpnAccess.access.SubscriptionURL) {
		t.Fatalf("Bot replies = %#v, want login code and private subscription", bot.sent)
	}
	if membership.calls != 1 || membership.telegramID != 12345 {
		t.Fatalf("membership handler calls=%d telegramID=%d", membership.calls, membership.telegramID)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"`+bot.sent[0]+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	application.handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%q", response.Code, response.Body.String())
	}
	if len(response.Result().Cookies()) != 2 || store.session.Digest == ([32]byte{}) {
		t.Fatalf("login did not persist a session and set two cookies: session=%#v cookies=%#v", store.session, response.Result().Cookies())
	}
}

func TestBuildApplicationRejectsMissingVPNAccessProvider(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}

	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, nil, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsStub{}, applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted a missing VPN access provider")
	}
}

func TestBuildApplicationRejectsMissingSubscriptionRenderer(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}

	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsStub{}, nil,
		applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted a missing subscription renderer")
	}
}

func TestBuildApplicationRejectsMissingVPNStatusProvider(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, nil, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted a missing VPN status provider")
	}
}

func TestBuildApplicationRejectsMissingAdministratorCommands(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, nil, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing administrator commands")
	}
}

func TestBuildApplicationRejectsMissingAdministratorManagement(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, nil, applicationAuditStub{}, applicationManagementSettingsStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing administrator management")
	}
}

func TestBuildApplicationRejectsMissingAuditReader(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, nil, applicationManagementSettingsStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing audit reader")
	}
}

func TestBuildApplicationRejectsMissingManagementSettings(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, nil,
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing management settings")
	}
}

func TestBuildApplicationRejectsMissingCoreSettingsManagement(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsWithoutCoreStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing core settings management")
	}
}

func TestBuildApplicationRejectsMissingTLSSettingsManagement(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsWithoutTLSStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil {
		t.Fatal("buildApplication() accepted missing TLS settings management")
	}
}

type applicationManagementSettingsWithoutTLSStub struct {
	applicationManagementSettingsWithoutCoreStub
}

func TestBuildApplicationRejectsMissingRealitySearch(t *testing.T) {
	configuration := config.Config{MasterKey: bytes.Repeat([]byte{7}, 32)}
	store := &applicationAuthStoreStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	bot := &applicationBotClientStub{}
	if _, err := buildApplication(
		context.Background(), configuration, readinessStub{}, store, bot,
		bytes.NewReader(make([]byte, 256)), time.Now, &applicationVPNAccessStub{}, &applicationVPNStatusStub{}, &applicationAdminCommandsStub{}, &applicationAdministratorManagementStub{}, applicationAuditStub{}, applicationManagementSettingsWithoutRealitySearchStub{},
		applicationSubscriptionStub{}, applicationUserManagementStub{}, applicationProvisioningManagementStub{}, applicationApprovalRequestStub{}, applicationCallbackHandlerStub{},
	); err == nil || !strings.Contains(err.Error(), "reality search") {
		t.Fatalf("buildApplication() error = %v, want reality search requirement", err)
	}
}

type applicationManagementSettingsWithoutRealitySearchStub struct {
	applicationManagementSettingsWithoutTLSStub
}

func (applicationManagementSettingsWithoutRealitySearchStub) GetOverview(context.Context) (domain.TLSSettingsOverview, error) {
	return domain.TLSSettingsOverview{}, nil
}

func (applicationManagementSettingsWithoutRealitySearchStub) Save(context.Context, int64, domain.TLSSettingsUpdate, time.Time) error {
	return nil
}

func (applicationManagementSettingsWithoutTLSStub) GetCore(context.Context) (domain.CoreSettingsOverview, error) {
	return domain.CoreSettingsOverview{}, nil
}

func (applicationManagementSettingsWithoutTLSStub) UpdateCore(context.Context, int64, domain.CoreSettingsUpdate, time.Time) error {
	return nil
}

type applicationAuditStub struct{}

func (applicationAuditStub) List(context.Context, int64, int) ([]domain.AuditEvent, error) {
	return nil, nil
}

type applicationManagementSettingsStub struct{}

func (applicationManagementSettingsStub) Get(context.Context) (domain.ManagementSettings, []domain.QualificationRuleOverview, error) {
	return domain.ManagementSettings{QualificationMode: domain.QualificationAny, RecheckIntervalMinutes: 60, RecheckRequestsPerSecond: 10, RecheckBatchSize: 50, QuotaLimitBytes: 50_000_000_000}, nil, nil
}
func (applicationManagementSettingsStub) PreviewInactivity(context.Context, int, time.Time) (int64, error) {
	return 0, nil
}
func (applicationManagementSettingsStub) Update(context.Context, int64, domain.ManagementSettings, bool, time.Time) error {
	return nil
}

func (applicationManagementSettingsStub) EnableByActor(context.Context, int64, qualification.ManagedRule) error {
	return nil
}

func (applicationManagementSettingsStub) DisableByActor(context.Context, int64, int64) error {
	return nil
}

func (applicationManagementSettingsStub) GetCore(context.Context) (domain.CoreSettingsOverview, error) {
	return domain.CoreSettingsOverview{}, nil
}

func (applicationManagementSettingsStub) UpdateCore(context.Context, int64, domain.CoreSettingsUpdate, time.Time) error {
	return nil
}

func (applicationManagementSettingsStub) GetOverview(context.Context) (domain.TLSSettingsOverview, error) {
	return domain.TLSSettingsOverview{}, nil
}

func (applicationManagementSettingsStub) Save(context.Context, int64, domain.TLSSettingsUpdate, time.Time) error {
	return nil
}

func (applicationManagementSettingsStub) Start(context.Context) error {
	return nil
}

func (applicationManagementSettingsStub) Snapshot() reality.SearchSnapshot {
	return reality.SearchSnapshot{Status: reality.SearchStatusIdle}
}

type applicationManagementSettingsWithoutCoreStub struct {
}

func (applicationManagementSettingsWithoutCoreStub) Get(context.Context) (domain.ManagementSettings, []domain.QualificationRuleOverview, error) {
	return domain.ManagementSettings{QualificationMode: domain.QualificationAny, RecheckIntervalMinutes: 60, RecheckRequestsPerSecond: 10, RecheckBatchSize: 50, QuotaLimitBytes: 50_000_000_000}, nil, nil
}
func (applicationManagementSettingsWithoutCoreStub) PreviewInactivity(context.Context, int, time.Time) (int64, error) {
	return 0, nil
}
func (applicationManagementSettingsWithoutCoreStub) Update(context.Context, int64, domain.ManagementSettings, bool, time.Time) error {
	return nil
}
func (applicationManagementSettingsWithoutCoreStub) EnableByActor(context.Context, int64, qualification.ManagedRule) error {
	return nil
}
func (applicationManagementSettingsWithoutCoreStub) DisableByActor(context.Context, int64, int64) error {
	return nil
}

type applicationSubscriptionStub struct{}

func (applicationSubscriptionStub) Render(context.Context, string, subscription.Format) ([]byte, error) {
	return []byte("subscription"), nil
}

type applicationUserManagementStub struct{}

func (applicationUserManagementStub) ListUsers(context.Context, int64, int) ([]domain.UserOverview, error) {
	return nil, nil
}
func (applicationUserManagementStub) Revoke(context.Context, int64, int64, domain.RevocationMode, time.Time) error {
	return nil
}
func (applicationUserManagementStub) RejectApproval(context.Context, int64, int64, time.Time) error {
	return nil
}

type applicationProvisioningManagementStub struct{}

func (applicationProvisioningManagementStub) Approve(context.Context, int64, time.Time) (domain.ProvisionedAccess, error) {
	return domain.ProvisionedAccess{}, nil
}
func (applicationProvisioningManagementStub) Rotate(context.Context, int64, time.Time, bool) (domain.ProvisionedAccess, error) {
	return domain.ProvisionedAccess{}, nil
}

type applicationCallbackHandlerStub struct{}

type applicationApprovalRequestStub struct{}

func (applicationApprovalRequestStub) NotifyApprovalRequired(context.Context, int64) error {
	return nil
}

func (applicationCallbackHandlerStub) HandleCallback(context.Context, telegram.Callback) error {
	return nil
}

func TestBuildRecheckSchedulerConnectsSettingsUsersTelegramAndAdministratorNotifications(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &applicationRecheckStoreStub{}
	bot := &applicationRecheckBotStub{}
	wait := func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	scheduler, err := buildRecheckScheduler(recheckRuntimeDependencies{
		settings:     store,
		users:        store,
		rules:        store,
		members:      bot,
		writer:       store,
		recipients:   store,
		sender:       bot,
		retryWait:    func(context.Context, time.Duration) error { return nil },
		now:          time.Now,
		jitter:       func(time.Duration) time.Duration { return 0 },
		scheduleWait: wait,
	})
	if err != nil {
		t.Fatalf("buildRecheckScheduler() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("scheduler.Run() error = %v", err)
	}
	if store.applied != 1 || len(bot.messages) != 1 || !strings.Contains(bot.messages[0], "資格補償重查完成") {
		t.Fatalf("applied=%d messages=%#v", store.applied, bot.messages)
	}
}

func TestBuildCoreWorkerConnectsActionSnapshotInstallAndCompletion(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	store := &applicationCoreActionStoreStub{actions: []postgres.CoreAction{{
		ID: 11, TelegramID: 12345, Action: postgres.CoreActionRevoke, Attempts: 1,
	}}}
	snapshot := &applicationCoreSnapshotStub{configuration: []byte(`{"inbounds":[]}`)}
	installer := &applicationCoreInstallerStub{}
	notifier := &applicationCoreNotifierStub{}

	worker, err := buildCoreWorker(coreRuntimeDependencies{
		store: store, snapshot: snapshot, installer: installer, notifier: notifier,
	})
	if err != nil {
		t.Fatalf("buildCoreWorker() error = %v", err)
	}
	if err := worker.Step(context.Background(), now); err != nil {
		t.Fatalf("worker.Step() error = %v", err)
	}
	if snapshot.calls != 1 || installer.calls != 1 || !reflect.DeepEqual(store.completed, []int64{11}) {
		t.Fatalf("snapshot=%d install=%d completed=%v", snapshot.calls, installer.calls, store.completed)
	}
}

func TestBuildCoreWorkerRejectsMissingDependency(t *testing.T) {
	if _, err := buildCoreWorker(coreRuntimeDependencies{}); err == nil {
		t.Fatal("buildCoreWorker() accepted missing dependencies")
	}
}

type readinessStub struct{}

func (readinessStub) Ping(context.Context) error { return nil }

type applicationAuthStoreStub struct {
	administrator auth.Administrator
	loginCode     auth.LoginCodeRecord
	session       auth.SessionRecord
}

func (store *applicationAuthStoreStub) FindActive(context.Context, int64) (auth.Administrator, error) {
	return store.administrator, nil
}

func (store *applicationAuthStoreStub) Replace(_ context.Context, record auth.LoginCodeRecord) error {
	store.loginCode = record
	return nil
}

func (store *applicationAuthStoreStub) Consume(_ context.Context, telegramID int64, digest [32]byte, now time.Time) (auth.Administrator, error) {
	if telegramID != store.loginCode.TelegramID || digest != store.loginCode.Digest || !now.Before(store.loginCode.ExpiresAt) {
		return auth.Administrator{}, auth.ErrLoginCodeInvalid
	}
	administrator := store.loginCode.Administrator
	store.loginCode = auth.LoginCodeRecord{}
	return administrator, nil
}

func (store *applicationAuthStoreStub) Create(_ context.Context, record auth.SessionRecord) error {
	store.session = record
	return nil
}

func (store *applicationAuthStoreStub) AuthenticateAndTouch(context.Context, [32]byte, time.Time, time.Duration) (auth.Administrator, error) {
	return auth.Administrator{}, auth.ErrSessionInvalid
}

func (store *applicationAuthStoreStub) Delete(context.Context, [32]byte) error { return nil }

func (store *applicationAuthStoreStub) DeleteAll(context.Context, int64) error { return nil }

type applicationBotClientStub struct {
	cancel context.CancelFunc
	calls  int
	sent   []string
}

func (client *applicationBotClientStub) GetMe(context.Context) (telegram.User, error) {
	return telegram.User{ID: 1, IsBot: true, Username: "vpn_example_bot"}, nil
}

func (client *applicationBotClientStub) GetUpdates(ctx context.Context, _ int64) ([]telegram.Update, error) {
	client.calls++
	if client.calls == 1 {
		return []telegram.Update{
			{
				UpdateID: 1,
				Message:  &telegram.APIMessage{Chat: telegram.Chat{ID: 9, Type: telegram.ChatPrivate}, From: &telegram.User{ID: 12345}, Text: "/adminlogin@vpn_example_bot"},
			},
			{
				UpdateID: 2,
				ChatMember: &telegram.ChatMemberUpdated{
					Chat:          telegram.Chat{ID: -1001, Type: telegram.ChatSupergroup},
					NewChatMember: telegram.ChatMember{User: telegram.User{ID: 12345}, Status: "left"},
				},
			},
			{
				UpdateID: 3,
				Message:  &telegram.APIMessage{Chat: telegram.Chat{ID: 9, Type: telegram.ChatPrivate}, From: &telegram.User{ID: 12345}, Text: "/vpn"},
			},
		}, nil
	}
	client.cancel()
	return nil, ctx.Err()
}

func (client *applicationBotClientStub) SendMessage(_ context.Context, _ int64, text string) error {
	client.sent = append(client.sent, text)
	return nil
}

type applicationMembershipHandlerStub struct {
	calls      int
	telegramID int64
}

type applicationVPNAccessStub struct {
	access vpn.Access
	calls  int
}

type applicationVPNStatusStub struct{ status vpn.Status }

func (stub *applicationVPNStatusStub) GetStatus(context.Context, int64) (vpn.Status, error) {
	return stub.status, nil
}

type applicationAdminCommandsStub struct{}

func (*applicationAdminCommandsStub) Execute(context.Context, int64, string) (string, error) {
	return "admin result", nil
}

type applicationAdministratorManagementStub struct{}

func (*applicationAdministratorManagementStub) List(context.Context) ([]auth.Administrator, error) {
	return nil, nil
}

func (*applicationAdministratorManagementStub) SetRole(context.Context, int64, int64, auth.Role, time.Time) error {
	return nil
}

func (*applicationAdministratorManagementStub) Remove(context.Context, int64, int64, time.Time) error {
	return nil
}

func (stub *applicationVPNAccessStub) GetOrClaim(context.Context, int64) (vpn.Access, error) {
	stub.calls++
	return stub.access, nil
}

func (handler *applicationMembershipHandlerStub) HandleMembership(_ context.Context, event telegram.MembershipEvent) error {
	handler.calls++
	handler.telegramID = event.TelegramID
	return nil
}

type applicationRecheckStoreStub struct {
	applied int
}

func (*applicationRecheckStoreStub) RecheckSettings(context.Context) (qualification.RecheckSettings, error) {
	return qualification.RecheckSettings{Interval: time.Hour, RequestsPerSecond: 10, BatchSize: 50}, nil
}

func (*applicationRecheckStoreStub) KnownUsersAfter(_ context.Context, after int64, _ int) ([]int64, error) {
	if after == 0 {
		return []int64{12345}, nil
	}
	return nil, nil
}

func (*applicationRecheckStoreStub) ActiveRules(context.Context) (domain.QualificationMode, []qualification.Rule, error) {
	return domain.QualificationAny, []qualification.Rule{{ChatID: -1001}}, nil
}

func (store *applicationRecheckStoreStub) ApplyQualification(context.Context, int64, domain.QualificationDecision) (domain.AccessChange, error) {
	store.applied++
	return domain.AccessChange{}, nil
}

func (*applicationRecheckStoreStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	return []int64{999}, nil
}

type applicationRecheckBotStub struct {
	messages []string
}

type applicationCoreActionStoreStub struct {
	actions   []postgres.CoreAction
	completed []int64
}

func (stub *applicationCoreActionStoreStub) ClaimDue(context.Context, time.Time, time.Duration, int) ([]postgres.CoreAction, error) {
	actions := stub.actions
	stub.actions = nil
	return actions, nil
}

func (stub *applicationCoreActionStoreStub) Complete(_ context.Context, ids []int64, _ time.Time) error {
	stub.completed = append([]int64(nil), ids...)
	return nil
}

func (*applicationCoreActionStoreStub) Retry(context.Context, []int64, time.Time, postgres.CoreFailureCode) error {
	return errors.New("unexpected retry")
}

type applicationCoreSnapshotStub struct {
	configuration []byte
	calls         int
}

func (stub *applicationCoreSnapshotStub) Build(context.Context) ([]byte, error) {
	stub.calls++
	return append([]byte(nil), stub.configuration...), nil
}

type applicationCoreInstallerStub struct{ calls int }

func (stub *applicationCoreInstallerStub) Install(context.Context, []byte) error {
	stub.calls++
	return nil
}

type applicationCoreNotifierStub struct{}

func (*applicationCoreNotifierStub) NotifyPlannedRestart(context.Context) error { return nil }

func (*applicationCoreNotifierStub) NotifyCoreFailure(context.Context, postgres.CoreFailureCode) error {
	return nil
}

var _ coreworker.ActionStore = (*applicationCoreActionStoreStub)(nil)

func (*applicationRecheckBotStub) GetChatMember(_ context.Context, _ int64, userID int64) (telegram.ChatMember, error) {
	return telegram.ChatMember{User: telegram.User{ID: userID}, Status: "member"}, nil
}

func (bot *applicationRecheckBotStub) SendMessage(_ context.Context, _ int64, text string) error {
	bot.messages = append(bot.messages, text)
	return nil
}
