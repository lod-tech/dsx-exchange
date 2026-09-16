// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package config loads the mock inference service configuration from the
// environment. Defaults let the service run with zero configuration inside the
// csc-event-bus namespace as a DSX Flex agent.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds the inference-mock runtime configuration.
type Config struct {
	// HTTPAddr is the OpenAI-compatible API listen address.
	HTTPAddr string

	// AgentID identifies this DSX Flex agent (source URI + status subject).
	AgentID string

	// FeedTag is the power feed this workload represents.
	FeedTag string

	// Model is the advertised model name.
	Model string

	// Power model.
	PerRequestMW float64 // Power drawn by a single in-flight request (megawatts).
	PowerMaxMW   float64 // Facility ceiling used when no target is active.
	PowerMinMW   float64 // Minimum feed power (metadata).
	DefaultMW    float64 // Default constraint when no load target is active.

	// Admission control.
	MaxQueue     int           // Maximum queued (waiting) requests before shedding.
	MaxQueueWait time.Duration // Maximum time a request waits for a power slot.

	// Request simulation.
	PrefillLatency   time.Duration // Simulated time-to-first-token.
	TokensPerSec     float64       // Simulated token generation rate.
	DefaultMaxTokens int           // Tokens generated when the request omits max_tokens.

	// NATS + OAuth2 (DSX Flex event plane).
	NATSURL           string
	OAuthIDPURL       string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthScope        string

	// Publishing cadence.
	StatusInterval    time.Duration // powerstate.status heartbeat.
	TelemetryInterval time.Duration // dsx.inference telemetry cadence.
}

// FromEnv builds a Config from environment variables, applying local defaults.
func FromEnv() (*Config, error) {
	c := &Config{
		HTTPAddr:          env("INFERENCE_HTTP_ADDR", ":8080"),
		AgentID:           env("INFERENCE_AGENT_ID", "maxlps"),
		FeedTag:           env("INFERENCE_FEED_TAG", "ai-factory-main"),
		Model:             env("INFERENCE_MODEL", "dsx-mock-llm"),
		PerRequestMW:      envFloat("INFERENCE_PER_REQUEST_MW", 0.001),
		PowerMaxMW:        envFloat("INFERENCE_POWER_MAX_MW", 96.0),
		PowerMinMW:        envFloat("INFERENCE_POWER_MIN_MW", 0.0),
		DefaultMW:         envFloat("INFERENCE_DEFAULT_MW", 96.0),
		MaxQueue:          envInt("INFERENCE_MAX_QUEUE", 256),
		MaxQueueWait:      envDuration("INFERENCE_MAX_QUEUE_WAIT", 5*time.Second),
		PrefillLatency:    envDuration("INFERENCE_PREFILL_LATENCY", 300*time.Millisecond),
		TokensPerSec:      envFloat("INFERENCE_TOKENS_PER_SEC", 40.0),
		DefaultMaxTokens:  envInt("INFERENCE_DEFAULT_MAX_TOKENS", 64),
		NATSURL:           env("INFERENCE_NATS_URL", "nats://nats:4222"),
		OAuthIDPURL:       env("INFERENCE_OAUTH_IDP_URL", "http://event-bus.idp.svc.cluster.local:5556"),
		OAuthClientID:     env("INFERENCE_OAUTH_CLIENT_ID", "inference"),
		OAuthClientSecret: env("INFERENCE_OAUTH_CLIENT_SECRET", "inference-secret"),
		OAuthScope:        env("INFERENCE_OAUTH_SCOPE", "mqtt"),
		StatusInterval:    envDuration("INFERENCE_STATUS_INTERVAL", 2*time.Second),
		TelemetryInterval: envDuration("INFERENCE_TELEMETRY_INTERVAL", 1*time.Second),
	}

	if c.PerRequestMW <= 0 {
		return nil, fmt.Errorf("INFERENCE_PER_REQUEST_MW must be positive")
	}
	if c.PowerMaxMW < c.PerRequestMW {
		return nil, fmt.Errorf("INFERENCE_POWER_MAX_MW must be >= INFERENCE_PER_REQUEST_MW")
	}
	if c.TokensPerSec <= 0 {
		return nil, fmt.Errorf("INFERENCE_TOKENS_PER_SEC must be positive")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
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

func envFloat(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%g", &f); err != nil {
		return def
	}
	return f
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

// TrimIDPURL validates the IdP URL has no trailing slash.
func (c *Config) TrimIDPURL() error {
	if strings.HasSuffix(c.OAuthIDPURL, "/") {
		return fmt.Errorf("INFERENCE_OAUTH_IDP_URL %q must not end with /", c.OAuthIDPURL)
	}
	return nil
}
