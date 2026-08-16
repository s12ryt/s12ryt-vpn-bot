package qualification

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestRecheckerProcessesKnownUsersInStableBatchesAndSendsOneSummary(t *testing.T) {
	users := make([]int64, 60)
	for index := range users {
		users[index] = int64(index + 1)
	}
	provider := &knownUserProviderStub{users: users}
	evaluator := &recheckEvaluatorStub{}
	writer := &recheckWriterStub{}
	notifier := &recheckNotifierStub{}
	rechecker, err := NewRechecker(provider, evaluator, writer, notifier, 50)
	if err != nil {
		t.Fatalf("NewRechecker() error = %v", err)
	}

	summary, err := rechecker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	want := RecheckSummary{Checked: 60, Eligible: 20, Ineligible: 20, Indeterminate: 20, Revocations: 20, UnclassifiedIndeterminate: 20}
	if summary != want {
		t.Fatalf("RunOnce() summary = %#v, want %#v", summary, want)
	}
	if !reflect.DeepEqual(provider.limits, []int{50, 50}) || !reflect.DeepEqual(provider.afterIDs, []int64{0, 50}) {
		t.Fatalf("page calls after=%v limits=%v", provider.afterIDs, provider.limits)
	}
	if len(writer.ids) != 40 {
		t.Fatalf("persisted decisions = %d, want 40 explicit decisions", len(writer.ids))
	}
	if len(notifier.summaries) != 1 || notifier.summaries[0] != want || len(notifier.failures) != 0 {
		t.Fatalf("notifications summaries=%#v failures=%#v", notifier.summaries, notifier.failures)
	}
}

func TestRecheckerImmediatelyReportsSystemicFailureAndStops(t *testing.T) {
	wantErr := errors.New("database unavailable")
	provider := &knownUserProviderStub{err: wantErr}
	notifier := &recheckNotifierStub{}
	rechecker, err := NewRechecker(provider, &recheckEvaluatorStub{}, &recheckWriterStub{}, notifier, 50)
	if err != nil {
		t.Fatalf("NewRechecker() error = %v", err)
	}

	if _, err := rechecker.RunOnce(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("RunOnce() error = %v, want provider failure", err)
	}
	if len(notifier.failures) != 1 || len(notifier.summaries) != 0 {
		t.Fatalf("notifications summaries=%#v failures=%#v", notifier.summaries, notifier.failures)
	}
}

func TestRecheckerRejectsInvalidBatchSize(t *testing.T) {
	for _, batchSize := range []int{9, 201} {
		if _, err := NewRechecker(&knownUserProviderStub{}, &recheckEvaluatorStub{}, &recheckWriterStub{}, &recheckNotifierStub{}, batchSize); err == nil {
			t.Fatalf("NewRechecker(batchSize=%d) error = nil", batchSize)
		}
	}
}

type knownUserProviderStub struct {
	users    []int64
	err      error
	afterIDs []int64
	limits   []int
}

func (stub *knownUserProviderStub) KnownUsersAfter(_ context.Context, afterTelegramID int64, limit int) ([]int64, error) {
	stub.afterIDs = append(stub.afterIDs, afterTelegramID)
	stub.limits = append(stub.limits, limit)
	if stub.err != nil {
		return nil, stub.err
	}
	result := make([]int64, 0, limit)
	for _, telegramID := range stub.users {
		if telegramID > afterTelegramID && len(result) < limit {
			result = append(result, telegramID)
		}
	}
	return result, nil
}

type recheckEvaluatorStub struct{}

func (*recheckEvaluatorStub) Evaluate(_ context.Context, telegramID int64) (domain.QualificationDecision, error) {
	switch telegramID % 3 {
	case 1:
		return domain.QualificationEligible, nil
	case 2:
		return domain.QualificationIneligible, nil
	default:
		return domain.QualificationIndeterminate, nil
	}
}

type recheckWriterStub struct {
	ids []int64
}

func (stub *recheckWriterStub) ApplyQualification(_ context.Context, telegramID int64, decision domain.QualificationDecision) (domain.AccessChange, error) {
	stub.ids = append(stub.ids, telegramID)
	return domain.AccessChange{RevokeCredentialsImmediately: decision == domain.QualificationIneligible}, nil
}

type recheckNotifierStub struct {
	summaries []RecheckSummary
	failures  []error
}

func (stub *recheckNotifierStub) NotifySummary(_ context.Context, summary RecheckSummary) error {
	stub.summaries = append(stub.summaries, summary)
	return nil
}

func (stub *recheckNotifierStub) NotifyFailure(_ context.Context, err error) error {
	stub.failures = append(stub.failures, err)
	return nil
}
