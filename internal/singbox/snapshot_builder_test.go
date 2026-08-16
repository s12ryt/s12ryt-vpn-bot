package singbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSnapshotBuilderBuildsSettingsWithCurrentActiveUsers(t *testing.T) {
	settings := testSettings()
	settings.Users = []User{{TelegramID: 9999, Credentials: testCredentialBundle("z")}}
	wantUsers := []User{
		{TelegramID: 2002, Credentials: testCredentialBundle("b")},
		{TelegramID: 1001, Credentials: testCredentialBundle("a")},
	}
	loader := &snapshotSettingsLoaderStub{settings: settings}
	users := &snapshotUserLoaderStub{users: wantUsers}
	builder := NewSnapshotBuilder(loader, users, Generator{})

	generated, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.Contains(string(generated), `"name": "1001"`) || !strings.Contains(string(generated), `"name": "2002"`) {
		t.Fatalf("Build() output lacks active users: %s", generated)
	}
	if strings.Contains(string(generated), `"name": "9999"`) {
		t.Fatal("Build() retained stale users from settings source")
	}
	if users.calls != 1 || loader.calls != 1 {
		t.Fatalf("settings calls=%d user calls=%d", loader.calls, users.calls)
	}
}

func TestSnapshotBuilderReturnsNoConfigurationOnDependencyFailure(t *testing.T) {
	wantSettingsErr := errors.New("settings failed")
	wantUsersErr := errors.New("users failed")
	wantGenerateErr := errors.New("generate failed")
	tests := []struct {
		name          string
		settings      *snapshotSettingsLoaderStub
		users         *snapshotUserLoaderStub
		generator     SnapshotGenerator
		wantErr       error
		wantUserCalls int
	}{
		{
			name: "settings", settings: &snapshotSettingsLoaderStub{err: wantSettingsErr},
			users: &snapshotUserLoaderStub{}, generator: &snapshotGeneratorStub{}, wantErr: wantSettingsErr,
		},
		{
			name: "users", settings: &snapshotSettingsLoaderStub{settings: testSettings()},
			users: &snapshotUserLoaderStub{err: wantUsersErr}, generator: &snapshotGeneratorStub{},
			wantErr: wantUsersErr, wantUserCalls: 1,
		},
		{
			name: "generator", settings: &snapshotSettingsLoaderStub{settings: testSettings()},
			users: &snapshotUserLoaderStub{}, generator: &snapshotGeneratorStub{err: wantGenerateErr},
			wantErr: wantGenerateErr, wantUserCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := NewSnapshotBuilder(test.settings, test.users, test.generator)
			generated, err := builder.Build(context.Background())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Build() error = %v, want %v", err, test.wantErr)
			}
			if generated != nil {
				t.Fatalf("Build() returned partial configuration: %q", generated)
			}
			if test.users.calls != test.wantUserCalls {
				t.Fatalf("user loader calls = %d, want %d", test.users.calls, test.wantUserCalls)
			}
		})
	}
}

type snapshotSettingsLoaderStub struct {
	settings Settings
	err      error
	calls    int
}

func (stub *snapshotSettingsLoaderStub) Load(context.Context) (Settings, error) {
	stub.calls++
	return stub.settings, stub.err
}

type snapshotUserLoaderStub struct {
	users []User
	err   error
	calls int
}

func (stub *snapshotUserLoaderStub) ListActive(context.Context) ([]User, error) {
	stub.calls++
	return stub.users, stub.err
}

type snapshotGeneratorStub struct {
	err error
}

func (stub *snapshotGeneratorStub) Generate(Settings) ([]byte, error) {
	if stub.err != nil {
		return nil, stub.err
	}
	return []byte(`{"ok":true}`), nil
}
