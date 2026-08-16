package trafficrunner

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
)

func TestRunnerCollectsSpoolsCommitsAndDeletes(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	collector := &collectorStub{samples: []trafficstats.Sample{{TelegramID: 1001, Uplink: 11}}}
	spool := &spoolStub{}
	recorder := &recorderStub{result: postgres.TrafficBatchResult{Applied: 1}}
	runner := New(collector, spool, recorder)

	result, err := runner.Step(context.Background(), now)
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if result.Replayed || result.Applied != 1 || collector.calls != 1 || spool.saveCalls != 1 || spool.deleteCalls != 1 || recorder.calls != 1 {
		t.Fatalf("Step() result=%#v collector=%d save=%d delete=%d record=%d", result, collector.calls, spool.saveCalls, spool.deleteCalls, recorder.calls)
	}
	if recorder.batch.ID == "" || !recorder.batch.CollectedAt.Equal(now) || !reflect.DeepEqual(recorder.batch.Samples, collector.samples) {
		t.Fatalf("recorded batch = %#v", recorder.batch)
	}
}

func TestRunnerReplaysPendingBeforeCollectingAgain(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	pending, err := trafficstats.NewPendingBatch(now.Add(-time.Minute), []trafficstats.Sample{{TelegramID: 1001, Downlink: 7}})
	if err != nil {
		t.Fatalf("NewPendingBatch() error = %v", err)
	}
	collector := &collectorStub{samples: []trafficstats.Sample{{TelegramID: 2002, Uplink: 99}}}
	spool := &spoolStub{batch: pending, exists: true}
	recorder := &recorderStub{result: postgres.TrafficBatchResult{Applied: 1}}
	runner := New(collector, spool, recorder)

	result, err := runner.Step(context.Background(), now)
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if !result.Replayed || collector.calls != 0 || spool.saveCalls != 0 || spool.deleteCalls != 1 || recorder.batch.ID != pending.ID {
		t.Fatalf("Step() result=%#v collector=%d save=%d delete=%d batch=%#v", result, collector.calls, spool.saveCalls, spool.deleteCalls, recorder.batch)
	}
}

func TestRunnerLeavesPendingAfterRecordFailureAndDoesNotResetAgain(t *testing.T) {
	wantErr := errors.New("database unavailable")
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	collector := &collectorStub{samples: []trafficstats.Sample{{TelegramID: 1001, Uplink: 1}}}
	spool := &spoolStub{}
	recorder := &recorderStub{err: wantErr}
	runner := New(collector, spool, recorder)

	if _, err := runner.Step(context.Background(), now); !errors.Is(err, wantErr) || FailureStageOf(err) != FailureRecord {
		t.Fatalf("first Step() error = %v stage=%q", err, FailureStageOf(err))
	}
	if collector.calls != 1 || !spool.exists || spool.deleteCalls != 0 {
		t.Fatalf("after failure collector=%d exists=%v delete=%d", collector.calls, spool.exists, spool.deleteCalls)
	}
	recorder.err = nil
	if _, err := runner.Step(context.Background(), now.Add(15*time.Second)); err != nil {
		t.Fatalf("replay Step() error = %v", err)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want no second reset while pending", collector.calls)
	}
}

func TestRunnerClassifiesCollectorAndSpoolFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		collector *collectorStub
		spool     *spoolStub
		wantStage FailureStage
	}{
		{name: "load", collector: &collectorStub{}, spool: &spoolStub{loadErr: errors.New("load")}, wantStage: FailureSpool},
		{name: "collect", collector: &collectorStub{err: errors.New("collect")}, spool: &spoolStub{}, wantStage: FailureCollect},
		{name: "save", collector: &collectorStub{samples: []trafficstats.Sample{{TelegramID: 1, Uplink: 1}}}, spool: &spoolStub{saveErr: errors.New("save")}, wantStage: FailureSpool},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := New(test.collector, test.spool, &recorderStub{})
			if _, err := runner.Step(context.Background(), time.Now()); err == nil || FailureStageOf(err) != test.wantStage {
				t.Fatalf("Step() error=%v stage=%q, want %q", err, FailureStageOf(err), test.wantStage)
			}
		})
	}
}

type collectorStub struct {
	samples []trafficstats.Sample
	err     error
	calls   int
}

func (stub *collectorStub) Collect(context.Context) ([]trafficstats.Sample, error) {
	stub.calls++
	return append([]trafficstats.Sample(nil), stub.samples...), stub.err
}

type spoolStub struct {
	batch       trafficstats.PendingBatch
	exists      bool
	loadErr     error
	saveErr     error
	deleteErr   error
	saveCalls   int
	deleteCalls int
}

func (stub *spoolStub) Load(context.Context) (trafficstats.PendingBatch, bool, error) {
	return stub.batch, stub.exists, stub.loadErr
}

func (stub *spoolStub) Save(_ context.Context, batch trafficstats.PendingBatch) error {
	stub.saveCalls++
	if stub.saveErr == nil {
		stub.batch = batch
		stub.exists = true
	}
	return stub.saveErr
}

func (stub *spoolStub) Delete(context.Context) error {
	stub.deleteCalls++
	if stub.deleteErr == nil {
		stub.batch = trafficstats.PendingBatch{}
		stub.exists = false
	}
	return stub.deleteErr
}

type recorderStub struct {
	batch  trafficstats.PendingBatch
	result postgres.TrafficBatchResult
	err    error
	calls  int
}

func (stub *recorderStub) RecordPendingBatch(_ context.Context, batch trafficstats.PendingBatch) (postgres.TrafficBatchResult, error) {
	stub.calls++
	stub.batch = batch
	return stub.result, stub.err
}
