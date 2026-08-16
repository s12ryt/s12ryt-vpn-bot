package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/config"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/corecontrol"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/coreworker"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/httpapi"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/quotasweep"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/subscription"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficrunner"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/vpn"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(context.Background(), configuration.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()
	connection, err := pool.Acquire(startupContext)
	if err != nil {
		return err
	}
	if err := postgres.Migrate(startupContext, connection); err != nil {
		connection.Release()
		return err
	}
	connection.Release()

	authStore := postgres.NewAuthStore(pool)
	if err := authStore.EnsureRootOwner(startupContext, configuration.OwnerTelegramID); err != nil {
		return err
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	botClient := telegram.NewClient(configuration.BootstrapBotToken, "https://api.telegram.org", &http.Client{Timeout: 40 * time.Second})
	qualificationStore := postgres.NewQualificationStore(pool)
	transactionRunner := postgres.NewTransactionRunner(pool)
	accessStore := postgres.NewAccessStore(transactionRunner)
	memberLookup, err := qualification.NewRetryingMemberLookup(botClient, 10, nil, time.Now, nil)
	if err != nil {
		return err
	}
	qualificationChecker := qualification.NewChecker(qualificationStore, memberLookup)
	membershipHandler := qualification.NewMembershipHandler(qualificationChecker, accessStore)

	credentialEncryptionKey, err := config.DeriveKey(configuration.MasterKey, "credential-encryption")
	if err != nil {
		return err
	}
	credentialDigestKey, err := config.DeriveKey(configuration.MasterKey, "subscription-token-digest")
	if err != nil {
		return err
	}
	credentialCipher, err := secrets.NewCredentialCipher(credentialEncryptionKey, credentialDigestKey, nil)
	if err != nil {
		return err
	}
	linkBuilder, err := subscription.NewLinkBuilder(configuration.WebPublicURL)
	if err != nil {
		return err
	}
	provisioningStore := postgres.NewProvisioningStore(transactionRunner, domain.NewCredentialIssuer(nil), credentialCipher)
	credentialStore := postgres.NewCredentialStore(pool, credentialCipher)
	coreSettingsKey, err := config.DeriveKey(configuration.MasterKey, "core-settings-encryption")
	if err != nil {
		return err
	}
	coreSettingsCipher, err := secrets.NewValueCipher(coreSettingsKey, nil)
	if err != nil {
		return err
	}
	coreSettingsStore := postgres.NewCoreSettingsStore(pool, coreSettingsCipher)
	subscriptionService, err := subscription.NewService(credentialStore, coreSettingsStore, subscription.Renderer{})
	if err != nil {
		return err
	}
	vpnAccessService := vpn.NewAccessService(
		qualificationChecker,
		accessStore,
		provisioningStore,
		credentialStore,
		linkBuilder,
		time.Now,
	)
	userManagementStore := postgres.NewUserManagementStore(transactionRunner, pool)
	vpnStatusService := vpn.NewStatusService(userManagementStore, credentialStore, linkBuilder, 30*24*time.Hour)
	approvalHandler, err := telegram.NewApprovalHandler(authStore, provisioningStore, userManagementStore, botClient, time.Now)
	if err != nil {
		return err
	}
	approvalRequests, err := telegram.NewApprovalRequestNotifier(authStore, botClient)
	if err != nil {
		return err
	}
	application, err := buildApplication(
		signalContext,
		configuration,
		pool,
		authStore,
		botClient,
		nil,
		time.Now,
		vpnAccessService,
		vpnStatusService,
		subscriptionService,
		userManagementStore,
		provisioningStore,
		approvalRequests,
		approvalHandler,
		membershipHandler,
	)
	if err != nil {
		return err
	}
	recheckScheduler, err := buildRecheckScheduler(recheckRuntimeDependencies{
		settings:   qualificationStore,
		users:      qualificationStore,
		rules:      qualificationStore,
		members:    botClient,
		writer:     accessStore,
		recipients: authStore,
		sender:     botClient,
		now:        time.Now,
	})
	if err != nil {
		return err
	}
	coreSnapshot := singbox.NewSnapshotBuilder(coreSettingsStore, credentialStore, singbox.Generator{})
	coreController, err := corecontrol.NewUnixClient(configuration.CoreControlSocket)
	if err != nil {
		return err
	}
	coreDeployment, err := singbox.NewFileDeployment(configuration.SingBoxConfigPath, coreController)
	if err != nil {
		return err
	}
	coreNotifier, err := coreworker.NewTelegramNotifier(
		qualificationStore,
		authStore,
		botClient,
		postgres.NewAuditStore(pool),
		time.Now,
	)
	if err != nil {
		return err
	}
	coreWorker, err := buildCoreWorker(coreRuntimeDependencies{
		store:     postgres.NewCoreActionStore(pool),
		snapshot:  coreSnapshot,
		installer: singbox.NewInstaller(coreDeployment),
		notifier:  coreNotifier,
	})
	if err != nil {
		return err
	}
	dynamicCollector, err := trafficstats.NewDynamicCollector(&coreStatsAddressProvider{settings: coreSettingsStore}, trafficstats.DialRPC)
	if err != nil {
		return err
	}
	trafficSpool, err := trafficstats.NewFileSpool(configuration.TrafficSpoolPath)
	if err != nil {
		return err
	}
	trafficStore := postgres.NewTrafficStore(transactionRunner)
	trafficNotifier, err := trafficrunner.NewTelegramFaultNotifier(authStore, botClient, postgres.NewAuditStore(pool), time.Now)
	if err != nil {
		return err
	}
	trafficService := trafficrunner.NewService(
		trafficrunner.New(dynamicCollector, trafficSpool, trafficStore),
		&trafficHealthAdapter{store: trafficStore},
		trafficNotifier,
	)
	quotaScheduler, err := quotasweep.New(trafficStore, nil, time.Now)
	if err != nil {
		return err
	}

	webHandler, err := httpapi.NewSPAHandler(application.handler, os.DirFS(configuration.WebAssetDir))
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              configuration.ListenAddress(),
		Handler:           webHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)
	botError := make(chan error, 1)
	recheckError := make(chan error, 1)
	coreWorkerError := make(chan error, 1)
	trafficError := make(chan error, 1)
	quotaSweepError := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "address", server.Addr)
		serverError <- server.ListenAndServe()
	}()
	go func() {
		botError <- application.bot.Run(signalContext)
	}()
	go func() {
		recheckError <- recheckScheduler.Run(signalContext)
	}()
	go func() {
		coreWorkerError <- coreWorker.Run(signalContext, nil)
	}()
	go func() {
		trafficError <- trafficService.Run(signalContext, nil)
	}()
	go func() {
		quotaSweepError <- quotaScheduler.Run(signalContext)
	}()

	select {
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-botError:
		if err == nil {
			return errors.New("Telegram bot stopped unexpectedly")
		}
		return err
	case err := <-recheckError:
		if err == nil {
			return errors.New("qualification recheck scheduler stopped unexpectedly")
		}
		return err
	case err := <-coreWorkerError:
		if err == nil {
			return errors.New("core worker stopped unexpectedly")
		}
		return err
	case err := <-trafficError:
		if err == nil {
			return errors.New("traffic collector stopped unexpectedly")
		}
		return err
	case err := <-quotaSweepError:
		if err == nil {
			return errors.New("quota period scheduler stopped unexpectedly")
		}
		return err
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
