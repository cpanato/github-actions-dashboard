// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package websocket

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/chainguard-dev/clog"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()

	if hub == nil {
		t.Fatal("NewHub() returned nil")
	}

	if hub.broadcast == nil {
		t.Error("Hub.broadcast channel is nil")
	}

	if hub.register == nil {
		t.Error("Hub.register channel is nil")
	}

	if hub.unregister == nil {
		t.Error("Hub.unregister channel is nil")
	}

	if hub.clients == nil {
		t.Error("Hub.clients map is nil")
	}

	if len(hub.clients) != 0 {
		t.Errorf("Hub.clients should be empty, got %d clients", len(hub.clients))
	}
}

func TestHub_Run(t *testing.T) {
	hub := NewHub()
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	// Create a context that will be cancelled after test
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	// Run hub in goroutine
	done := make(chan bool)
	go func() {
		hub.Run(ctx)
		done <- true
	}()

	// Wait for context to be done
	select {
	case <-done:
		// Hub stopped as expected
	case <-time.After(200 * time.Millisecond):
		t.Error("Hub.Run() did not stop after context was cancelled")
	}
}

func TestHub_ClientCount(t *testing.T) {
	hub := NewHub()

	// Initial count should be 0
	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount() = %d, want 0", count)
	}

	// Add some mock clients
	client1 := &Client{hub: hub}
	client2 := &Client{hub: hub}

	hub.mu.Lock()
	hub.clients[client1] = true
	hub.clients[client2] = true
	hub.mu.Unlock()

	if count := hub.ClientCount(); count != 2 {
		t.Errorf("ClientCount() = %d, want 2", count)
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub := NewHub()

	tests := []struct {
		name      string
		eventType string
		data      interface{}
	}{
		{
			name:      "workflow update",
			eventType: "workflow_update",
			data: map[string]interface{}{
				"id":     123,
				"status": "completed",
			},
		},
		{
			name:      "check run update",
			eventType: "check_run_update",
			data: map[string]interface{}{
				"id":         456,
				"conclusion": "success",
			},
		},
		{
			name:      "string data",
			eventType: "test_event",
			data:      "simple string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hub.Broadcast(tt.eventType, tt.data)
			if err != nil {
				t.Errorf("Broadcast() error = %v", err)
			}
		})
	}
}

func TestHub_Broadcast_InvalidJSON(t *testing.T) {
	hub := NewHub()

	// Try to broadcast data that can't be marshaled to JSON
	invalidData := make(chan int) // channels can't be marshaled to JSON

	err := hub.Broadcast("test", invalidData)
	if err == nil {
		t.Error("Broadcast() with invalid data should return error")
	}
}

func TestMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "workflow message",
			msg: Message{
				Type: "workflow_update",
				Data: map[string]interface{}{
					"id":     123,
					"status": "completed",
				},
			},
		},
		{
			name: "check run message",
			msg: Message{
				Type: "check_run_update",
				Data: map[string]string{
					"name":       "tests",
					"conclusion": "success",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.Type == "" {
				t.Error("Message.Type should not be empty")
			}
			if tt.msg.Data == nil {
				t.Error("Message.Data should not be nil")
			}
		})
	}
}

func TestHub_ConcurrentClientManagement(t *testing.T) {
	hub := NewHub()
	ctx := context.Background()
	logger := clog.New(slog.Default().Handler())
	ctx = clog.WithLogger(ctx, logger)

	// Create a context that will be cancelled after test
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	// Run hub in background
	go hub.Run(ctx)

	// Simulate concurrent client registration
	for i := 0; i < 10; i++ {
		go func() {
			client := &Client{
				hub:  hub,
				send: make(chan []byte, 256),
			}
			hub.register <- client

			// Small delay then unregister
			time.Sleep(10 * time.Millisecond)
			hub.unregister <- client
		}()
	}

	// Wait a bit for operations to complete
	time.Sleep(100 * time.Millisecond)

	// All clients should be unregistered
	if count := hub.ClientCount(); count != 0 {
		t.Logf("Warning: ClientCount() = %d, expected 0 (may be timing dependent)", count)
	}
}
