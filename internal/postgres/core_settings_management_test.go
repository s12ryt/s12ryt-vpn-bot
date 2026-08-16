package postgres

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

func TestCoreSettingsManagementStoreReadsWithoutReturningPrivateKey(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &coreSettingsManagementRowStub{values: []any{
		true, "203.0.113.10", "2001:db8::10", 443, 443, 8443, 8443,
		"vpn.example.com", "/run/tls/fullchain.pem", "/run/tls/privkey.pem", "www.example.com", 443,
		"0123456789abcdef", "127.0.0.1:10085", false, true,
	}}}
	store := NewCoreSettingsManagementStore(nil, database, nil)

	settings, err := store.GetCore(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !settings.Configured || !settings.HasRealityPrivateKey || settings.ListenIPv4 != "203.0.113.10" || settings.VLESSPort != 443 {
		t.Fatalf("Get() = %#v", settings)
	}
	if strings.Contains(strings.ToLower(database.query), "ciphertext") || strings.Contains(strings.ToLower(database.query), "private_key_nonce") {
		t.Fatalf("management query reads private key material: %q", database.query)
	}
}

func TestCoreSettingsManagementStoreUpdatesSecretAndQueuesReconcileAtomically(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{[]byte{}, []byte{}}}}
	runner := &transactionRunnerStub{transaction: transaction}
	cipher := &coreSettingsCipherStub{sealed: secrets.SealedValue{Nonce: []byte("123456789012"), Ciphertext: []byte("encrypted reality private key")}}
	store := NewCoreSettingsManagementStore(runner, nil, cipher)
	input := validCoreSettingsUpdate()

	if err := store.UpdateCore(context.Background(), 9001, input, now); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if cipher.sealPurpose != realityPrivateKeyPurpose || cipher.sealValue != input.RealityPrivateKey {
		t.Fatalf("Seal(%q, %q)", cipher.sealPurpose, cipher.sealValue)
	}
	if len(transaction.execSQL) != 3 || !strings.Contains(transaction.execSQL[0], "UPDATE core_settings") ||
		!strings.Contains(transaction.execSQL[1], "core_action_outbox") || !strings.Contains(transaction.execSQL[1], "reconcile") ||
		!strings.Contains(transaction.execSQL[2], "audit_events") {
		t.Fatalf("update statements = %#v", transaction.execSQL)
	}
	for _, arguments := range transaction.execArgs {
		for _, argument := range arguments {
			if text, ok := argument.(string); ok && text == input.RealityPrivateKey {
				t.Fatal("database arguments contain plaintext REALITY private key")
			}
		}
	}
}

func TestCoreSettingsManagementStorePreservesExistingSecretWhenUpdateOmitsIt(t *testing.T) {
	nonce := []byte("123456789012")
	ciphertext := []byte("encrypted reality private key")
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{nonce, ciphertext}}}
	cipher := &coreSettingsCipherStub{opened: validCoreSettingsUpdate().RealityPrivateKey}
	store := NewCoreSettingsManagementStore(&transactionRunnerStub{transaction: transaction}, nil, cipher)
	input := validCoreSettingsUpdate()
	input.RealityPrivateKey = ""

	if err := store.UpdateCore(context.Background(), 9001, input, time.Now().UTC()); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if cipher.sealCalled || cipher.openPurpose != realityPrivateKeyPurpose {
		t.Fatalf("sealCalled=%v openPurpose=%q", cipher.sealCalled, cipher.openPurpose)
	}
	if got := transaction.execArgs[0]; string(got[11].([]byte)) != string(nonce) || string(got[12].([]byte)) != string(ciphertext) {
		t.Fatalf("persisted secret = %#v", got[11:13])
	}
}

func TestCoreSettingsManagementStoreRejectsInvalidConfigurationBeforeWriting(t *testing.T) {
	input := validCoreSettingsUpdate()
	input.AnyTLSPort = input.VLESSPort
	runner := &transactionRunnerStub{}
	store := NewCoreSettingsManagementStore(runner, nil, &coreSettingsCipherStub{})
	if err := store.UpdateCore(context.Background(), 9001, input, time.Now().UTC()); err == nil || runner.called {
		t.Fatalf("Update() error=%v transaction=%v", err, runner.called)
	}
}

func validCoreSettingsUpdate() domain.CoreSettingsUpdate {
	privateKey := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	return domain.CoreSettingsUpdate{CoreSettingsOverview: domain.CoreSettingsOverview{
		Configured: true, ListenIPv4: "203.0.113.10", ListenIPv6: "2001:db8::10",
		VLESSPort: 443, Hysteria2Port: 443, TUICPort: 8443, AnyTLSPort: 8443,
		TLSServerName: "vpn.example.com", TLSCertificatePath: "/run/tls/fullchain.pem", TLSKeyPath: "/run/tls/privkey.pem",
		RealityServer: "www.example.com", RealityServerPort: 443, RealityShortID: "0123456789abcdef",
		StatsListen: "127.0.0.1:10085", AllowIPv4Outbound: false,
	}, RealityPrivateKey: privateKey}
}

type coreSettingsCipherStub struct {
	sealed      secrets.SealedValue
	opened      string
	sealPurpose string
	sealValue   string
	openPurpose string
	sealCalled  bool
}

func (stub *coreSettingsCipherStub) Seal(purpose, value string) (secrets.SealedValue, error) {
	stub.sealCalled, stub.sealPurpose, stub.sealValue = true, purpose, value
	return stub.sealed, nil
}

func (stub *coreSettingsCipherStub) Open(purpose string, _ secrets.SealedValue) (string, error) {
	stub.openPurpose = purpose
	return stub.opened, nil
}

type coreSettingsManagementRowStub struct{ values []any }

func (row *coreSettingsManagementRowStub) Scan(destinations ...any) error {
	if len(destinations) != len(row.values) {
		return context.Canceled
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *bool:
			*pointer = row.values[index].(bool)
		case *string:
			*pointer = row.values[index].(string)
		case *int:
			*pointer = row.values[index].(int)
		}
	}
	return nil
}
