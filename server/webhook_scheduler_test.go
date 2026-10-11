package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palantir/go-githubapp/githubapp"
)

type schedulerTestHandler func(context.Context) error

func (h schedulerTestHandler) Handles() []string                                       { return []string{"workflow_run"} }
func (h schedulerTestHandler) Handle(ctx context.Context, _, _ string, _ []byte) error { return h(ctx) }

func waitSchedulerStopped(t *testing.T, s *webhookScheduler) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("scheduler did not stop accepting work")
		}
	}
}

func TestWebhookSchedulerDrainsAndRejectsNewWork(t *testing.T) {
	s := newWebhookScheduler(1, 1)
	started, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	d := githubapp.Dispatch{Handler: schedulerTestHandler(func(ctx context.Context) error {
		if count.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})}
	if err := s.Schedule(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Schedule(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(t.Context(), d); !errors.Is(err, githubapp.ErrCapacityExceeded) {
		t.Fatalf("full queue: %v", err)
	}
	finished := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { finished <- s.Close(ctx) }()
	waitSchedulerStopped(t, s)
	if err := s.Schedule(t.Context(), d); !errors.Is(err, githubapp.ErrCapacityExceeded) {
		t.Fatalf("shutdown accepted work: %v", err)
	}
	select {
	case err := <-finished:
		t.Fatalf("returned before drain: %v", err)
	default:
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatalf("executed %d deliveries", count.Load())
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookSchedulerCancelsOnDrainDeadline(t *testing.T) {
	s := newWebhookScheduler(1, 1)
	started := make(chan struct{})
	if err := s.Schedule(t.Context(), githubapp.Dispatch{Handler: schedulerTestHandler(func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() })}); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain cancellation: %v", err)
	}
}

func TestPostgresServerCloseDrainsBeforeClosingDatabase(t *testing.T) {
	s, _ := integrationServer(t)
	started, release := make(chan struct{}), make(chan struct{})
	written := make(chan error, 1)
	if err := s.webhookScheduler.Schedule(t.Context(), githubapp.Dispatch{Handler: schedulerTestHandler(func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO repositories(id,owner,name,full_name) VALUES(96000,'test','shutdown','test/shutdown')`)
		written <- err
		return err
	})}); err != nil {
		t.Fatal(err)
	}
	<-started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitSchedulerStopped(t, s.webhookScheduler)
	close(release)
	if err := <-written; err != nil {
		t.Fatalf("database closed before accepted delivery: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}
