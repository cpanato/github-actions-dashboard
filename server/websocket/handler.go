// Copyright 2025
//
// Licensed under the Apache License, Version 2.0

package websocket

import (
	"net/http"

	"github.com/chainguard-dev/clog"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		// Allow all origins for now (consider restricting in production)
		return true
	},
}

// ServeWS handles WebSocket requests from clients
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	logger := clog.FromContext(r.Context())

	logger.Info("WebSocket upgrade request received",
		"remote_addr", r.RemoteAddr,
		"url_path", r.URL.Path,
		"url_rawpath", r.URL.RawPath,
		"request_uri", r.RequestURI,
		"upgrade_header", r.Header.Get("Upgrade"),
		"connection_header", r.Header.Get("Connection"),
		"sec_websocket_version", r.Header.Get("Sec-WebSocket-Version"),
		"sec_websocket_key", r.Header.Get("Sec-WebSocket-Key"))

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade WebSocket connection",
			"error", err,
			"remote_addr", r.RemoteAddr,
			"headers", r.Header)
		return
	}

	logger.Info("WebSocket connection established", "remote_addr", r.RemoteAddr)

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}
	client.hub.register <- client

	// Start pumps in separate goroutines
	go client.writePump()
	go client.readPump()
}
