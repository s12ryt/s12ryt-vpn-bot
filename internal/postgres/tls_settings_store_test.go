package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

func TestTLSSettingsStoreSavesEncryptedConfigurationAndAudits(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{[]byte{}, []byte{}}}}
	runner := &transactionRunnerStub{transaction: transaction}
	cipher := &coreSettingsCipherStub{sealed: secrets.SealedValue{Nonce: []byte("123456789012"), Ciphertext: []byte("encrypted duckdns token")}}
	store := NewTLSSettingsStore(runner, nil, cipher)

	err := store.Save(context.Background(), 9001, domain.TLSSettingsUpdate{
		Mode: "duckdns", Domain: "node.duckdns.org", Challenge: "dns_01", Email: "owner@example.com",
		CADirectoryURLs: []string{"https://acme-v02.api.letsencrypt.org/directory"}, TermsAccepted: true, DuckDNSToken: "duckdns-token",
	}, now)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if cipher.sealPurpose != duckDNSTokenPurpose || cipher.sealValue != "duckdns-token" {
		t.Fatalf("Seal(%q, %q)", cipher.sealPurpose, cipher.sealValue)
	}
	if len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[0], "UPDATE tls_settings") ||
		!strings.Contains(transaction.execSQL[1], "tls.settings.update") {
		t.Fatalf("statements = %#v", transaction.execSQL)
	}
	for _, arguments := range transaction.execArgs {
		for _, argument := range arguments {
			if text, ok := argument.(string); ok && text == "duckdns-token" {
				t.Fatal("database arguments contain plaintext DuckDNS token")
			}
		}
	}
}

func TestTLSSettingsStoreRejectsInvalidSettingsBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewTLSSettingsStore(runner, nil, &coreSettingsCipherStub{})
	invalid := []domain.TLSSettingsUpdate{
		{Mode: "duckdns", Domain: "node.duckdns.org", Challenge: "dns_01", CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true},
		{Mode: "duckdns", Domain: "node.duckdns.org", Challenge: "dns_01", CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true, DuckDNSToken: "token", Email: "not-an-email"},
		{Mode: "sslip_io", Domain: "node.duckdns.org", Challenge: "http_01", CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true},
		{Mode: "custom", Domain: "vpn.example.com", Challenge: "dns_01", CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: false},
		{Mode: "custom", Domain: "vpn.example.com", Challenge: "http_01", CADirectoryURLs: nil, TermsAccepted: true},
	}
	for _, update := range invalid {
		if err := store.Save(context.Background(), 9001, update, time.Now().UTC()); err == nil || runner.called {
			t.Fatalf("Save(%+v) error=%v transaction=%v", update, err, runner.called)
		}
	}
}

func TestTLSSettingsStoreGetOverviewNeverReadsTokenCiphertext(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &tlsOverviewRowStub{values: []any{
		true, "duckdns", "node.duckdns.org", "dns_01", "owner@example.com",
		[]string{"https://ca.example/directory"}, true, true, "issued",
		time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), "https://ca.example/directory",
	}}}
	store := NewTLSSettingsStore(nil, database, nil)

	overview, err := store.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if !overview.Configured || overview.Mode != "duckdns" || !overview.HasDuckDNSToken || overview.State != "issued" || !overview.CertificateExpiresAt.Equal(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("GetOverview() = %#v", overview)
	}
	if !strings.Contains(database.query, "duckdns_token_nonce IS NOT NULL") {
		t.Fatalf("overview query must derive token presence without reading material: %q", database.query)
	}
	if strings.Contains(database.query, "duckdns_token_ciphertext") {
		t.Fatalf("overview query must not reference token ciphertext: %q", database.query)
	}
}

func TestTLSSettingsStoreRecordsIssuanceAndQueuesReconcile(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	expires := now.Add(90 * 24 * time.Hour)
	transaction := &accessTransactionStub{}
	store := NewTLSSettingsStore(&transactionRunnerStub{transaction: transaction}, nil, nil)

	if err := store.RecordIssuance(context.Background(), "https://ca.example/directory", expires, now); err != nil {
		t.Fatalf("RecordIssuance() error = %v", err)
	}
	if len(transaction.execSQL) != 3 || !strings.Contains(transaction.execSQL[0], "state = 'issued'") ||
		!strings.Contains(transaction.execSQL[1], "core_action_outbox") ||
		!strings.Contains(transaction.execSQL[2], "tls.certificate.issued") {
		t.Fatalf("statements = %#v", transaction.execSQL)
	}
	if err := store.RecordIssuance(context.Background(), "https://ca.example/directory", now, now); err == nil {
		t.Fatal("expected expiry to be rejected")
	}
}

func TestTLSSettingsStoreRecordsFailureWithClosedReasons(t *testing.T) {
	now := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{}
	store := NewTLSSettingsStore(&transactionRunnerStub{transaction: transaction}, nil, nil)

	if err := store.RecordFailure(context.Background(), "all_cas_failed", now); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}
	if len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[0], "last_failure_reason") ||
		!strings.Contains(transaction.execSQL[1], "tls.certificate.failed") {
		t.Fatalf("statements = %#v", transaction.execSQL)
	}
	if err := store.RecordFailure(context.Background(), "certificate authority exploded with token xyz", now); err == nil {
		t.Fatal("expected arbitrary failure reason to be rejected")
	}
}

func TestTLSSettingsStoreLoadsForIssuanceAndDecryptsToken(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &tlsIssuanceRowStub{values: []any{
		true, "duckdns", "node.duckdns.org", "dns_01", "owner@example.com",
		[]string{"https://ca.example/directory"}, true,
		[]byte("123456789012"), []byte("encrypted duckdns token"), nil,
	}}}
	cipher := &coreSettingsCipherStub{opened: "duckdns-token"}
	store := NewTLSSettingsStore(nil, database, cipher)

	settings, expiresAt, err := store.LoadForIssuance(context.Background())
	if err != nil {
		t.Fatalf("LoadForIssuance() error = %v", err)
	}
	if !settings.TermsAccepted || settings.Domain != "node.duckdns.org" || settings.Challenge != "dns_01" ||
		settings.DNSProviderName != "duckdns" || settings.DNSProviderToken != "duckdns-token" {
		t.Fatalf("LoadForIssuance() settings = %#v", settings)
	}
	if !expiresAt.IsZero() {
		t.Fatalf("expiresAt = %v, want zero when none issued", expiresAt)
	}
	if cipher.openPurpose != duckDNSTokenPurpose {
		t.Fatalf("Open purpose = %q", cipher.openPurpose)
	}
}

func TestTLSSettingsStoreLoadForIssuanceFailsClosedWithoutToken(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &tlsIssuanceRowStub{values: []any{
		true, "duckdns", "node.duckdns.org", "dns_01", "",
		[]string{"https://ca.example/directory"}, true, nil, nil,
	}}}
	store := NewTLSSettingsStore(nil, database, &coreSettingsCipherStub{})
	if _, _, err := store.LoadForIssuance(context.Background()); err == nil {
		t.Fatal("expected failure when duckdns token is missing")
	}
}

type tlsIssuanceRowStub struct{ values []any }

func (row *tlsIssuanceRowStub) Scan(destinations ...any) error {
	if len(destinations) != len(row.values) {
		return context.Canceled
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *bool:
			*pointer = row.values[index].(bool)
		case *string:
			*pointer = row.values[index].(string)
		case *[]string:
			*pointer = row.values[index].([]string)
		case *[]byte:
			value := row.values[index]
			if value == nil {
				continue
			}
			*pointer = value.([]byte)
		case *sql.NullTime:
			*pointer = sql.NullTime{}
		}
	}
	return nil
}

func TestTLSSettingsStoreIssuedRequiresValidExpiry(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &boolRow{value: true}}
	store := NewTLSSettingsStore(nil, database, nil)

	issued, err := store.Issued(context.Background())
	if err != nil {
		t.Fatalf("Issued() error = %v", err)
	}
	if !issued {
		t.Fatal("Issued() = false, want true")
	}
	if !strings.Contains(database.query, "state = 'issued'") ||
		!strings.Contains(database.query, "certificate_expires_at > now()") ||
		strings.Contains(database.query, "ciphertext") {
		t.Fatalf("Issued() query = %q", database.query)
	}
}

type tlsOverviewRowStub struct{ values []any }

func (row *tlsOverviewRowStub) Scan(destinations ...any) error {
	if len(destinations) != len(row.values) {
		return context.Canceled
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *bool:
			*pointer = row.values[index].(bool)
		case *string:
			*pointer = row.values[index].(string)
		case *[]string:
			*pointer = row.values[index].([]string)
		case *sql.NullTime:
			value := row.values[index].(time.Time)
			*pointer = sql.NullTime{Valid: true, Time: value}
		}
	}
	return nil
}
