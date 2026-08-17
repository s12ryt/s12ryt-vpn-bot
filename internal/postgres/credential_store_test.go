package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestCredentialStoreLoadsOnlyActiveEligibleBundleByKeyedDigest(t *testing.T) {
	wantBundle := validProvisioningBundle()
	cryptor := &credentialCryptorStub{digest: [32]byte{9}, bundle: wantBundle}
	database := &credentialDatabaseStub{row: &credentialRowStub{
		telegramID: 12345,
		generation: 4,
		nonce:      []byte("123456789012"),
		ciphertext: []byte("authenticated ciphertext payload"),
	}}
	store := NewCredentialStore(database, cryptor)

	bundle, err := store.FindActiveBySubscriptionToken(context.Background(), strings.Repeat("A", 43))
	if err != nil {
		t.Fatalf("FindActiveBySubscriptionToken() error = %v", err)
	}
	if bundle != wantBundle {
		t.Fatalf("FindActiveBySubscriptionToken() = %#v, want %#v", bundle, wantBundle)
	}
	if !strings.Contains(database.query, "subscription_token_digest") ||
		!strings.Contains(database.query, "status = 'active'") ||
		!strings.Contains(database.query, "eligible = TRUE") {
		t.Fatalf("credential query does not enforce active eligibility: %q", database.query)
	}
	if len(database.arguments) != 1 || string(database.arguments[0].([]byte)) != string(cryptor.digest[:]) {
		t.Fatalf("credential query arguments = %#v", database.arguments)
	}
	if cryptor.openTelegramID != 12345 || cryptor.openGeneration != 4 {
		t.Fatalf("Open() owner = %d generation = %d", cryptor.openTelegramID, cryptor.openGeneration)
	}
}

func TestCredentialStoreNormalizesMissingOrRevokedToken(t *testing.T) {
	store := NewCredentialStore(
		&credentialDatabaseStub{row: &credentialRowStub{err: pgx.ErrNoRows}},
		&credentialCryptorStub{digest: [32]byte{1}},
	)

	_, err := store.FindActiveBySubscriptionToken(context.Background(), strings.Repeat("A", 43))
	if !errors.Is(err, ErrCredentialBundleNotFound) {
		t.Fatalf("FindActiveBySubscriptionToken() error = %v, want not found", err)
	}
}

func TestCredentialStoreRejectsMalformedTokenBeforeDatabase(t *testing.T) {
	wantErr := errors.New("invalid subscription token")
	database := &credentialDatabaseStub{}
	store := NewCredentialStore(database, &credentialCryptorStub{digestErr: wantErr})

	if _, err := store.FindActiveBySubscriptionToken(context.Background(), "bad"); !errors.Is(err, wantErr) {
		t.Fatalf("FindActiveBySubscriptionToken() error = %v, want digest error", err)
	}
	if database.query != "" {
		t.Fatal("malformed token reached database")
	}
}

func TestCredentialStoreLoadsActiveBundleByTelegramID(t *testing.T) {
	wantBundle := validProvisioningBundle()
	cryptor := &credentialCryptorStub{bundle: wantBundle}
	database := &credentialDatabaseStub{row: &credentialRowStub{
		telegramID: 12345,
		generation: 2,
		nonce:      []byte("123456789012"),
		ciphertext: []byte("authenticated ciphertext payload"),
	}}
	store := NewCredentialStore(database, cryptor)

	bundle, err := store.FindActiveByTelegramID(context.Background(), 12345)
	if err != nil {
		t.Fatalf("FindActiveByTelegramID() error = %v", err)
	}
	if bundle != wantBundle || len(database.arguments) != 1 || database.arguments[0] != int64(12345) {
		t.Fatalf("FindActiveByTelegramID() = %#v args=%#v", bundle, database.arguments)
	}
	if !strings.Contains(database.query, "status = 'active'") || !strings.Contains(database.query, "eligible = TRUE") {
		t.Fatalf("credential query does not enforce active eligibility: %q", database.query)
	}
}

func TestCredentialStoreRejectsInvalidTelegramIDBeforeDatabase(t *testing.T) {
	database := &credentialDatabaseStub{}
	store := NewCredentialStore(database, &credentialCryptorStub{})
	if _, err := store.FindActiveByTelegramID(context.Background(), 0); err == nil {
		t.Fatal("FindActiveByTelegramID(0) error = nil")
	}
	if database.query != "" {
		t.Fatal("invalid Telegram ID reached database")
	}
}

func TestCredentialStoreListsAllActiveEligibleBundlesInStableOrder(t *testing.T) {
	wantFirst := validProvisioningBundle()
	wantSecond := validProvisioningBundle()
	wantSecond.AnyTLSPassword = "second-anytls-password"
	cryptor := &credentialListCryptorStub{bundles: map[int64]domain.CredentialBundle{
		1001: wantFirst,
		2002: wantSecond,
	}}
	database := &credentialDatabaseStub{row: &credentialListRowStub{
		telegramIDs: []int64{1001, 2002},
		generations: []int64{2, 5},
		nonces:      [][]byte{[]byte("nonce-1001--"), []byte("nonce-2002--")},
		ciphertexts: [][]byte{[]byte("ciphertext-1001"), []byte("ciphertext-2002")},
	}}
	store := NewCredentialStore(database, cryptor)

	users, err := store.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive() error = %v", err)
	}
	if len(users) != 2 || users[0].TelegramID != 1001 || users[0].Credentials != wantFirst ||
		users[1].TelegramID != 2002 || users[1].Credentials != wantSecond {
		t.Fatalf("ListActive() = %#v", users)
	}
	if !strings.Contains(database.query, "status = 'active'") ||
		!strings.Contains(database.query, "eligible = TRUE") ||
		!strings.Contains(database.query, "blocked = FALSE") ||
		!strings.Contains(database.query, "fail_closed = FALSE") ||
		!strings.Contains(database.query, "tls.state = 'issued'") ||
		!strings.Contains(database.query, "tls.certificate_expires_at > now()") ||
		!strings.Contains(database.query, "ORDER BY bundle.telegram_id") {
		t.Fatalf("active credential list query is not stable and restricted: %q", database.query)
	}
	if len(cryptor.opens) != 2 || cryptor.opens[0].telegramID != 1001 || cryptor.opens[0].generation != 2 ||
		cryptor.opens[1].telegramID != 2002 || cryptor.opens[1].generation != 5 {
		t.Fatalf("Open() calls = %#v", cryptor.opens)
	}
}

func TestCredentialStoreReturnsNoPartialListWhenPersistedDataOrDecryptionIsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		row     *credentialListRowStub
		cryptor *credentialListCryptorStub
	}{
		{
			name: "mismatched arrays",
			row: &credentialListRowStub{
				telegramIDs: []int64{1001, 2002}, generations: []int64{1},
				nonces: [][]byte{[]byte("nonce-1001--")}, ciphertexts: [][]byte{[]byte("ciphertext-1001")},
			},
			cryptor: &credentialListCryptorStub{},
		},
		{
			name: "non increasing IDs",
			row: &credentialListRowStub{
				telegramIDs: []int64{2002, 1001}, generations: []int64{1, 1},
				nonces:      [][]byte{[]byte("nonce-2002--"), []byte("nonce-1001--")},
				ciphertexts: [][]byte{[]byte("ciphertext-2002"), []byte("ciphertext-1001")},
			},
			cryptor: &credentialListCryptorStub{},
		},
		{
			name: "second decrypt fails",
			row: &credentialListRowStub{
				telegramIDs: []int64{1001, 2002}, generations: []int64{1, 1},
				nonces:      [][]byte{[]byte("nonce-1001--"), []byte("nonce-2002--")},
				ciphertexts: [][]byte{[]byte("ciphertext-1001"), []byte("ciphertext-2002")},
			},
			cryptor: &credentialListCryptorStub{
				bundles:    map[int64]domain.CredentialBundle{1001: validProvisioningBundle()},
				openErrors: map[int64]error{2002: errors.New("decrypt failed")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewCredentialStore(&credentialDatabaseStub{row: test.row}, test.cryptor)
			users, err := store.ListActive(context.Background())
			if err == nil {
				t.Fatalf("ListActive() = %#v, error = nil", users)
			}
			if users != nil {
				t.Fatalf("ListActive() returned partial users: %#v", users)
			}
		})
	}
}

type credentialCryptorStub struct {
	digest         [32]byte
	digestErr      error
	bundle         domain.CredentialBundle
	openErr        error
	openTelegramID int64
	openGeneration uint64
	openNonce      []byte
	openCiphertext []byte
}

func (stub *credentialCryptorStub) SubscriptionTokenDigest(string) ([32]byte, error) {
	return stub.digest, stub.digestErr
}

func (stub *credentialCryptorStub) Open(telegramID int64, generation uint64, nonce, ciphertext []byte) (domain.CredentialBundle, error) {
	stub.openTelegramID = telegramID
	stub.openGeneration = generation
	stub.openNonce = nonce
	stub.openCiphertext = ciphertext
	return stub.bundle, stub.openErr
}

type credentialDatabaseStub struct {
	query     string
	arguments []any
	row       pgx.Row
}

func (stub *credentialDatabaseStub) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	stub.query = query
	stub.arguments = arguments
	return stub.row
}

func (stub *credentialDatabaseStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

type credentialRowStub struct {
	telegramID int64
	generation int64
	nonce      []byte
	ciphertext []byte
	err        error
}

func (row *credentialRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 4 {
		return errors.New("unexpected credential destination count")
	}
	*destinations[0].(*int64) = row.telegramID
	*destinations[1].(*int64) = row.generation
	*destinations[2].(*[]byte) = row.nonce
	*destinations[3].(*[]byte) = row.ciphertext
	return nil
}

type credentialOpenCall struct {
	telegramID int64
	generation uint64
}

type credentialListCryptorStub struct {
	bundles    map[int64]domain.CredentialBundle
	openErrors map[int64]error
	opens      []credentialOpenCall
}

func (stub *credentialListCryptorStub) SubscriptionTokenDigest(string) ([32]byte, error) {
	return [32]byte{}, errors.New("unexpected digest")
}

func (stub *credentialListCryptorStub) Open(telegramID int64, generation uint64, _, _ []byte) (domain.CredentialBundle, error) {
	stub.opens = append(stub.opens, credentialOpenCall{telegramID: telegramID, generation: generation})
	if err := stub.openErrors[telegramID]; err != nil {
		return domain.CredentialBundle{}, err
	}
	return stub.bundles[telegramID], nil
}

type credentialListRowStub struct {
	telegramIDs []int64
	generations []int64
	nonces      [][]byte
	ciphertexts [][]byte
	err         error
}

func (row *credentialListRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 4 {
		return errors.New("unexpected credential list destination count")
	}
	*destinations[0].(*[]int64) = row.telegramIDs
	*destinations[1].(*[]int64) = row.generations
	*destinations[2].(*[][]byte) = row.nonces
	*destinations[3].(*[][]byte) = row.ciphertexts
	return nil
}
