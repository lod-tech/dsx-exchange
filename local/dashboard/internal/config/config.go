// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package config loads the DSX Live Dashboard runtime configuration from the
// environment. All values have local-development defaults so the dashboard runs
// with zero configuration inside the csc-event-bus namespace.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds the dashboard runtime configuration.
type Config struct {
	// HTTPAddr is the listen address for the web UI and WebSocket endpoint.
	HTTPAddr string

	// NATSURL is the NATS server URL used for both the SYS and CSC connections.
	NATSURL string

	// SysNKeySeedFile is the path to the SYS-account NKey seed (mounted from the
	// nats-surveyor secret). It authorizes connection-lifecycle monitoring.
	SysNKeySeedFile string

	// EventSubject is the subject the CSC event subscriber listens on.
	EventSubject string

	// DropInboxEvents drops _INBOX.> reply traffic from the event feed to reduce
	// noise from request/reply plumbing.
	DropInboxEvents bool

	// ConnzInterval is how often the full connection roster is polled via
	// $SYS.REQ.SERVER.PING.CONNZ.
	ConnzInterval time.Duration

	// EventFlushInterval is how often buffered events are flushed to browsers.
	EventFlushInterval time.Duration

	// EventBufferSize is the number of recent events retained for new clients.
	EventBufferSize int

	// MaxPayloadBytes is the maximum event payload size forwarded to the UI.
	MaxPayloadBytes int

	// OAuth settings for the CSC event subscriber (authorized subscribe path).
	OAuthIDPURL       string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthScope        string

	// Public connection info advertised in the "Connection info" dialog. These
	// are the addresses external systems use to reach the stack, so they must be
	// set to the public gateway/ingress/broker endpoints (not in-cluster DNS).
	// Locally they default to the documented port-forward addresses.
	PublicCompletionURL   string // OpenAI-compatible base URL, e.g. https://api.example.com/v1
	PublicCompletionModel string // model id served by the completion API
	PublicMQTTURL         string // MQTT broker URL, e.g. tls://mqtt.example.com:8883
	PublicOAuthTokenURL   string // OAuth2 token endpoint, e.g. https://idp.example.com/token
	FlexISVClientID       string // OAuth2 client id an ISV uses to publish targets
	FlexISVClientSecret   string // OAuth2 client secret for the ISV client
	FlexISVScope          string // OAuth2 scope required for MQTT publish
	FlexAgentID           string // DSX Flex agent id (drives feedback topics)
	FlexFeedTag           string // power feed tag the agent represents
}

// FromEnv builds a Config from environment variables, applying local defaults.
func FromEnv() (*Config, error) {
	c := &Config{
		HTTPAddr:           env("DASHBOARD_HTTP_ADDR", ":8080"),
		NATSURL:            env("DASHBOARD_NATS_URL", "nats://nats:4222"),
		SysNKeySeedFile:    env("DASHBOARD_SYS_NKEY_SEED_FILE", "/etc/dsx-dashboard/sys/seed"),
		EventSubject:       env("DASHBOARD_EVENT_SUBJECT", ">"),
		DropInboxEvents:    envBool("DASHBOARD_DROP_INBOX_EVENTS", true),
		ConnzInterval:      envDuration("DASHBOARD_CONNZ_INTERVAL", 2*time.Second),
		EventFlushInterval: envDuration("DASHBOARD_EVENT_FLUSH_INTERVAL", 250*time.Millisecond),
		EventBufferSize:    envInt("DASHBOARD_EVENT_BUFFER_SIZE", 500),
		MaxPayloadBytes:    envInt("DASHBOARD_MAX_PAYLOAD_BYTES", 4096),
		OAuthIDPURL:        env("DASHBOARD_OAUTH_IDP_URL", "http://event-bus.idp.svc.cluster.local:5556"),
		OAuthClientID:      env("DASHBOARD_OAUTH_CLIENT_ID", "dashboard"),
		OAuthClientSecret:  env("DASHBOARD_OAUTH_CLIENT_SECRET", "dashboard-secret"),
		OAuthScope:         env("DASHBOARD_OAUTH_SCOPE", "mqtt"),

		PublicCompletionURL:   env("DASHBOARD_PUBLIC_COMPLETION_URL", "http://localhost:8081/v1"),
		PublicCompletionModel: env("DASHBOARD_PUBLIC_COMPLETION_MODEL", "dsx-mock-llm"),
		PublicMQTTURL:         env("DASHBOARD_PUBLIC_MQTT_URL", "tcp://localhost:1883"),
		PublicOAuthTokenURL:   env("DASHBOARD_PUBLIC_OAUTH_TOKEN_URL", "http://localhost:5556/token"),
		FlexISVClientID:       env("DASHBOARD_FLEX_ISV_CLIENT_ID", "grid-isv"),
		FlexISVClientSecret:   env("DASHBOARD_FLEX_ISV_CLIENT_SECRET", "grid-isv-secret"),
		FlexISVScope:          env("DASHBOARD_FLEX_ISV_SCOPE", "mqtt"),
		FlexAgentID:           env("DASHBOARD_FLEX_AGENT_ID", "maxlps"),
		FlexFeedTag:           env("DASHBOARD_FLEX_FEED_TAG", "ai-factory-main"),
	}

	if c.EventSubject == "" {
		return nil, fmt.Errorf("DASHBOARD_EVENT_SUBJECT must not be empty")
	}
	if c.EventBufferSize <= 0 {
		return nil, fmt.Errorf("DASHBOARD_EVENT_BUFFER_SIZE must be positive")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
