package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/config"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/coreworker"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/httpapi"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficrunner"
)

type applicationAuthStore interface {
	auth.AdministratorLookup
	auth.LoginCodeStore
	auth.SessionStore
}

type applicationBotClient interface {
	telegram.UpdateClient
	GetMe(ctx context.Context) (telegram.User, error)
}

type applicationRuntime struct {
	handler http.Handler
	bot     *telegram.Runner
}

type recheckRuntimeDependencies struct {
	settings     qualification.RecheckSettingsProvider
	users        qualification.KnownUserProvider
	rules        qualification.RuleProvider
	members      qualification.MemberLookup
	writer       qualification.DecisionWriter
	recipients   qualification.AdministratorRecipientProvider
	sender       qualification.AdministratorMessageSender
	retryWait    qualification.RetryWaitFunc
	now          func() time.Time
	jitter       qualification.JitterFunc
	scheduleWait qualification.ScheduleWaitFunc
}

type coreRuntimeDependencies struct {
	store     coreworker.ActionStore
	snapshot  coreworker.SnapshotBuilder
	installer coreworker.Installer
	notifier  coreworker.Notifier
}

type coreSettingsLoader interface {
	Load(context.Context) (singbox.Settings, error)
}

type coreStatsAddressProvider struct{ settings coreSettingsLoader }

func (provider *coreStatsAddressProvider) StatsAddress(ctx context.Context) (string, error) {
	if provider == nil || provider.settings == nil {
		return "", errors.New("core settings provider is required")
	}
	settings, err := provider.settings.Load(ctx)
	if err != nil {
		return "", err
	}
	return settings.StatsListen, nil
}

type trafficHealthAdapter struct{ store *postgres.TrafficStore }

func (adapter *trafficHealthAdapter) ObserveFailure(ctx context.Context, stage trafficrunner.FailureStage, at time.Time) (trafficrunner.FaultObservation, error) {
	observed, err := adapter.store.ObserveFailure(ctx, string(stage), at)
	return trafficrunner.FaultObservation{StartedAt: observed.StartedAt, FailClosed: observed.FailClosed, Notify: observed.Notify}, err
}

func (adapter *trafficHealthAdapter) ObserveRecovery(ctx context.Context, at time.Time) (trafficrunner.FaultRecovery, error) {
	recovered, err := adapter.store.ObserveRecovery(ctx, at)
	return trafficrunner.FaultRecovery{Recovered: recovered.Recovered, WasFailClosed: recovered.WasFailClosed, StartedAt: recovered.StartedAt}, err
}

func buildCoreWorker(dependencies coreRuntimeDependencies) (*coreworker.Worker, error) {
	if dependencies.store == nil || dependencies.snapshot == nil || dependencies.installer == nil || dependencies.notifier == nil {
		return nil, errors.New("core worker dependencies are required")
	}
	return coreworker.New(dependencies.store, dependencies.snapshot, dependencies.installer, dependencies.notifier), nil
}

func buildRecheckScheduler(dependencies recheckRuntimeDependencies) (*qualification.RecheckScheduler, error) {
	notifier, err := qualification.NewAdministratorNotifier(dependencies.recipients, dependencies.sender)
	if err != nil {
		return nil, err
	}
	coordinator, err := qualification.NewRecheckCoordinator(
		dependencies.settings,
		dependencies.users,
		dependencies.rules,
		dependencies.members,
		dependencies.writer,
		notifier,
		dependencies.retryWait,
		dependencies.now,
		dependencies.jitter,
	)
	if err != nil {
		return nil, err
	}
	return qualification.NewRecheckScheduler(coordinator, dependencies.scheduleWait)
}

func buildApplication(
	ctx context.Context,
	configuration config.Config,
	readiness httpapi.ReadinessProbe,
	store applicationAuthStore,
	botClient applicationBotClient,
	randomSource io.Reader,
	now func() time.Time,
	vpnAccess telegram.VPNAccessProvider,
	vpnStatus telegram.VPNStatusProvider,
	adminCommands telegram.AdminCommandProvider,
	subscriptions httpapi.SubscriptionRenderer,
	users httpapi.UserManager,
	provisioning httpapi.UserProvisioningManager,
	approvalRequests telegram.ApprovalRequiredNotifier,
	callbacks telegram.CallbackHandler,
	membershipHandlers ...telegram.MembershipHandler,
) (applicationRuntime, error) {
	if vpnAccess == nil {
		return applicationRuntime{}, errors.New("VPN access provider is required")
	}
	if vpnStatus == nil {
		return applicationRuntime{}, errors.New("VPN status provider is required")
	}
	if adminCommands == nil {
		return applicationRuntime{}, errors.New("Telegram administrator commands are required")
	}
	if subscriptions == nil {
		return applicationRuntime{}, errors.New("subscription renderer is required")
	}
	if users == nil || provisioning == nil {
		return applicationRuntime{}, errors.New("user management dependencies are required")
	}
	if approvalRequests == nil {
		return applicationRuntime{}, errors.New("approval request notifier is required")
	}
	if callbacks == nil {
		return applicationRuntime{}, errors.New("Telegram callback handler is required")
	}
	loginCodeKey, err := config.DeriveKey(configuration.MasterKey, "admin-login-code")
	if err != nil {
		return applicationRuntime{}, err
	}
	sessionKey, err := config.DeriveKey(configuration.MasterKey, "admin-session")
	if err != nil {
		return applicationRuntime{}, err
	}
	identity, err := botClient.GetMe(ctx)
	if err != nil {
		return applicationRuntime{}, err
	}
	if identity.ID <= 0 || !identity.IsBot || strings.TrimSpace(identity.Username) == "" {
		return applicationRuntime{}, errors.New("Telegram bot identity is invalid")
	}
	loginCodes, err := auth.NewLoginCodeService(randomSource, loginCodeKey, store, store, now)
	if err != nil {
		return applicationRuntime{}, err
	}
	sessions, err := auth.NewSessionService(randomSource, sessionKey, store, now)
	if err != nil {
		return applicationRuntime{}, err
	}
	loginLimiter, err := httpapi.NewLoginRateLimiter(httpapi.DefaultLoginRateLimits(), now)
	if err != nil {
		return applicationRuntime{}, err
	}
	loginFlow := auth.NewLoginFlow(randomSource, loginCodes, sessions)
	commandHandler := telegram.NewCommandHandler(identity.Username, loginCodes, telegram.NewAdminLoginRateLimiter(now)).
		WithVPNAccess(vpnAccess).
		WithStatus(vpnStatus).
		WithAdminCommands(adminCommands).
		WithApprovalRequests(approvalRequests)
	return applicationRuntime{
		handler: httpapi.NewApplicationHandler(readiness, loginFlow, sessions, httpapi.LoginProtection{
			SourceIPs: httpapi.NewSourceIPResolver(configuration.TrustedProxyCIDRs),
			Limiter:   loginLimiter,
		}, subscriptions, users, provisioning),
		bot: telegram.NewRunner(botClient, commandHandler, nil, membershipHandlers...).WithCallbackHandler(callbacks),
	}, nil
}
