package reality

import (
	"context"
	"errors"
	"sync"
	"time"
)

// SearchStatus is the lifecycle state of a background REALITY target search.
type SearchStatus string

const (
	SearchStatusIdle      SearchStatus = "idle"
	SearchStatusRunning   SearchStatus = "running"
	SearchStatusCompleted SearchStatus = "completed"
	SearchStatusFailed    SearchStatus = "failed"
)

// ErrSearchRunning is returned when a search is requested while another one is
// still in progress.
var ErrSearchRunning = errors.New("reality search already running")

// SearchSnapshot is an immutable view of the current or most recent search.
type SearchSnapshot struct {
	Status    SearchStatus `json:"status"`
	StartedAt time.Time    `json:"started_at,omitempty"`
	Targets   []Target     `json:"targets,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// searchFailedReason is the only failure reason exposed in snapshots so the
// underlying dataset or probe errors never leak to API consumers.
const searchFailedReason = "search_failed"

// Service runs at most one REALITY target search at a time in the background
// so HTTP callers can trigger a search and poll for results instead of
// blocking a request for the whole search budget.
type Service struct {
	options Options

	mu        sync.Mutex
	status    SearchStatus
	startedAt time.Time
	targets   []Target
}

// NewService validates the search options up front and returns nil when they
// are invalid. Callers must pass non-nil dataset and prober implementations
// within the documented limits.
func NewService(options Options) *Service {
	if options.Dataset == nil || options.Prober == nil {
		return nil
	}
	if options.SampleLimit < 1 || options.SampleLimit > maxSampleLimit {
		return nil
	}
	if options.Concurrency < 1 || options.Concurrency > maxConcurrency {
		return nil
	}
	if options.Budget <= 0 || options.Budget > maxBudget {
		return nil
	}
	return &Service{options: options, status: SearchStatusIdle}
}

// Start launches a background search. It returns ErrSearchRunning when a
// search is already in progress; a finished search may be started again. The
// provided context must be non-nil and should outlive the triggering HTTP
// request (use context.WithoutCancel on the request context).
func (service *Service) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("reality search requires a context")
	}
	service.mu.Lock()
	if service.status == SearchStatusRunning {
		service.mu.Unlock()
		return ErrSearchRunning
	}
	service.status = SearchStatusRunning
	service.startedAt = time.Now().UTC()
	service.targets = nil
	service.mu.Unlock()

	go service.run(ctx)
	return nil
}

// Snapshot returns the current search state with a copy of any targets.
func (service *Service) Snapshot() SearchSnapshot {
	service.mu.Lock()
	defer service.mu.Unlock()
	snapshot := SearchSnapshot{
		Status:    service.status,
		StartedAt: service.startedAt,
	}
	if service.targets != nil {
		snapshot.Targets = append([]Target(nil), service.targets...)
	}
	if service.status == SearchStatusFailed {
		snapshot.Error = searchFailedReason
	}
	return snapshot
}

func (service *Service) run(ctx context.Context) {
	targets, err := Search(ctx, service.options)
	service.mu.Lock()
	defer service.mu.Unlock()
	if err != nil {
		service.status = SearchStatusFailed
		service.targets = nil
		return
	}
	service.status = SearchStatusCompleted
	service.targets = targets
}
