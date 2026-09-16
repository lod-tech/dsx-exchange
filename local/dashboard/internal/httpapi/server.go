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

// ConnectionInfo is the public wiring information the UI shows external systems
// in the "Connection info" dialog. All addresses are the public endpoints a
// client outside the cluster uses (gateway/ingress/broker), never in-cluster
// service DNS.
type ConnectionInfo struct {
	Completion CompletionInfo `json:"completion"`
	Flex       FlexInfo       `json:"flex"`
}

// CompletionInfo describes the OpenAI-compatible completion API (for an EDN
// system).
type CompletionInfo struct {
	BaseURL   string   `json:"baseURL"`
	Model     string   `json:"model"`
	Endpoints []string `json:"endpoints"`
	Auth      string   `json:"auth"`
}

// FlexInfo describes the DSX Flex MQTT surface (for an energy system / ISV).
type FlexInfo struct {
	MQTTURL          string   `json:"mqttURL"`
	OAuthTokenURL    string   `json:"oauthTokenURL"`
	ClientID         string   `json:"clientID"`
	ClientSecret     string   `json:"clientSecret"`
	Scope            string   `json:"scope"`
	Grant            string   `json:"grant"`
	Username         string   `json:"username"`
	AgentID          string   `json:"agentID"`
	Feed             string   `json:"feed"`
	PublishTopic     string   `json:"publishTopic"`
	SubscribeTopics  []string `json:"subscribeTopics"`
	CloudEventType   string   `json:"cloudEventType"`
	CloudEventSource string   `json:"cloudEventSource"`
}

// New builds the HTTP server for the given address, hub, and connection info.
func New(addr string, h *hub.Hub, info ConnectionInfo) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.ServeWS)
	mux.HandleFunc("/healthz", healthHandler(h))
	mux.HandleFunc("/api/connection-info", connectionInfoHandler(info))
	mux.Handle("/", http.FileServer(http.FS(web.FS)))

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}

// connectionInfoHandler serves the static, config-derived public connection
// info as JSON for the UI dialog.
func connectionInfoHandler(info ConnectionInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(info)
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
