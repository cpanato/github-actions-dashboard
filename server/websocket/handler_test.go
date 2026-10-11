package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
