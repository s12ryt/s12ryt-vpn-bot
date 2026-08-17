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

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/config"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/corecontrol"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/coreworker"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/httpapi"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/quotasweep"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
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
	transactionRunner := postgres.NewTransactionRunner(pool)
	botTokenKey, err := config.DeriveKey(configuration.MasterKey, "bot-token-encryption")
	if err != nil {
		return err
	}
	botTokenCipher, err := secrets.NewValueCipher(botTokenKey, nil)
	if err != nil {
		return err
	}
	botSettingsStore := postgres.NewBotSettingsStore(transactionRunner, pool, botTokenCipher)
	activeBotToken := configuration.BootstrapBotToken
	if storedToken, tokenErr := botSettingsStore.Token(startupContext); tokenErr == nil && storedToken != "" {
		activeBotToken = storedToken
	}
	botHTTPClient := &http.Client{Timeout: 40 * time.Second}
	const telegramAPIBase = "https://api.telegram.org"
	botClientFactory := func(token string) *telegram.Client {
		return telegram.NewClient(token, telegramAPIBase, botHTTPClient)
	}
	botClient := botClientFactory(activeBotToken)
	botIdentity, err := botClient.GetMe(startupContext)
	if err != nil {
		return err
	}
	swapAwareBotClient, err := telegram.NewSwapAwareClient(botClient, botIdentity)
	if err != nil {
		return err
	}
	botTokenManager, err := buildBotTokenManager(botTokenDependencies{
		wrapper: swapAwareBotClient,
		store:   botSettingsStore,
		factory: botClientFactory,
		verify: func(ctx context.Context, client *telegram.Client) (telegram.User, error) {
			return client.GetMe(ctx)
		},
	})
	if err != nil {
		return err
	}
	qualificationStore := postgres.NewQualificationStore(pool)
	accessStore := postgres.NewAccessStore(transactionRunner)
	memberLookup, err := qualification.NewRetryingMemberLookup(swapAwareBotClient, 10, nil, time.Now, nil)
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
	coreSettingsManagementStore := postgres.NewCoreSettingsManagementStore(transactionRunner, pool, coreSettingsCipher)
	tlsSettingsStore := postgres.NewTLSSettingsStore(transactionRunner, pool, coreSettingsCipher)
	subscriptionService, err := subscription.NewService(credentialStore, coreSettingsStore, subscription.Renderer{})
	if err != nil {
		return err
	}
	subscriptionService = subscriptionService.WithTLSReadiness(tlsSettingsStore)
	vpnAccessService := vpn.NewAccessService(
		qualificationChecker,
		accessStore,
		provisioningStore,
		credentialStore,
		linkBuilder,
		time.Now,
	)
	userManagementStore := postgres.NewUserManagementStore(transactionRunner, pool)
	administratorStore := postgres.NewAdministratorStore(transactionRunner, pool)
	auditStore := postgres.NewAuditStore(pool)
	vpnStatusService := vpn.NewStatusService(userManagementStore, credentialStore, linkBuilder, 30*24*time.Hour)
	adminCommands, err := telegram.NewAdminCommands(authStore, userManagementStore, provisioningStore, time.Now)
	if err != nil {
		return err
	}
	approvalHandler, err := telegram.NewApprovalHandler(authStore, provisioningStore, userManagementStore, swapAwareBotClient, time.Now)
	if err != nil {
		return err
	}
	approvalRequests, err := telegram.NewApprovalRequestNotifier(authStore, swapAwareBotClient)
	if err != nil {
		return err
	}
	recheckScheduler, err := buildRecheckScheduler(recheckRuntimeDependencies{
		settings:   qualificationStore,
		users:      qualificationStore,
		rules:      qualificationStore,
		members:    swapAwareBotClient,
		writer:     accessStore,
		recipients: authStore,
		sender:     swapAwareBotClient,
		now:        time.Now,
	})
	if err != nil {
		return err
	}
	managementSettingsStore := postgres.NewManagementSettingsStore(transactionRunner, pool, recheckScheduler)
	backupSettingsStore := postgres.NewBackupSettingsStore(transactionRunner, pool)
	qualificationRuleStore := postgres.NewQualificationRuleStore(transactionRunner)
	qualificationRuleManager := qualification.NewRuleManager(swapAwareBotClient.Identity().ID, swapAwareBotClient, qualificationRuleStore, time.Now, recheckScheduler)
	realityDataset, err := reality.NewEmbeddedDataset()
	if err != nil {
		return err
	}
	realityProber, err := reality.NewTLSProber(5*time.Second, nil)
	if err != nil {
		return err
	}
	realitySearchService := reality.NewService(reality.Options{
		Dataset:     realityDataset,
		Prober:      realityProber,
		SampleLimit: 200,
		Concurrency: 5,
		Budget:      60 * time.Second,
	})
	if realitySearchService == nil {
		return errors.New("reality search service options are invalid")
	}
	realityHealthStore := postgres.NewRealityHealthStore(transactionRunner, pool)
	realityHealthMonitor, err := buildRealityHealthMonitor(realityHealthRuntimeDependencies{
		targets:    realityHealthStore,
		prober:     realityProber,
		recorder:   realityHealthStore,
		recipients: authStore,
		sender:     swapAwareBotClient,
		audit:      auditStore,
		now:        time.Now,
	})
	if err != nil {
		return err
	}
	application, err := buildApplicationWithOptions(
		signalContext,
		configuration,
		pool,
		authStore,
		swapAwareBotClient,
		nil,
		time.Now,
		vpnAccessService,
		vpnStatusService,
		adminCommands,
		administratorStore,
		auditStore,
		applicationManagementSettings{ManagementSettingsManager: managementSettingsStore, QualificationRuleManager: qualificationRuleManager, CoreSettingsManager: coreSettingsManagementStore, TLSSettingsManager: tlsSettingsStore, RealitySearchManager: realitySearchService, BackupSettingsManager: backupSettingsStore},
		subscriptionService,
		userManagementStore,
		provisioningStore,
		approvalRequests,
		approvalHandler,
		&dashboardAdapter{users: userManagementStore, tls: tlsSettingsStore, core: coreSettingsManagementStore},
		botTokenManager,
		membershipHandler,
	)
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
		swapAwareBotClient,
		auditStore,
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
	trafficNotifier, err := trafficrunner.NewTelegramFaultNotifier(authStore, swapAwareBotClient, postgres.NewAuditStore(pool), time.Now)
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

	legoIssuer, err := acme.NewLegoIssuer()
	if err != nil {
		return err
	}
	tlsCoordinator, err := buildTLSRuntime(tlsRuntimeDependencies{
		core:     coreSettingsStore,
		settings: tlsSettingsStore,
		issuer:   legoIssuer,
		issuance: tlsSettingsStore,
		failures: tlsSettingsStore,
		now:      time.Now,
	})
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
	tlsError := make(chan error, 1)
	realityHealthError := make(chan error, 1)
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
	go func() {
		tlsError <- tlsCoordinator.Run(signalContext, nil)
	}()
	go func() {
		realityHealthError <- realityHealthMonitor.Run(signalContext, nil)
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
	case err := <-tlsError:
		if err == nil {
			return errors.New("TLS renewal coordinator stopped unexpectedly")
		}
		return err
	case err := <-realityHealthError:
		if err == nil {
			return errors.New("REALITY health monitor stopped unexpectedly")
		}
		return err
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
