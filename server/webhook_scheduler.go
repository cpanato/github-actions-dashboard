package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/palantir/go-githubapp/githubapp"
)

type webhookJob struct {
	ctx      context.Context
	dispatch githubapp.Dispatch
}

// webhookScheduler owns its queue and workers so shutdown can drain accepted work.
type webhookScheduler struct {
	mu      sync.Mutex
	stopped bool
	queue   chan webhookJob
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
}

func newWebhookScheduler(capacity, workers int) *webhookScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &webhookScheduler{queue: make(chan webhookJob, capacity), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range s.queue {
				s.execute(job)
			}
		}()
	}
	go func() { wg.Wait(); close(s.done) }()
	return s
}

func (s *webhookScheduler) Schedule(ctx context.Context, d githubapp.Dispatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return githubapp.ErrCapacityExceeded
	}
	select {
	case s.queue <- webhookJob{ctx: context.WithoutCancel(ctx), dispatch: d}:
		return nil
	default:
		return githubapp.ErrCapacityExceeded
	}
}

func (s *webhookScheduler) execute(job webhookJob) {
	ctx, cancel := context.WithTimeout(job.ctx, 30*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("webhook handler panicked: %v", recovered)
		}
		if err != nil {
			githubapp.DefaultAsyncErrorCallback(ctx, job.dispatch, err)
		}
	}()
	if err = s.ctx.Err(); err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = job.dispatch.Execute(ctx)
	}
}

func (s *webhookScheduler) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.queue)
	}
	s.mu.Unlock()
	defer s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.cancel()
		// Handlers use cancellable database operations. Do not close their pool until
		// active handlers have returned and queued jobs have observed cancellation.
		<-s.done
		return ctx.Err()
	}
}
