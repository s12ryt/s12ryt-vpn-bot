package auth

import (
	"context"
	"errors"
	"io"
	"regexp"
	"testing"
)

func TestLoginFlowConsumesCodeAndCreatesSession(t *testing.T) {
	administrator := Administrator{TelegramID: 12345, Role: RoleOwner, Active: true}
	codes := &codeConsumerStub{administrator: administrator}
	sessions := &sessionCreatorStub{token: "session-token"}
	flow := NewLoginFlow(&sessionTokenReader{}, codes, sessions)

	sessionToken, csrfToken, err := flow.Exchange(context.Background(), 12345, "Ab12Cd34")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if sessionToken != "session-token" || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(csrfToken) {
		t.Fatalf("Exchange() = (%q, %q), want session token and 32-byte CSRF token", sessionToken, csrfToken)
	}
	if codes.telegramID != 12345 || codes.code != "Ab12Cd34" || sessions.administrator != administrator {
		t.Fatalf("flow did not pass authenticated administrator: telegram_id=%d code=%q admin=%#v", codes.telegramID, codes.code, sessions.administrator)
	}
}

func TestLoginFlowDoesNotConsumeCodeWhenCSRFRandomnessFails(t *testing.T) {
	codes := &codeConsumerStub{}
	sessions := &sessionCreatorStub{}
	flow := NewLoginFlow(errorReader{}, codes, sessions)

	if _, _, err := flow.Exchange(context.Background(), 12345, "Ab12Cd34"); err == nil {
		t.Fatal("Exchange() error = nil, want random source error")
	}
	if codes.calls != 0 || sessions.calls != 0 {
		t.Fatalf("calls after random failure = codes %d, sessions %d; want zero", codes.calls, sessions.calls)
	}
}

func TestLoginFlowStopsWhenCodeOrSessionCreationFails(t *testing.T) {
	tests := []struct {
		name       string
		codeErr    error
		sessionErr error
		wantCreate int
	}{
		{name: "invalid code", codeErr: ErrLoginCodeInvalid},
		{name: "session store failure", sessionErr: errors.New("store unavailable"), wantCreate: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codes := &codeConsumerStub{administrator: Administrator{TelegramID: 12345, Role: RoleOwner, Active: true}, err: test.codeErr}
			sessions := &sessionCreatorStub{err: test.sessionErr}
			flow := NewLoginFlow(&sessionTokenReader{}, codes, sessions)

			if _, _, err := flow.Exchange(context.Background(), 12345, "Ab12Cd34"); err == nil {
				t.Fatal("Exchange() error = nil, want dependency error")
			}
			if sessions.calls != test.wantCreate {
				t.Fatalf("session create calls = %d, want %d", sessions.calls, test.wantCreate)
			}
		})
	}
}

type codeConsumerStub struct {
	administrator Administrator
	err           error
	code          string
	telegramID    int64
	calls         int
}

func (stub *codeConsumerStub) Consume(_ context.Context, telegramID int64, code string) (Administrator, error) {
	stub.calls++
	stub.telegramID = telegramID
	stub.code = code
	return stub.administrator, stub.err
}

type sessionCreatorStub struct {
	token         string
	err           error
	administrator Administrator
	calls         int
}

func (stub *sessionCreatorStub) Create(_ context.Context, administrator Administrator) (string, error) {
	stub.calls++
	stub.administrator = administrator
	return stub.token, stub.err
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
