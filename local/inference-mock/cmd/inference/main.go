// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command inference runs the mock AI inference service. It serves an
// OpenAI-compatible API where each in-flight request draws a fixed amount of
// power, and it acts as a DSX Flex agent: it enforces load targets received from
// the event bus and publishes power state, breach alerts, and telemetry back to
// the bus.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/agent"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/config"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/httpapi"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/power"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("inference: invalid configuration: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine := power.New(cfg.PerRequestMW, cfg.PowerMaxMW, cfg.PowerMinMW, cfg.DefaultMW, cfg.MaxQueue, cfg.MaxQueueWait)
	go engine.Run(ctx)

	// DSX Flex agent: enforce load targets and publish state (retries until the
	// bus and IdP are reachable).
	go runWithRetry(ctx, "agent", func(ctx context.Context) error {
		ag, err := agent.Connect(ctx, cfg, engine)
		if err != nil {
			return err
		}
		defer ag.Close()
		return ag.Run(ctx)
	})

	api := httpapi.New(engine, cfg.Model, cfg.PrefillLatency, cfg.TokensPerSec, cfg.DefaultMaxTokens)
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler()}
	go func() {
		log.Printf("inference: serving OpenAI-compatible API on %s (per-request %g MW, cap %.1f MW)",
			cfg.HTTPAddr, cfg.PerRequestMW, cfg.DefaultMW)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("inference: http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("inference: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("inference: http shutdown: %v", err)
	}
}

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
