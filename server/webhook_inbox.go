package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/palantir/go-githubapp/githubapp"
)

var (
	errInboxUnavailable = errors.New("webhook inbox unavailable")
	errDeliveryConflict = errors.New("delivery ID already contains a different event")
)

type inboxDelivery struct {
	id, event string
	payload   []byte
	attempt   int
}

// webhookInbox persists accepted work and claims it with PostgreSQL leases.
type webhookInbox struct {
	db       *sql.DB
	handlers map[string]githubapp.EventHandler
	ready    func() bool
	mu       sync.Mutex
	stopped  bool
	stopping chan struct{}
	wake     chan struct{}
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

func newWebhookInbox(db *sql.DB, handlers []githubapp.EventHandler, workers int, ready func() bool) *webhookInbox {
	ctx, cancel := context.WithCancel(context.Background())
	s := &webhookInbox{db: db, handlers: make(map[string]githubapp.EventHandler), ready: ready,
		stopping: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	for i := len(handlers) - 1; i >= 0; i-- {
		for _, event := range handlers[i].Handles() {
			s.handlers[event] = handlers[i]
		}
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() { defer wg.Done(); s.run() }()
	}
	go func() { wg.Wait(); close(s.done) }()
	return s
}

// Schedule runs only after the dispatcher has verified the webhook signature.
func (s *webhookInbox) Schedule(ctx context.Context, d githubapp.Dispatch) error {
	if d.DeliveryID == "" || len(d.DeliveryID) > 255 {
		return githubapp.ValidationError{Cause: errors.New("delivery ID is required and must be at most 255 bytes")}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.ready() {
		return errInboxUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	digest := sha256.Sum256(d.Payload)
	hash := hex.EncodeToString(digest[:])
	var storedEvent, storedHash string
	err := s.db.QueryRowContext(ctx, `INSERT INTO webhook_deliveries(delivery_id,event_type,payload,payload_hash)
 VALUES($1,$2,$3,$4) ON CONFLICT(delivery_id) DO UPDATE SET delivery_id=EXCLUDED.delivery_id
 RETURNING event_type,payload_hash`, d.DeliveryID, d.EventType, d.Payload, hash).Scan(&storedEvent, &storedHash)
	if err != nil {
		return fmt.Errorf("%w: %w", errInboxUnavailable, err)
	}
	if storedEvent != d.EventType || storedHash != hash {
		return errDeliveryConflict
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

func (s *webhookInbox) claim(ctx context.Context) (inboxDelivery, error) {
	var d inboxDelivery
	err := s.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT delivery_id FROM webhook_deliveries
 WHERE (state='pending' AND available_at<=NOW()) OR (state='processing' AND locked_until<=NOW())
 ORDER BY received_at,delivery_id FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE webhook_deliveries w SET state='processing',attempts=attempts+1,
 locked_until=NOW()+INTERVAL '60 seconds',updated_at=NOW()
 FROM candidate c WHERE w.delivery_id=c.delivery_id
 RETURNING w.delivery_id,w.event_type,w.payload,w.attempts`).Scan(&d.id, &d.event, &d.payload, &d.attempt)
	return d, err
}

func (s *webhookInbox) run() {
	for {
		select {
		case <-s.stopping:
			return
		default:
		}
		if s.ready() {
			ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
			delivery, err := s.claim(ctx)
			cancel()
			if err == nil {
				s.execute(delivery)
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				clog.FromContext(s.ctx).Debug("Webhook inbox claim failed", "error", err)
			}
		}
		select {
		case <-s.stopping:
			return
		case <-s.wake:
		case <-time.After(time.Second):
		}
	}
}

func (s *webhookInbox) execute(d inboxDelivery) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	err := s.handle(ctx, d)
	cancel()
	state, lastError := "succeeded", ""
	if err != nil {
		state, lastError = "failed", err.Error()
		if s.ctx.Err() != nil {
			state = "pending"
		}
		clog.FromContext(s.ctx).Warn("Webhook processing failed", "delivery_id", d.id, "event", d.event, "attempt", d.attempt, "error", err)
	}
	// Use a separate bounded context to release a lease even during shutdown.
	completeCtx, completeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer completeCancel()
	_, completeErr := s.db.ExecContext(completeCtx, `UPDATE webhook_deliveries SET state=$3,last_error=$4,
 locked_until=NULL,updated_at=NOW(),completed_at=CASE WHEN $3='succeeded' THEN NOW() ELSE NULL END,
 payload=CASE WHEN $3='succeeded' THEN NULL ELSE payload END
 WHERE delivery_id=$1 AND attempts=$2 AND state='processing'`, d.id, d.attempt, state, lastError)
	if completeErr != nil {
		clog.FromContext(s.ctx).Error("Failed to complete webhook lease", "delivery_id", d.id, "error", completeErr)
	}
}

func (s *webhookInbox) handle(ctx context.Context, d inboxDelivery) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("webhook handler panicked: %v", recovered)
		}
	}()
	handler, ok := s.handlers[d.event]
	if !ok {
		return fmt.Errorf("no handler for event %q", d.event)
	}
	return handler.Handle(ctx, d.event, d.id, d.payload)
}

func (s *webhookInbox) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopping)
	}
	s.mu.Unlock()
	defer s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.cancel()
		<-s.done
		return ctx.Err()
	}
}
