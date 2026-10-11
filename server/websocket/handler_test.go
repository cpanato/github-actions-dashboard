package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://dashboard.example/ws", nil)
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	w := httptest.NewRecorder()
	NewHub().ServeWS(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
}

func TestWebSocketSessionExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hub := NewHub()
	go hub.Run(ctx)
	expires := time.Now().Add(500 * time.Millisecond)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hub.ServeWSUntil(w, r, expires) }))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitForSocketRegistration(t, hub)
	if err := hub.Broadcast("before_expiry", nil); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message Message
	if err := conn.ReadJSON(&message); err != nil || message.Type != "before_expiry" {
		t.Fatalf("before expiry: %+v, %v", message, err)
	}
	// Heartbeats must not extend the session deadline.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
				return
			}
		}
	}()
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("expected session expiry close, got %v", err)
	}
	conn.Close()
	<-done
}

func TestWebSocketPastSessionRejected(t *testing.T) {
	w := httptest.NewRecorder()
	NewHub().ServeWSUntil(w, httptest.NewRequest(http.MethodGet, "/ws", nil), time.Now().Add(-time.Second))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", w.Code)
	}
}

func TestWebSocketWithoutSessionDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hub := NewHub()
	go hub.Run(ctx)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitForSocketRegistration(t, hub)
	if err := hub.Broadcast("demo", nil); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var message Message
	if err := conn.ReadJSON(&message); err != nil || message.Type != "demo" {
		t.Fatalf("demo socket: %+v %v", message, err)
	}
}

// A successful upgrade precedes hub registration. Wait for the subscriber before
// broadcasting so the test does not depend on goroutine scheduling.
func waitForSocketRegistration(t *testing.T, hub *Hub) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for hub.ClientCount() != 1 {
		select {
		case <-deadline.C:
			t.Fatal("WebSocket client was not registered")
		case <-ticker.C:
		}
	}
}
