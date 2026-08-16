package telegram

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRunnerDispatchesMessagesRepliesAndAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &runnerClientStub{
		updates: []Update{{
			UpdateID: 10,
			Message: &APIMessage{
				Chat: Chat{ID: 99, Type: ChatPrivate},
				From: &User{ID: 12345},
				Text: "/adminlogin",
			},
		}},
		cancel: cancel,
	}
	handler := runnerHandlerStub{reply: Reply{Text: "Ab12Cd34"}, handled: true}
	runner := NewRunner(client, handler, nil)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(client.offsets, []int64{0, 11}) {
		t.Fatalf("offsets = %v, want [0 11]", client.offsets)
	}
	if !reflect.DeepEqual(client.sent, []sentMessage{{chatID: 99, text: "Ab12Cd34"}}) {
		t.Fatalf("sent = %#v", client.sent)
	}
	if handler.lastMessage.SenderID != 0 {
		// The handler value is copied into the runner; message mapping is asserted by the dedicated pointer test below.
		t.Fatal("unexpected mutation of copied handler")
	}
}

func TestRunnerMapsTelegramMessageToCommandHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &runnerClientStub{
		updates: []Update{{UpdateID: 4, Message: &APIMessage{Chat: Chat{ID: 9, Type: ChatSupergroup}, From: &User{ID: 88}, Text: "/adminlogin"}}},
		cancel:  cancel,
	}
	handler := &runnerHandlerPointerStub{}
	runner := NewRunner(client, handler, nil)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := Message{ChatType: ChatSupergroup, SenderID: 88, Text: "/adminlogin"}
	if handler.message != want {
		t.Fatalf("mapped message = %#v, want %#v", handler.message, want)
	}
}

func TestRunnerDispatchesChatMemberChangeWithoutSendingPublicReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &runnerClientStub{
		updates: []Update{{
			UpdateID: 5,
			ChatMember: &ChatMemberUpdated{
				Chat:          Chat{ID: -1009, Type: ChatSupergroup},
				NewChatMember: ChatMember{User: User{ID: 12345}, Status: "left"},
			},
		}},
		cancel: cancel,
	}
	membership := &membershipHandlerStub{}
	runner := NewRunner(client, runnerHandlerStub{}, nil, membership)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := MembershipEvent{ChatID: -1009, ChatType: ChatSupergroup, TelegramID: 12345, Result: "not_member"}
	if membership.event != want {
		t.Fatalf("membership event = %#v, want %#v", membership.event, want)
	}
	if len(client.sent) != 0 {
		t.Fatalf("sent messages = %#v, want none", client.sent)
	}
}

func TestRunnerRetriesChatMemberChangeBeforeAdvancingOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &membershipRetryClientStub{
		update: Update{
			UpdateID: 8,
			ChatMember: &ChatMemberUpdated{
				Chat:          Chat{ID: -1009, Type: ChatSupergroup},
				NewChatMember: ChatMember{User: User{ID: 12345}, Status: "member"},
			},
		},
		cancel: cancel,
	}
	membership := &membershipRetryHandlerStub{}
	runner := NewRunner(client, runnerHandlerStub{}, func(context.Context, time.Duration) error { return nil }, membership)

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(client.offsets, []int64{0, 0, 9}) {
		t.Fatalf("offsets = %v, want [0 0 9]", client.offsets)
	}
	if membership.calls != 2 {
		t.Fatalf("membership calls = %d, want 2", membership.calls)
	}
}

func TestRunnerDispatchesCallbackAndRetriesBeforeAdvancingOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &membershipRetryClientStub{update: Update{UpdateID: 12, CallbackQuery: &CallbackQuery{ID: "cb-1", From: User{ID: 77}, Data: "approve:12345"}}, cancel: cancel}
	callbacks := &callbackRetryHandlerStub{}
	runner := NewRunner(client, runnerHandlerStub{}, func(context.Context, time.Duration) error { return nil }).WithCallbackHandler(callbacks)
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(client.offsets, []int64{0, 0, 13}) {
		t.Fatalf("offsets = %v", client.offsets)
	}
	if callbacks.calls != 2 || callbacks.callback != (Callback{ID: "cb-1", SenderID: 77, Data: "approve:12345"}) {
		t.Fatalf("callbacks = %d %#v", callbacks.calls, callbacks.callback)
	}
}

func TestRunnerRetriesTemporaryPollingFailureWithBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &runnerClientStub{firstErr: errors.New("temporary Telegram failure"), cancel: cancel}
	waits := 0
	runner := NewRunner(client, runnerHandlerStub{}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if waits != 1 || len(client.offsets) != 2 || client.offsets[0] != 0 || client.offsets[1] != 0 {
		t.Fatalf("retry behavior = waits %d offsets %v", waits, client.offsets)
	}
}

type runnerClientStub struct {
	updates  []Update
	firstErr error
	cancel   context.CancelFunc
	offsets  []int64
	sent     []sentMessage
}

func (stub *runnerClientStub) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	stub.offsets = append(stub.offsets, offset)
	if len(stub.offsets) == 1 && stub.firstErr != nil {
		return nil, stub.firstErr
	}
	if len(stub.offsets) == 1 && stub.firstErr == nil {
		return stub.updates, nil
	}
	stub.cancel()
	return nil, ctx.Err()
}

func (stub *runnerClientStub) SendMessage(_ context.Context, chatID int64, text string) error {
	stub.sent = append(stub.sent, sentMessage{chatID: chatID, text: text})
	return nil
}

type sentMessage struct {
	chatID int64
	text   string
}

type runnerHandlerStub struct {
	reply       Reply
	handled     bool
	lastMessage Message
}

func (stub runnerHandlerStub) Handle(_ context.Context, message Message) (Reply, bool) {
	stub.lastMessage = message
	return stub.reply, stub.handled
}

type runnerHandlerPointerStub struct {
	message Message
}

type membershipHandlerStub struct {
	event MembershipEvent
}

func (stub *membershipHandlerStub) HandleMembership(_ context.Context, event MembershipEvent) error {
	stub.event = event
	return nil
}

type membershipRetryClientStub struct {
	update  Update
	cancel  context.CancelFunc
	offsets []int64
}

func (stub *membershipRetryClientStub) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	stub.offsets = append(stub.offsets, offset)
	if len(stub.offsets) <= 2 {
		return []Update{stub.update}, nil
	}
	stub.cancel()
	return nil, ctx.Err()
}

func (*membershipRetryClientStub) SendMessage(context.Context, int64, string) error { return nil }

type membershipRetryHandlerStub struct {
	calls int
}

type callbackRetryHandlerStub struct {
	calls    int
	callback Callback
}

func (stub *callbackRetryHandlerStub) HandleCallback(_ context.Context, callback Callback) error {
	stub.calls++
	stub.callback = callback
	if stub.calls == 1 {
		return errors.New("temporary callback failure")
	}
	return nil
}

func (stub *membershipRetryHandlerStub) HandleMembership(context.Context, MembershipEvent) error {
	stub.calls++
	if stub.calls == 1 {
		return errors.New("temporary persistence failure")
	}
	return nil
}

func (stub *runnerHandlerPointerStub) Handle(_ context.Context, message Message) (Reply, bool) {
	stub.message = message
	return Reply{}, false
}
