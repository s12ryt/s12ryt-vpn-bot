package coreworker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
)

func TestWorkerMergesPlannedActionsDuringThirtySecondNotice(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionStoreStub{claims: [][]postgres.CoreAction{
		{{ID: 1, TelegramID: 1001, Action: postgres.CoreActionReconcile, Attempts: 1}},
		{{ID: 2, TelegramID: 2002, Action: postgres.CoreActionReconcile, Attempts: 1}},
	}}
	installer := &installerStub{}
	notifier := &notifierStub{}
	worker := newTestWorker(store, installer, notifier)

	if err := worker.Step(context.Background(), now); err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if notifier.plannedCalls != 1 || installer.calls != 0 {
		t.Fatalf("after first step notifier=%d installer=%d", notifier.plannedCalls, installer.calls)
	}
	if err := worker.Step(context.Background(), now.Add(10*time.Second)); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if notifier.plannedCalls != 1 || installer.calls != 0 {
		t.Fatalf("during notice notifier=%d installer=%d", notifier.plannedCalls, installer.calls)
	}
	if err := worker.Step(context.Background(), now.Add(30*time.Second)); err != nil {
		t.Fatalf("third Step() error = %v", err)
	}
	if installer.calls != 1 || !reflect.DeepEqual(store.completed, [][]int64{{1, 2}}) {
		t.Fatalf("installer=%d completed=%#v", installer.calls, store.completed)
	}
}

func TestWorkerSafetyRevokeInterruptsNoticeAndCompletesLatestSnapshotOnce(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionStoreStub{claims: [][]postgres.CoreAction{
		{{ID: 1, TelegramID: 1001, Action: postgres.CoreActionReconcile, Attempts: 1}},
		{{ID: 2, TelegramID: 2002, Action: postgres.CoreActionRevoke, Attempts: 1}},
	}}
	installer := &installerStub{}
	worker := newTestWorker(store, installer, &notifierStub{})

	if err := worker.Step(context.Background(), now); err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if err := worker.Step(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if installer.calls != 1 || !reflect.DeepEqual(store.completed, [][]int64{{1, 2}}) {
		t.Fatalf("installer=%d completed=%#v", installer.calls, store.completed)
	}
}

func TestWorkerLimitsPlannedRestartsToOnePerMinute(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionStoreStub{claims: [][]postgres.CoreAction{
		{{ID: 1, TelegramID: 1001, Action: postgres.CoreActionReconcile, Attempts: 1}},
		nil,
		{{ID: 2, TelegramID: 2002, Action: postgres.CoreActionReconcile, Attempts: 1}},
	}}
	installer := &installerStub{}
	worker := newTestWorker(store, installer, &notifierStub{})

	for _, at := range []time.Time{now, now.Add(30 * time.Second), now.Add(40 * time.Second), now.Add(89 * time.Second)} {
		if err := worker.Step(context.Background(), at); err != nil {
			t.Fatalf("Step(%s) error = %v", at, err)
		}
	}
	if installer.calls != 1 {
		t.Fatalf("installer calls before cooldown = %d", installer.calls)
	}
	if err := worker.Step(context.Background(), now.Add(90*time.Second)); err != nil {
		t.Fatalf("cooldown Step() error = %v", err)
	}
	if installer.calls != 2 || !reflect.DeepEqual(store.completed, [][]int64{{1}, {2}}) {
		t.Fatalf("installer=%d completed=%#v", installer.calls, store.completed)
	}
}

func TestWorkerRetriesEachFailedActionByAttemptAndReportsClosedFailure(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	wantErr := errors.New("database contains sensitive detail")
	store := &actionStoreStub{claims: [][]postgres.CoreAction{{
		{ID: 1, TelegramID: 1001, Action: postgres.CoreActionRevoke, Attempts: 1},
		{ID: 2, TelegramID: 2002, Action: postgres.CoreActionReconcile, Attempts: 3},
	}}}
	notifier := &notifierStub{}
	worker := New(store, &snapshotStub{err: wantErr}, &installerStub{}, notifier)

	err := worker.Step(context.Background(), now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Step() error = %v", err)
	}
	wantRetries := []retryCall{
		{ids: []int64{1}, at: now.Add(5 * time.Second), failure: postgres.CoreFailureSnapshot},
		{ids: []int64{2}, at: now.Add(time.Minute), failure: postgres.CoreFailureSnapshot},
	}
	if !reflect.DeepEqual(store.retries, wantRetries) {
		t.Fatalf("retries = %#v, want %#v", store.retries, wantRetries)
	}
	if !reflect.DeepEqual(notifier.failures, []postgres.CoreFailureCode{postgres.CoreFailureSnapshot}) {
		t.Fatalf("failure notifications = %#v", notifier.failures)
	}
}

func TestWorkerContinuesPlannedRestartWhenNoticeDeliveryIsPartial(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionStoreStub{claims: [][]postgres.CoreAction{{
		{ID: 1, TelegramID: 1001, Action: postgres.CoreActionReconcile, Attempts: 1},
	}}}
	installer := &installerStub{}
	notifier := &notifierStub{plannedErr: errors.New("some users unreachable")}
	worker := newTestWorker(store, installer, notifier)

	if err := worker.Step(context.Background(), now); err != nil {
		t.Fatalf("notice Step() error = %v", err)
	}
	if err := worker.Step(context.Background(), now.Add(30*time.Second)); err != nil {
		t.Fatalf("install Step() error = %v", err)
	}
	if installer.calls != 1 {
		t.Fatalf("installer calls = %d", installer.calls)
	}
}

func TestWorkerDoesNotRestartAgainWhenCompletionTemporarilyFails(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	wantErr := errors.New("completion unavailable")
	store := &actionStoreStub{
		claims:         [][]postgres.CoreAction{{{ID: 1, TelegramID: 1001, Action: postgres.CoreActionRevoke, Attempts: 1}}},
		completeErrors: []error{wantErr, nil},
	}
	installer := &installerStub{}
	worker := newTestWorker(store, installer, &notifierStub{})

	if err := worker.Step(context.Background(), now); !errors.Is(err, wantErr) {
		t.Fatalf("first Step() error = %v", err)
	}
	if err := worker.Step(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if installer.calls != 1 {
		t.Fatalf("installer calls = %d, want 1", installer.calls)
	}
}

func TestWorkerRejectsInvalidClaimBatchWithoutPartialPendingState(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionStoreStub{claims: [][]postgres.CoreAction{{
		{ID: 1, TelegramID: 1001, Action: postgres.CoreActionReconcile, Attempts: 1},
		{ID: 0, TelegramID: 2002, Action: postgres.CoreActionReconcile, Attempts: 1},
	}}}
	worker := newTestWorker(store, &installerStub{}, &notifierStub{})

	if err := worker.Step(context.Background(), now); err == nil {
		t.Fatal("Step() accepted invalid claim batch")
	}
	if len(worker.pending) != 0 {
		t.Fatalf("pending = %#v, want no partial state", worker.pending)
	}
}

func TestWorkerRunPollsEverySecondAndStopsOnCancellation(t *testing.T) {
	store := &actionStoreStub{}
	worker := newTestWorker(store, &installerStub{}, &notifierStub{})
	ctx, cancel := context.WithCancel(context.Background())
	var waits []time.Duration

	err := worker.Run(ctx, func(waitContext context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		if len(waits) == 3 {
			cancel()
		}
		return waitContext.Err()
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if store.claimCalls != 3 || !reflect.DeepEqual(waits, []time.Duration{time.Second, time.Second, time.Second}) {
		t.Fatalf("claim calls=%d waits=%#v", store.claimCalls, waits)
	}
}

func newTestWorker(store *actionStoreStub, installer *installerStub, notifier *notifierStub) *Worker {
	return New(store, &snapshotStub{configuration: []byte(`{"ok":true}`)}, installer, notifier)
}

type actionStoreStub struct {
	claims         [][]postgres.CoreAction
	completed      [][]int64
	completeErrors []error
	retries        []retryCall
	claimCalls     int
}

func (stub *actionStoreStub) ClaimDue(context.Context, time.Time, time.Duration, int) ([]postgres.CoreAction, error) {
	stub.claimCalls++
	if len(stub.claims) == 0 {
		return nil, nil
	}
	actions := stub.claims[0]
	stub.claims = stub.claims[1:]
	return actions, nil
}

func (stub *actionStoreStub) Complete(_ context.Context, ids []int64, _ time.Time) error {
	stub.completed = append(stub.completed, append([]int64(nil), ids...))
	if len(stub.completeErrors) > 0 {
		err := stub.completeErrors[0]
		stub.completeErrors = stub.completeErrors[1:]
		return err
	}
	return nil
}

func (stub *actionStoreStub) Retry(_ context.Context, ids []int64, at time.Time, failure postgres.CoreFailureCode) error {
	stub.retries = append(stub.retries, retryCall{ids: append([]int64(nil), ids...), at: at, failure: failure})
	return nil
}

type retryCall struct {
	ids     []int64
	at      time.Time
	failure postgres.CoreFailureCode
}

type snapshotStub struct {
	configuration []byte
	err           error
}

func (stub *snapshotStub) Build(context.Context) ([]byte, error) {
	return append([]byte(nil), stub.configuration...), stub.err
}

type installerStub struct {
	calls int
	err   error
}

func (stub *installerStub) Install(context.Context, []byte) error {
	stub.calls++
	return stub.err
}

type notifierStub struct {
	plannedCalls int
	plannedErr   error
	failures     []postgres.CoreFailureCode
}

func (stub *notifierStub) NotifyPlannedRestart(context.Context) error {
	stub.plannedCalls++
	return stub.plannedErr
}

func (stub *notifierStub) NotifyCoreFailure(_ context.Context, failure postgres.CoreFailureCode) error {
	stub.failures = append(stub.failures, failure)
	return nil
}
