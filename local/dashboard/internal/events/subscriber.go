// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package events subscribes to the CSC account and forwards observed messages to
// the dashboard hub. It authenticates via OAuth2 (client credentials) so the
// subscriber uses the same authorized path as any external application.
package events

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

// Sink receives observed events and connection-state updates.
type Sink interface {
	PushEvent(model.Event)
	SetEventConnected(bool)
	SetPower(model.Power)
	PushNotice(model.Notice)
}

// Options configures the event subscriber.
type Options struct {
	NATSURL         string
	Subject         string
	DropInbox       bool
	MaxPayloadBytes int

	IDPURL       string
	ClientID     string
	ClientSecret string
	Scope        string
}

// Subscriber holds the CSC NATS connection.
type Subscriber struct {
	nc   *nats.Conn
	sink Sink
	opts Options

	mu             sync.Mutex
	lastPower      model.Power
	havePower      bool
	breachStatus   string
	breachSeverity string
}

// Connect obtains an OAuth2 token source and dials NATS on the CSC account.
func Connect(ctx context.Context, opts Options, sink Sink) (*Subscriber, error) {
	if strings.HasSuffix(opts.IDPURL, "/") {
		return nil, fmt.Errorf("IDP URL %q must not end with /", opts.IDPURL)
	}
	ccfg := &clientcredentials.Config{
		ClientID:     opts.ClientID,
		ClientSecret: opts.ClientSecret,
		TokenURL:     opts.IDPURL + "/token",
		Scopes:       []string{opts.Scope},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	// TokenSource caches the token and refreshes it automatically before expiry.
	tokenSource := ccfg.TokenSource(context.WithoutCancel(ctx))

	// Validate we can obtain a token up front for a clear startup error.
	if _, err := tokenSource.Token(); err != nil {
		return nil, fmt.Errorf("obtain initial OAuth2 token: %w", err)
	}

	tokenFn := func() string {
		tok, err := tokenSource.Token()
		if err != nil {
			log.Printf("events: token refresh failed: %v", err)
			return ""
		}
		return tok.AccessToken
	}

	s := &Subscriber{sink: sink, opts: opts}
	natsOpts := []nats.Option{
		nats.TokenHandler(tokenFn),
		nats.Name("dsx-dashboard-events"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ConnectHandler(func(*nats.Conn) {
			log.Printf("events: connected to NATS (CSC account)")
			sink.SetEventConnected(true)
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("events: disconnected: %v", err)
			sink.SetEventConnected(false)
		}),
		nats.ReconnectHandler(func(*nats.Conn) {
			log.Printf("events: reconnected")
			sink.SetEventConnected(true)
		}),
	}

	nc, err := nats.Connect(opts.NATSURL, natsOpts...)
	if err != nil {
		return nil, fmt.Errorf("connect CSC account: %w", err)
	}
	s.nc = nc
	sink.SetEventConnected(true)
	return s, nil
}

// Run subscribes to the configured subject until ctx is cancelled.
func (s *Subscriber) Run(ctx context.Context) error {
	sub, err := s.nc.Subscribe(s.opts.Subject, s.handle)
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", s.opts.Subject, err)
	}
	log.Printf("events: subscribed to %q", s.opts.Subject)
	<-ctx.Done()
	_ = sub.Unsubscribe()
	return nil
}

// Close drains the NATS connection.
func (s *Subscriber) Close() {
	if s.nc != nil {
		_ = s.nc.Drain()
	}
}

func (s *Subscriber) handle(msg *nats.Msg) {
	if s.opts.DropInbox && strings.HasPrefix(msg.Subject, "_INBOX.") {
		return
	}
	// DSX Flex power-control subjects and inference telemetry drive the power UI
	// rather than the raw event feed.
	if s.dispatchPower(msg.Subject, msg.Data) {
		return
	}
	payload := msg.Data
	truncated := false
	if s.opts.MaxPayloadBytes > 0 && len(payload) > s.opts.MaxPayloadBytes {
		payload = payload[:s.opts.MaxPayloadBytes]
		truncated = true
	}
	text := string(payload)
	if truncated {
		text += "\u2026"
	}
	s.sink.PushEvent(model.Event{
		Subject: msg.Subject,
		Payload: text,
		Size:    len(msg.Data),
		Time:    time.Now().UTC(),
	})
}
