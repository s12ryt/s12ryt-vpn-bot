package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

func TestProvisioningStoreClaimsAndPersistsAllArtifactsAtomically(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	bundle := validProvisioningBundle()
	issuer := &bundleIssuerStub{bundle: bundle}
	sealer := &credentialSealerStub{sealed: secrets.SealedCredentialBundle{
		SubscriptionTokenDigest: [32]byte{1, 2, 3},
		Nonce:                   []byte("123456789012"),
		Ciphertext:              []byte("authenticated ciphertext payload"),
	}}
	transaction := &provisioningTransactionStub{rows: []pgx.Row{
		&accessRowStub{values: []any{int64(12345), true, string(domain.AccessStatusUnclaimed), int64(0), sql.NullTime{}, sql.NullTime{}}},
		&provisioningSettingsRow{limit: 50_000_000_000, periodSeconds: 2_592_000},
	}}
	runner := &provisioningTransactionRunnerStub{transaction: transaction}
	store := NewProvisioningStore(runner, issuer, sealer)

	provisioned, err := store.Claim(context.Background(), 12345, now)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if provisioned.Credentials != bundle || provisioned.Issuance.CredentialGeneration != 1 || !provisioned.Issuance.PeriodStartedAt.Equal(now) {
		t.Fatalf("Claim() = %#v", provisioned)
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if sealer.telegramID != 12345 || sealer.generation != 1 || sealer.bundle != bundle {
		t.Fatalf("Seal() inputs = id %d generation %d bundle %#v", sealer.telegramID, sealer.generation, sealer.bundle)
	}
	joined := strings.Join(transaction.execSQL, "\n")
	for _, required := range []string{
		"UPDATE vpn_users",
		"INSERT INTO credential_bundles",
		"INSERT INTO quota_windows",
		"INSERT INTO core_action_outbox",
		"'reconcile'",
		"INSERT INTO audit_events",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("transaction SQL does not contain %q:\n%s", required, joined)
		}
	}
	if containsProvisioningPlaintext(transaction.execArgs, bundle) {
		t.Fatal("database arguments contain plaintext credentials")
	}
}

func TestProvisioningStoreRollsBackWithoutReturningSecretsWhenSealingFails(t *testing.T) {
	wantErr := errors.New("random source failed")
	issuer := &bundleIssuerStub{bundle: validProvisioningBundle()}
	sealer := &credentialSealerStub{err: wantErr}
	transaction := &provisioningTransactionStub{rows: []pgx.Row{
		&accessRowStub{values: []any{int64(12345), true, string(domain.AccessStatusUnclaimed), int64(0), sql.NullTime{}, sql.NullTime{}}},
	}}
	runner := &provisioningTransactionRunnerStub{transaction: transaction}
	store := NewProvisioningStore(runner, issuer, sealer)

	provisioned, err := store.Claim(context.Background(), 12345, time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Claim() error = %v, want sealing error", err)
	}
	if provisioned != (domain.ProvisionedAccess{}) {
		t.Fatalf("failed Claim() returned secrets: %#v", provisioned)
	}
	if runner.committed || !runner.rolledBack || len(transaction.execSQL) != 0 {
		t.Fatalf("failed transaction committed=%v rolledBack=%v writes=%d", runner.committed, runner.rolledBack, len(transaction.execSQL))
	}
}

func TestProvisioningStoreDoesNotOpenTransactionWhenCredentialIssuanceFails(t *testing.T) {
	wantErr := errors.New("entropy unavailable")
	issuer := &bundleIssuerStub{err: wantErr}
	runner := &provisioningTransactionRunnerStub{}
	store := NewProvisioningStore(runner, issuer, &credentialSealerStub{})

	provisioned, err := store.Claim(context.Background(), 12345, time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Claim() error = %v, want issuance error", err)
	}
	if provisioned != (domain.ProvisionedAccess{}) || runner.called {
		t.Fatalf("failed Claim() = %#v, transaction called=%v", provisioned, runner.called)
	}
}

func TestProvisioningStoreRejectsInvalidInputBeforeIssuance(t *testing.T) {
	for _, testCase := range []struct {
		telegramID int64
		now        time.Time
	}{
		{telegramID: 0, now: time.Now()},
		{telegramID: 12345, now: time.Time{}},
	} {
		issuer := &bundleIssuerStub{bundle: validProvisioningBundle()}
		runner := &provisioningTransactionRunnerStub{}
		store := NewProvisioningStore(runner, issuer, &credentialSealerStub{})
		if _, err := store.Claim(context.Background(), testCase.telegramID, testCase.now); err == nil {
			t.Fatalf("Claim(%d, %v) error = nil", testCase.telegramID, testCase.now)
		}
		if issuer.calls != 0 || runner.called {
			t.Fatalf("invalid Claim() issued=%d transaction=%v", issuer.calls, runner.called)
		}
	}
}

func TestProvisioningStoreApprovesPendingAccessWithNewQuotaPeriod(t *testing.T) {
	previousPeriod := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	now := previousPeriod.Add(40 * 24 * time.Hour)
	transaction := &provisioningTransactionStub{rows: []pgx.Row{
		&accessRowStub{values: []any{
			int64(12345), true, string(domain.AccessStatusPendingApproval), int64(1),
			sql.NullTime{Time: previousPeriod, Valid: true}, sql.NullTime{Time: previousPeriod, Valid: true},
		}},
		&provisioningSettingsRow{limit: 50_000_000_000, periodSeconds: 2_592_000},
	}}
	store := NewProvisioningStore(
		&provisioningTransactionRunnerStub{transaction: transaction},
		&bundleIssuerStub{bundle: validProvisioningBundle()},
		&credentialSealerStub{sealed: validSealedProvisioningBundle()},
	)

	provisioned, err := store.Approve(context.Background(), 12345, now)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if provisioned.Issuance.CredentialGeneration != 2 || !provisioned.Issuance.PeriodStartedAt.Equal(now) {
		t.Fatalf("Approve() issuance = %#v", provisioned.Issuance)
	}
	if !strings.Contains(strings.Join(transaction.execSQL, "\n"), "INSERT INTO quota_windows") {
		t.Fatal("Approve() did not reset quota window")
	}
}

func TestProvisioningStoreRotationCanPreserveCurrentQuotaWindow(t *testing.T) {
	periodStartedAt := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	now := periodStartedAt.Add(10 * 24 * time.Hour)
	sealer := &credentialSealerStub{sealed: validSealedProvisioningBundle()}
	transaction := &provisioningTransactionStub{rows: []pgx.Row{
		&accessRowStub{values: []any{
			int64(12345), true, string(domain.AccessStatusActive), int64(3),
			sql.NullTime{Time: periodStartedAt, Valid: true}, sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		}},
	}}
	store := NewProvisioningStore(
		&provisioningTransactionRunnerStub{transaction: transaction},
		&bundleIssuerStub{bundle: validProvisioningBundle()},
		sealer,
	)

	provisioned, err := store.Rotate(context.Background(), 12345, now, false)
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if provisioned.Issuance.CredentialGeneration != 4 || !provisioned.Issuance.PeriodStartedAt.Equal(periodStartedAt) {
		t.Fatalf("Rotate() issuance = %#v", provisioned.Issuance)
	}
	if sealer.generation != 4 {
		t.Fatalf("Seal() generation = %d, want 4", sealer.generation)
	}
	joined := strings.Join(transaction.execSQL, "\n")
	if strings.Contains(joined, "INSERT INTO quota_windows") {
		t.Fatal("preserving rotation rewrote quota window")
	}
	if len(transaction.querySQL) != 1 {
		t.Fatalf("preserving rotation queries = %d, want only account lock", len(transaction.querySQL))
	}
}

type bundleIssuerStub struct {
	bundle domain.CredentialBundle
	err    error
	calls  int
}

func (stub *bundleIssuerStub) Issue() (domain.CredentialBundle, error) {
	stub.calls++
	return stub.bundle, stub.err
}

type credentialSealerStub struct {
	sealed     secrets.SealedCredentialBundle
	err        error
	telegramID int64
	generation uint64
	bundle     domain.CredentialBundle
}

func (stub *credentialSealerStub) Seal(telegramID int64, generation uint64, bundle domain.CredentialBundle) (secrets.SealedCredentialBundle, error) {
	stub.telegramID = telegramID
	stub.generation = generation
	stub.bundle = bundle
	return stub.sealed, stub.err
}

type provisioningTransactionRunnerStub struct {
	transaction *provisioningTransactionStub
	called      bool
	committed   bool
	rolledBack  bool
}

func (stub *provisioningTransactionRunnerStub) RunInTransaction(ctx context.Context, operation func(Database) error) error {
	stub.called = true
	err := operation(stub.transaction)
	if err != nil {
		stub.rolledBack = true
		return err
	}
	stub.committed = true
	return nil
}

type provisioningTransactionStub struct {
	rows      []pgx.Row
	querySQL  []string
	execSQL   []string
	execArgs  [][]any
	execErrAt int
	execErr   error
}

func (stub *provisioningTransactionStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stub.querySQL = append(stub.querySQL, query)
	index := len(stub.querySQL) - 1
	if index >= len(stub.rows) {
		return &accessRowStub{err: errors.New("unexpected query")}
	}
	return stub.rows[index]
}

func (stub *provisioningTransactionStub) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	stub.execSQL = append(stub.execSQL, query)
	stub.execArgs = append(stub.execArgs, arguments)
	if stub.execErr != nil && len(stub.execSQL)-1 == stub.execErrAt {
		return pgconn.CommandTag{}, stub.execErr
	}
	return pgconn.CommandTag{}, nil
}

type provisioningSettingsRow struct {
	limit         int64
	periodSeconds int64
}

func (row *provisioningSettingsRow) Scan(destinations ...any) error {
	if len(destinations) != 2 {
		return errors.New("unexpected settings destination count")
	}
	*destinations[0].(*int64) = row.limit
	*destinations[1].(*int64) = row.periodSeconds
	return nil
}

func validProvisioningBundle() domain.CredentialBundle {
	return domain.CredentialBundle{
		SubscriptionToken: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		VLESSUUID:         "11111111-1111-4111-8111-111111111111",
		Hysteria2Password: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		TUICUUID:          "22222222-2222-4222-8222-222222222222",
		TUICPassword:      "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
		AnyTLSPassword:    "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
	}
}

func validSealedProvisioningBundle() secrets.SealedCredentialBundle {
	return secrets.SealedCredentialBundle{
		SubscriptionTokenDigest: [32]byte{1},
		Nonce:                   []byte("123456789012"),
		Ciphertext:              []byte("authenticated ciphertext payload"),
	}
}

func containsProvisioningPlaintext(arguments [][]any, bundle domain.CredentialBundle) bool {
	plainValues := []string{bundle.SubscriptionToken, bundle.VLESSUUID, bundle.Hysteria2Password, bundle.TUICUUID, bundle.TUICPassword, bundle.AnyTLSPassword}
	for _, call := range arguments {
		for _, argument := range call {
			value, ok := argument.(string)
			if !ok {
				continue
			}
			for _, plaintext := range plainValues {
				if value == plaintext {
					return true
				}
			}
		}
	}
	return false
}
