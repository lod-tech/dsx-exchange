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
	events  []model.Event
	powers  []model.Power
	notices []model.Notice
}

func (f *fakeSink) PushEvent(e model.Event)   { f.events = append(f.events, e) }
func (f *fakeSink) SetEventConnected(bool)    {}
func (f *fakeSink) SetPower(p model.Power)    { f.powers = append(f.powers, p) }
func (f *fakeSink) PushNotice(n model.Notice) { f.notices = append(f.notices, n) }

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

func TestHandleTelemetryDrivesPowerNotFeed(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 4096}}
	payload := `{"feed_tag":"ai-factory-main","rps":12.5,"power_mw":8,"effective_cap_mw":20,"target_active":true,"compliant":true}`
	s.handle(&nats.Msg{Subject: "dsx.inference.v1.telemetry", Data: []byte(payload)})
	if len(sink.events) != 0 {
		t.Fatalf("telemetry should not enter the raw feed, got %d events", len(sink.events))
	}
	if len(sink.powers) != 1 {
		t.Fatalf("expected 1 power update, got %d", len(sink.powers))
	}
	p := sink.powers[0]
	if p.RPS != 12.5 || p.PowerMW != 8 || p.TargetMW != 20 || !p.TargetActive || !p.Compliant {
		t.Errorf("unexpected power snapshot: %+v", p)
	}
}

func TestHandleLoadTargetEmitsNotice(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 4096}}
	ce := `{"type":"grid.loadtarget.set.v1","data":{"targets":[{"feed_tags":["ai-factory-main"],"load_constraint":{"value":20,"unit":"megawatt"}}]}}`
	s.handle(&nats.Msg{Subject: "grid.v1.isv.acme.loadtarget.set", Data: []byte(ce)})
	if len(sink.events) != 0 {
		t.Fatalf("load target should not enter the raw feed, got %d events", len(sink.events))
	}
	if len(sink.notices) != 1 {
		t.Fatalf("expected 1 notice, got %d", len(sink.notices))
	}
	if !strings.Contains(sink.notices[0].Text, "20.0 MW") {
		t.Errorf("notice text = %q", sink.notices[0].Text)
	}
}

func TestHandleBreachUpdatesPowerAndNotice(t *testing.T) {
	sink := &fakeSink{}
	s := &Subscriber{sink: sink, opts: Options{DropInbox: true, MaxPayloadBytes: 4096}}
	// Seed a power snapshot so breach can merge into it.
	s.handle(&nats.Msg{Subject: "dsx.inference.v1.telemetry", Data: []byte(`{"power_mw":30,"effective_cap_mw":20,"compliant":false}`)})
	ce := `{"type":"grid.powerbreach.alert.v1","data":{"feed_tag":"ai-factory-main","status":"active","severity":"critical","measured_load":{"value":30,"unit":"megawatt"},"target":{"load_constraint":{"value":20,"unit":"megawatt"}}}}`
	s.handle(&nats.Msg{Subject: "grid.v1.dsx-flex-agent.maxlps.powerbreach", Data: []byte(ce)})
	if len(sink.notices) != 1 || sink.notices[0].Level != "critical" {
		t.Fatalf("expected 1 critical breach notice, got %+v", sink.notices)
	}
	last := sink.powers[len(sink.powers)-1]
	if last.BreachStatus != "active" || last.BreachSeverity != "critical" {
		t.Errorf("breach not merged into power: %+v", last)
	}
}
