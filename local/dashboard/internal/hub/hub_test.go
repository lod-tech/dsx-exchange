// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

func TestPushEventRingBufferAndSeq(t *testing.T) {
	h := New(250*time.Millisecond, time.Second, 3)
	for i := 0; i < 5; i++ {
		h.PushEvent(model.Event{Subject: "test.sub", Payload: "p", Size: 1})
	}

	envs := h.snapshotEnvelopes()
	var events []model.Event
	var stats *model.Stats
	for _, e := range envs {
		switch e.Type {
		case "events":
			events = e.Events
		case "stats":
			stats = e.Stats
		}
	}

	if len(events) != 3 {
		t.Fatalf("recent events buffer = %d, want 3 (capped)", len(events))
	}
	// The buffer retains the most recent events; seq must be monotonically last.
	if got := events[len(events)-1].Seq; got != 5 {
		t.Errorf("last event seq = %d, want 5", got)
	}
	if events[0].Seq != 3 {
		t.Errorf("first retained event seq = %d, want 3", events[0].Seq)
	}
	if stats == nil || stats.EventsTotal != 5 {
		t.Errorf("stats.EventsTotal = %v, want 5", stats)
	}
}

func TestFlushEventsClearsPending(t *testing.T) {
	h := New(250*time.Millisecond, time.Second, 10)
	h.PushEvent(model.Event{Subject: "a"})
	h.PushEvent(model.Event{Subject: "b"})

	h.mu.RLock()
	pending := len(h.pendingEvents)
	h.mu.RUnlock()
	if pending != 2 {
		t.Fatalf("pending before flush = %d, want 2", pending)
	}

	h.flushEvents()

	h.mu.RLock()
	pending = len(h.pendingEvents)
	h.mu.RUnlock()
	if pending != 0 {
		t.Errorf("pending after flush = %d, want 0", pending)
	}
}

func TestClientConnCount(t *testing.T) {
	h := New(250*time.Millisecond, time.Second, 10)
	h.SetConnections([]model.Connection{
		{CID: 1, Kind: "Client"},
		{CID: 2, Kind: "Leafnode"},
		{CID: 3, Kind: ""}, // treated as client
	})

	env := h.statsEnvelope()
	if env.Stats.Connections != 3 {
		t.Errorf("Connections = %d, want 3", env.Stats.Connections)
	}
	if env.Stats.ClientConns != 2 {
		t.Errorf("ClientConns = %d, want 2", env.Stats.ClientConns)
	}
}

func TestComputeRate(t *testing.T) {
	h := New(250*time.Millisecond, time.Second, 10)
	for i := 0; i < 10; i++ {
		h.PushEvent(model.Event{Subject: "x"})
	}
	h.computeRate(2 * time.Second)
	env := h.statsEnvelope()
	if env.Stats.EventsPerSecond != 5 {
		t.Errorf("EventsPerSecond = %v, want 5", env.Stats.EventsPerSecond)
	}
}
