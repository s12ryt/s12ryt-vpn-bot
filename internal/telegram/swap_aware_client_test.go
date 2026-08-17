package telegram

import (
	"context"
	"errors"
	"testing"
)

func TestSwapAwareClientRejectsInvalidInitialIdentity(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		identity User
	}{
		{name: "nil client", client: nil, identity: User{ID: 42, IsBot: true, Username: "member_bot"}},
		{name: "non-positive id", client: &Client{}, identity: User{ID: 0, IsBot: true, Username: "member_bot"}},
		{name: "not a bot", client: &Client{}, identity: User{ID: 42, IsBot: false, Username: "member_bot"}},
		{name: "empty username", client: &Client{}, identity: User{ID: 42, IsBot: true, Username: ""}},
	}
	for _, test := range tests {
		if _, err := NewSwapAwareClient(test.client, test.identity); err == nil {
			t.Fatalf("%s: expected rejection", test.name)
		}
	}
}

func TestSwapAwareClientExposesIdentityAndCurrentClient(t *testing.T) {
	initial := &Client{}
	identity := User{ID: 42, IsBot: true, Username: "member_bot"}
	wrapper, err := NewSwapAwareClient(initial, identity)
	if err != nil {
		t.Fatalf("NewSwapAwareClient() error = %v", err)
	}
	if wrapper.Current() != initial || wrapper.Identity() != identity {
		t.Fatalf("wrapper exposed client=%p identity=%#v", wrapper.Current(), wrapper.Identity())
	}
}

func TestSwapReplacesClientOnlyAfterSameBotIdentityVerified(t *testing.T) {
	identity := User{ID: 42, IsBot: true, Username: "member_bot"}
	wrapper, _ := NewSwapAwareClient(&Client{}, identity)
	factory := &swapFactory{}
	verifier := func(_ context.Context, _ *Client) (User, error) { return identity, nil }

	if err := wrapper.Swap(context.Background(), "new-token", factory.newClient, verifier); err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	if len(factory.tokens) != 1 || factory.tokens[0] != "new-token" {
		t.Fatalf("factory tokens = %v", factory.tokens)
	}
	if wrapper.Current() != factory.last {
		t.Fatal("swap must install the validated candidate")
	}
	if wrapper.Identity() != identity {
		t.Fatal("identity must be unchanged for the same bot")
	}
}

func TestSwapRejectsDifferentBotOrVerificationFailure(t *testing.T) {
	identity := User{ID: 42, IsBot: true, Username: "member_bot"}
	tests := []struct {
		name      string
		verified  User
		verifyErr error
	}{
		{name: "different bot id", verified: User{ID: 99, IsBot: true, Username: "other_bot"}},
		{name: "not a bot", verified: User{ID: 42, IsBot: false, Username: "member_bot"}},
		{name: "empty username", verified: User{ID: 42, IsBot: true, Username: ""}},
		{name: "verification failure", verified: identity, verifyErr: errors.New("network down")},
	}
	for _, test := range tests {
		wrapper, _ := NewSwapAwareClient(&Client{}, identity)
		before := wrapper.Current()
		factory := &swapFactory{}
		verifier := func(_ context.Context, _ *Client) (User, error) { return test.verified, test.verifyErr }
		if err := wrapper.Swap(context.Background(), "new-token", factory.newClient, verifier); err == nil {
			t.Fatalf("%s: expected rejection", test.name)
		}
		if wrapper.Current() != before {
			t.Fatalf("%s: failed swap must not replace the client", test.name)
		}
	}
}

func TestSwapRejectsEmptyTokenAndNilArguments(t *testing.T) {
	identity := User{ID: 42, IsBot: true, Username: "member_bot"}
	wrapper, _ := NewSwapAwareClient(&Client{}, identity)
	verifier := func(_ context.Context, _ *Client) (User, error) { return identity, nil }
	if err := wrapper.Swap(context.Background(), "", wrapperClientFactory(), verifier); err == nil {
		t.Fatal("empty token must be rejected")
	}
	if err := wrapper.Swap(context.Background(), "token", nil, verifier); err == nil {
		t.Fatal("nil factory must be rejected")
	}
	if err := wrapper.Swap(context.Background(), "token", wrapperClientFactory(), nil); err == nil {
		t.Fatal("nil verifier must be rejected")
	}
}

func wrapperClientFactory() ClientFactory {
	return func(string) *Client { return &Client{} }
}

type swapFactory struct {
	tokens []string
	last   *Client
}

func (factory *swapFactory) newClient(token string) *Client {
	factory.tokens = append(factory.tokens, token)
	factory.last = &Client{}
	return factory.last
}
