// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package httpapi serves the dashboard web UI, the WebSocket stream, and a
// health endpoint.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/hub"
	"github.com/NVIDIA/dsx-exchange/local/dashboard/web"
)

// New builds the HTTP server for the given address and hub.
func New(addr string, h *hub.Hub) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.ServeWS)
	mux.HandleFunc("/healthz", healthHandler(h))
	mux.Handle("/", http.FileServer(http.FS(web.FS)))

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}

// healthHandler always returns 200 so the pod is considered live/ready as soon
// as the HTTP server is up. Upstream NATS/IdP connectivity is reported in the
// body (and in the UI status dots) rather than gating readiness, which avoids a
// startup ordering deadlock during local deploys.
func healthHandler(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		sys, event := h.ConnectionState()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"sysConnected":   sys,
			"eventConnected": event,
		})
	}
}
