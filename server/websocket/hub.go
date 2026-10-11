// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/gorilla/websocket"
)

// Hub maintains active WebSocket connections and broadcasts messages
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan []byte

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	mu   sync.RWMutex
	done chan struct{}
}

// Client represents a WebSocket client connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Message types for WebSocket communication
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// NewHub creates a new WebSocket hub
func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run(ctx context.Context) {
	logger := clog.FromContext(ctx)
	defer func() {
		close(h.done)
		h.mu.Lock()
		defer h.mu.Unlock()
		for client := range h.clients {
			close(client.send)
			if client.conn != nil {
				client.conn.Close()
			}
			delete(h.clients, client)
		}
	}()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logger.Debug("Client connected", "total_clients", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			logger.Debug("Client disconnected", "total_clients", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.send <- message:
				default:
					// Client's send channel is full, disconnect
					h.mu.Lock()
					close(client.send)
					delete(h.clients, client)
					h.mu.Unlock()
				}
			}

		case <-ctx.Done():
			logger.Info("Shutting down WebSocket hub")
			return
		}
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(msgType string, data interface{}) error {
	msg := Message{
		Type: msgType,
		Data: data,
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case <-h.done:
		return errors.New("websocket hub stopped")
	default:
	}
	select {
	case <-h.done:
		return errors.New("websocket hub stopped")
	case h.broadcast <- jsonData:
		return nil
	default:
		return errors.New("websocket broadcast queue full")
	}
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.done:
		}
		c.conn.Close()
	}()

	if err := c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		slog.Warn("Failed to set WebSocket read deadline", "error", err)
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("Unexpected WebSocket close", "error", err)
			}
			break
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				slog.Warn("Failed to set WebSocket write deadline", "error", err)
				return
			}
			if !ok {
				// Hub closed the channel
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					slog.Debug("Failed to write WebSocket close message", "error", err)
				}
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				slog.Warn("Failed to write WebSocket message", "error", err)
				if closeErr := w.Close(); closeErr != nil {
					slog.Debug("Failed to close WebSocket writer", "error", closeErr)
				}
				return
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				slog.Warn("Failed to set WebSocket write deadline", "error", err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
