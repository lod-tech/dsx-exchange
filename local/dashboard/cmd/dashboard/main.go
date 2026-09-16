// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command dashboard runs the DSX Live Dashboard: it monitors live client
// connections via the NATS system account, streams observed events from the CSC
// account, and serves a live web UI over WebSocket.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/config"
	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/events"
	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/httpapi"
	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/hub"
	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/sysmon"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("dashboard: invalid configuration: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := hub.New(cfg.EventFlushInterval, time.Second, cfg.EventBufferSize)
	go h.Run(ctx)

	// SYS-account connection monitor (retries until NATS is reachable).
	go runWithRetry(ctx, "sysmon", func(ctx context.Context) error {
		mon, err := sysmon.Connect(cfg.NATSURL, cfg.SysNKeySeedFile, h, cfg.ConnzInterval)
		if err != nil {
			return err
		}
		defer mon.Close()
		return mon.Run(ctx)
	})

	// CSC-account event subscriber (retries until NATS + IdP are reachable).
	go runWithRetry(ctx, "events", func(ctx context.Context) error {
		sub, err := events.Connect(ctx, events.Options{
			NATSURL:         cfg.NATSURL,
			Subject:         cfg.EventSubject,
			DropInbox:       cfg.DropInboxEvents,
			MaxPayloadBytes: cfg.MaxPayloadBytes,
			IDPURL:          cfg.OAuthIDPURL,
			ClientID:        cfg.OAuthClientID,
			ClientSecret:    cfg.OAuthClientSecret,
			Scope:           cfg.OAuthScope,
		}, h)
		if err != nil {
			return err
		}
		defer sub.Close()
		return sub.Run(ctx)
	})

	srv := httpapi.New(cfg.HTTPAddr, h, connectionInfo(cfg))
	go func() {
		log.Printf("dashboard: serving UI on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("dashboard: http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("dashboard: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("dashboard: http shutdown: %v", err)
	}
}

// connectionInfo builds the public wiring info advertised to external systems
// from the environment-configured public addresses.
func connectionInfo(cfg *config.Config) httpapi.ConnectionInfo {
	agent := cfg.FlexAgentID
	return httpapi.ConnectionInfo{
		Completion: httpapi.CompletionInfo{
			BaseURL:   cfg.PublicCompletionURL,
			Model:     cfg.PublicCompletionModel,
			Endpoints: []string{"/models", "/chat/completions", "/completions"},
			Auth:      "none",
		},
		Flex: httpapi.FlexInfo{
			MQTTURL:          cfg.PublicMQTTURL,
			OAuthTokenURL:    cfg.PublicOAuthTokenURL,
			ClientID:         cfg.FlexISVClientID,
			ClientSecret:     cfg.FlexISVClientSecret,
			Scope:            cfg.FlexISVScope,
			Grant:            "client_credentials",
			Username:         "oauthtoken",
			AgentID:          agent,
			Feed:             cfg.FlexFeedTag,
			PublishTopic:     "grid/v1/isv/<isv-id>/loadtarget/set",
			SubscribeTopics:  []string{"grid/v1/dsx-flex-agent/" + agent + "/powerstate/status", "grid/v1/dsx-flex-agent/" + agent + "/powerbreach"},
			CloudEventType:   "grid.loadtarget.set.v1",
			CloudEventSource: "//grid/v1/isv/<isv-id>",
		},
	}
}

// runWithRetry runs fn, restarting it after a short delay if it returns an error
// or exits, until ctx is cancelled.
func runWithRetry(ctx context.Context, name string, fn func(context.Context) error) {
	const retryDelay = 3 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := fn(ctx); err != nil {
			log.Printf("%s: %v (retrying in %s)", name, err, retryDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}
