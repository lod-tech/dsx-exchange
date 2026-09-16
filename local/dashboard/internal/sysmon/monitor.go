// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package sysmon monitors live client connections through the NATS system
// account. It subscribes to connect/disconnect lifecycle events and periodically
// polls the full connection roster via $SYS.REQ.SERVER.PING.CONNZ.
package sysmon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

const (
	connectSubject    = "$SYS.ACCOUNT.*.CONNECT"
	disconnectSubject = "$SYS.ACCOUNT.*.DISCONNECT"
	connzRequest      = "$SYS.REQ.SERVER.PING.CONNZ"
	connzGather       = 750 * time.Millisecond
)

// Sink receives connection state derived from the system account.
type Sink interface {
	SetConnections([]model.Connection)
	PushLifecycle(model.LifecycleEvent)
	SetSysConnected(bool)
}

// Monitor holds the SYS-account NATS connection and polling loop.
type Monitor struct {
	nc       *nats.Conn
	sink     Sink
	interval time.Duration
	refresh  chan struct{}
}

// Connect dials NATS on the system account using the provided NKey seed file.
func Connect(natsURL, seedFile string, sink Sink, interval time.Duration) (*Monitor, error) {
	nkeyOpt, err := nats.NkeyOptionFromSeed(seedFile)
	if err != nil {
		return nil, fmt.Errorf("load SYS nkey seed: %w", err)
	}

	m := &Monitor{sink: sink, interval: interval, refresh: make(chan struct{}, 1)}

	opts := []nats.Option{
		nkeyOpt,
		nats.Name("dsx-dashboard-sys"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ConnectHandler(func(*nats.Conn) {
			log.Printf("sysmon: connected to NATS system account")
			sink.SetSysConnected(true)
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("sysmon: disconnected: %v", err)
			sink.SetSysConnected(false)
		}),
		nats.ReconnectHandler(func(*nats.Conn) {
			log.Printf("sysmon: reconnected")
			sink.SetSysConnected(true)
		}),
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect SYS account: %w", err)
	}
	m.nc = nc
	sink.SetSysConnected(true)
	return m, nil
}

// Run subscribes to lifecycle events and polls the connection roster until ctx
// is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	if _, err := m.nc.Subscribe(connectSubject, m.handleLifecycle("connect")); err != nil {
		return fmt.Errorf("subscribe connect: %w", err)
	}
	if _, err := m.nc.Subscribe(disconnectSubject, m.handleLifecycle("disconnect")); err != nil {
		return fmt.Errorf("subscribe disconnect: %w", err)
	}

	// Prime the roster immediately, then poll on the configured interval.
	m.pollConnz()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.pollConnz()
		case <-m.refresh:
			m.pollConnz()
		}
	}
}

// Close drains the NATS connection.
func (m *Monitor) Close() {
	if m.nc != nil {
		_ = m.nc.Drain()
	}
}

func (m *Monitor) handleLifecycle(kind string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		account := accountFromSubject(msg.Subject)
		var ev struct {
			Client clientInfo `json:"client"`
			Reason string     `json:"reason"`
		}
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			log.Printf("sysmon: decode %s event: %v", kind, err)
			return
		}
		ip := ev.Client.IP
		if ip == "" {
			ip = ev.Client.Host
		}
		m.sink.PushLifecycle(model.LifecycleEvent{
			Kind:    kind,
			Account: account,
			Name:    ev.Client.Name,
			CID:     ev.Client.CID,
			IP:      ip,
			Reason:  ev.Reason,
			Time:    time.Now().UTC(),
		})
		// A lifecycle change means the roster is stale; refresh soon.
		select {
		case m.refresh <- struct{}{}:
		default:
		}
	}
}

// pollConnz requests CONNZ from every server and aggregates the roster.
func (m *Monitor) pollConnz() {
	inbox := m.nc.NewRespInbox()
	sub, err := m.nc.SubscribeSync(inbox)
	if err != nil {
		log.Printf("sysmon: connz inbox subscribe: %v", err)
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := m.nc.PublishRequest(connzRequest, inbox, []byte(`{"subscriptions":false}`)); err != nil {
		log.Printf("sysmon: connz request: %v", err)
		return
	}

	deadline := time.Now().Add(connzGather)
	var conns []model.Connection
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			break // timeout: no more server responses
		}
		var resp connzResponse
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			log.Printf("sysmon: decode connz: %v", err)
			continue
		}
		serverName := resp.Server.Name
		if serverName == "" {
			serverName = resp.Server.ID
		}
		for _, ci := range resp.Data.Connections {
			conns = append(conns, ci.toModel(serverName))
		}
	}

	sort.Slice(conns, func(i, j int) bool {
		if conns[i].Account != conns[j].Account {
			return conns[i].Account < conns[j].Account
		}
		return conns[i].Start.Before(conns[j].Start)
	})
	m.sink.SetConnections(conns)
}

func accountFromSubject(subject string) string {
	// $SYS.ACCOUNT.<account>.CONNECT
	parts := strings.Split(subject, ".")
	if len(parts) >= 4 && parts[0] == "$SYS" && parts[1] == "ACCOUNT" {
		return parts[2]
	}
	return ""
}
