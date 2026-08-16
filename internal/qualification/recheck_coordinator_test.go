package qualification

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestRecheckCoordinatorReloadsTuningForEveryRun(t *testing.T) {
	clock := newRetryClock()
	settings := &recheckSettingsProviderStub{settings: []RecheckSettings{
		{Interval: time.Hour, RequestsPerSecond: 10, BatchSize: 50},
		{Interval: 2 * time.Hour, RequestsPerSecond: 20, BatchSize: 100},
	}}
	users := &knownUserProviderStub{users: []int64{1, 2}}
	rules := &ruleProviderStub{mode: domain.QualificationAny, rules: []Rule{{ChatID: -1001}}}
	members := &coordinatorMemberLookup{}
	writer := &recheckWriterStub{}
	notifier := &recheckNotifierStub{}
	coordinator, err := NewRecheckCoordinator(settings, users, rules, members, writer, notifier, clock.Wait, clock.Now, noJitter)
	if err != nil {
		t.Fatalf("NewRecheckCoordinator() error = %v", err)
	}

	first, firstInterval, err := coordinator.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	second, secondInterval, err := coordinator.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	if first.Checked != 2 || second.Checked != 2 || firstInterval != time.Hour || secondInterval != 2*time.Hour {
		t.Fatalf("runs = (%#v, %v), (%#v, %v)", first, firstInterval, second, secondInterval)
	}
	if !reflect.DeepEqual(users.limits, []int{50, 100}) {
		t.Fatalf("page limits = %v", users.limits)
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{100 * time.Millisecond, 50 * time.Millisecond}) {
		t.Fatalf("rate waits = %v", clock.waits)
	}
}

func TestRecheckCoordinatorReportsSettingsFailure(t *testing.T) {
	wantErr := errors.New("settings unavailable")
	notifier := &recheckNotifierStub{}
	coordinator, err := NewRecheckCoordinator(
		&recheckSettingsProviderStub{err: wantErr},
		&knownUserProviderStub{},
		&ruleProviderStub{},
		&coordinatorMemberLookup{},
		&recheckWriterStub{},
		notifier,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewRecheckCoordinator() error = %v", err)
	}

	if _, interval, err := coordinator.RunOnce(context.Background()); !errors.Is(err, wantErr) || interval != 0 {
		t.Fatalf("RunOnce() interval=%v error=%v", interval, err)
	}
	if len(notifier.failures) != 1 || len(notifier.summaries) != 0 {
		t.Fatalf("notifications summaries=%v failures=%v", notifier.summaries, notifier.failures)
	}
}

type recheckSettingsProviderStub struct {
	settings []RecheckSettings
	err      error
	calls    int
}

func (stub *recheckSettingsProviderStub) RecheckSettings(context.Context) (RecheckSettings, error) {
	if stub.err != nil {
		return RecheckSettings{}, stub.err
	}
	index := stub.calls
	stub.calls++
	if index >= len(stub.settings) {
		index = len(stub.settings) - 1
	}
	return stub.settings[index], nil
}

type coordinatorMemberLookup struct{}

func (*coordinatorMemberLookup) GetChatMember(_ context.Context, _ int64, userID int64) (telegram.ChatMember, error) {
	return telegram.ChatMember{User: telegram.User{ID: userID}, Status: "member"}, nil
}
