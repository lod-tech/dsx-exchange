// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

type fakeSink struct {
	events []model.Event
}

func (f *fakeSink) PushEvent(e model.Event) { f.events = append(f.events, e) }
func (f *fakeSink) SetEventConnected(bool)  {}

func TestHandleDropsInbox(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 1024}}
	s.handle(&nats.Msg{Subject: "_INBOX.abc", Data: []byte("x")})
	if len(sink.events) != 0 {
		t.Fatalf("expected inbox message dropped, got %d events", len(sink.events))
	}
}

func TestHandlePassesThrough(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 1024}}
	s.handle(&nats.Msg{Subject: "BMS.v1.PUB.Value.Rack", Data: []byte(`{"value":1}`)})
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Subject != "BMS.v1.PUB.Value.Rack" {
		t.Errorf("subject = %q", ev.Subject)
	}
	if ev.Payload != `{"value":1}` {
		t.Errorf("payload = %q", ev.Payload)
	}
	if ev.Size != len(`{"value":1}`) {
		t.Errorf("size = %d, want %d", ev.Size, len(`{"value":1}`))
	}
}

func TestHandleTruncatesLargePayload(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 4}}
	full := "0123456789"
	s.handle(&nats.Msg{Subject: "test.big", Data: []byte(full)})
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Size != len(full) {
		t.Errorf("size = %d, want %d (original length)", ev.Size, len(full))
	}
	if !strings.HasPrefix(ev.Payload, "0123") {
		t.Errorf("payload = %q, want truncated prefix", ev.Payload)
	}
	if !strings.HasSuffix(ev.Payload, "\u2026") {
		t.Errorf("payload = %q, want ellipsis suffix", ev.Payload)
	}
}
