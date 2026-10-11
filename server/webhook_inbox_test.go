package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cpanato/github-actions-dashboard/internal/testdb"
	"github.com/palantir/go-githubapp/githubapp"
)

type inboxTestHandler func(context.Context, string, []byte) error

func (h inboxTestHandler) Handles() []string { return []string{"workflow_run"} }
func (h inboxTestHandler) Handle(ctx context.Context, _, id string, payload []byte) error {
	return h(ctx, id, payload)
}

func testInbox(t *testing.T, workers int, h githubapp.EventHandler) *webhookInbox {
	t.Helper()
	db := testdb.Open(t)
	if err := initializeDatabase(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	inbox := newWebhookInbox(db, []githubapp.EventHandler{h}, workers, func() bool { return true })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := inbox.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return inbox
}

func waitDelivery(t *testing.T, s *webhookInbox, id, state string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var actual string
		var attempts int
		err := s.db.QueryRow(`SELECT state,attempts FROM webhook_deliveries WHERE delivery_id=$1`, id).Scan(&actual, &attempts)
		if err != nil {
			t.Fatal(err)
		}
		if actual == state {
			return attempts
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("delivery %s did not reach %s", id, state)
	return 0
}

func TestPostgresInboxPersistsBeforeAcknowledgingAndResumes(t *testing.T) {
	payload := []byte(`{"action":"completed"}`)
	calls := make(chan string, 2)
	handler := inboxTestHandler(func(_ context.Context, id string, body []byte) error {
		if !bytes.Equal(body, payload) {
			return errors.New("payload changed")
		}
		calls <- id
		return nil
	})
	s := testInbox(t, 0, handler)
	d := githubapp.Dispatch{EventType: "workflow_run", DeliveryID: "restart", Payload: payload}
	if err := s.Schedule(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if attempts := waitDelivery(t, s, "restart", "pending"); attempts != 0 {
		t.Fatal("delivery ran before workers started")
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := newWebhookInbox(s.db, []githubapp.EventHandler{handler}, 1, func() bool { return true })
	defer next.Close(t.Context())
	if attempts := waitDelivery(t, next, "restart", "succeeded"); attempts != 1 {
		t.Fatalf("attempts=%d", attempts)
	}
	if err := next.Schedule(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal("duplicate delivery executed again")
	}
	var retained []byte
	if err := s.db.QueryRow(`SELECT payload FROM webhook_deliveries WHERE delivery_id='restart'`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != nil {
		t.Fatal("successful payload was not cleared")
	}
	d.Payload = []byte(`{"changed":true}`)
	if err := next.Schedule(t.Context(), d); !errors.Is(err, errDeliveryConflict) {
		t.Fatalf("delivery ID conflict: %v", err)
	}
	d.DeliveryID = ""
	if err := next.Schedule(t.Context(), d); err == nil {
		t.Fatal("missing delivery ID accepted")
	}
}

func TestPostgresInboxConcurrentClaimsAndLeaseRecovery(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	handler := inboxTestHandler(func(_ context.Context, id string, _ []byte) error {
		mu.Lock()
		defer mu.Unlock()
		calls[id]++
		return nil
	})
	s := testInbox(t, 0, handler)
	for i := range 20 {
		if err := s.Schedule(t.Context(), githubapp.Dispatch{EventType: "workflow_run", DeliveryID: fmt.Sprintf("event-%d", i), Payload: []byte("{}")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE webhook_deliveries SET state='processing',attempts=1,locked_until=NOW()-INTERVAL '1 second' WHERE delivery_id='event-0'`); err != nil {
		t.Fatal(err)
	}
	a := newWebhookInbox(s.db, []githubapp.EventHandler{handler}, 2, func() bool { return true })
	defer a.Close(t.Context())
	b := newWebhookInbox(s.db, []githubapp.EventHandler{handler}, 2, func() bool { return true })
	defer b.Close(t.Context())
	for i := range 20 {
		attempts := waitDelivery(t, s, fmt.Sprintf("event-%d", i), "succeeded")
		want := 1
		if i == 0 {
			want = 2
		}
		if attempts != want {
			t.Fatalf("event-%d attempts=%d want=%d", i, attempts, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 20 {
		t.Fatalf("processed %d deliveries", len(calls))
	}
	for id, count := range calls {
		if count != 1 {
			t.Fatalf("%s executed %d times", id, count)
		}
	}
}

func TestPostgresInboxFailureAndStaleLease(t *testing.T) {
	s := testInbox(t, 0, inboxTestHandler(func(context.Context, string, []byte) error { return errors.New("temporary failure") }))
	if err := s.Schedule(t.Context(), githubapp.Dispatch{EventType: "workflow_run", DeliveryID: "failed", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	first, err := s.claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE webhook_deliveries SET locked_until=NOW()-INTERVAL '1 second' WHERE delivery_id='failed'`); err != nil {
		t.Fatal(err)
	}
	second, err := s.claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.execute(first)
	if attempts := waitDelivery(t, s, "failed", "processing"); attempts != 2 {
		t.Fatalf("stale completion changed current lease: %d", attempts)
	}
	s.execute(second)
	waitDelivery(t, s, "failed", "failed")
	var retained []byte
	var message string
	if err := s.db.QueryRow(`SELECT payload,last_error FROM webhook_deliveries WHERE delivery_id='failed'`).Scan(&retained, &message); err != nil {
		t.Fatal(err)
	}
	if string(retained) != "{}" || message != "temporary failure" {
		t.Fatal("failed delivery details lost")
	}
}

func TestPostgresInboxCloseFinishesActiveWork(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := testInbox(t, 1, inboxTestHandler(func(ctx context.Context, _ string, _ []byte) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	if err := s.Schedule(t.Context(), githubapp.Dispatch{EventType: "workflow_run", DeliveryID: "active", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	<-started
	finished := make(chan error, 1)
	go func() { finished <- s.Close(t.Context()) }()
	select {
	case <-s.stopping:
	case <-time.After(time.Second):
		t.Fatal("inbox did not stop")
	}
	if err := s.Schedule(t.Context(), githubapp.Dispatch{DeliveryID: "after-close"}); !errors.Is(err, errInboxUnavailable) {
		t.Fatalf("accepted work after shutdown: %v", err)
	}
	select {
	case err := <-finished:
		t.Fatalf("returned before active work completed: %v", err)
	default:
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	waitDelivery(t, s, "active", "succeeded")
}

func TestPostgresInboxCancelledShutdownRequeuesActiveWork(t *testing.T) {
	started := make(chan struct{})
	s := testInbox(t, 1, inboxTestHandler(func(ctx context.Context, _ string, _ []byte) error { close(started); <-ctx.Done(); return ctx.Err() }))
	if err := s.Schedule(t.Context(), githubapp.Dispatch{EventType: "workflow_run", DeliveryID: "interrupted", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
	waitDelivery(t, s, "interrupted", "pending")
}

func TestPostgresServerCloseFinishesInboxWrite(t *testing.T) {
	s, _ := integrationServer(t)
	if err := s.webhookInbox.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	written := make(chan error, 1)
	handler := inboxTestHandler(func(ctx context.Context, _ string, _ []byte) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO repositories(id,owner,name,full_name) VALUES(96000,'test','shutdown','test/shutdown')`)
		written <- err
		return err
	})
	s.webhookInbox = newWebhookInbox(s.db, []githubapp.EventHandler{handler}, 1, s.ready.Load)
	if err := s.webhookInbox.Schedule(t.Context(), githubapp.Dispatch{EventType: "workflow_run", DeliveryID: "shutdown-write", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	<-started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-s.webhookInbox.stopping:
	case <-time.After(time.Second):
		t.Fatal("server did not stop inbox")
	}
	close(release)
	if err := <-written; err != nil {
		t.Fatalf("database closed before accepted work: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}
