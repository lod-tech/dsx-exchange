// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package flex

import (
	"encoding/json"
	"testing"
)

func TestLoadTargetSetRoundTrip(t *testing.T) {
	in := SetLoadTargetData{
		Targets: []LoadTargetRequest{{
			FeedTags:       []string{"ai-factory-main"},
			LoadConstraint: &LoadConstraint{Value: 20, Unit: UnitMegawatt},
			Strategy:       &Strategy{BestEffort: true},
		}},
	}
	ev, err := NewEvent(TypeLoadTargetSet, ISVSource("acme"), "loadtarget", "corr-1", in)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if ev.SpecVersion != SpecVersion || ev.Source != "//grid/v1/isv/acme" {
		t.Errorf("unexpected envelope: %+v", ev)
	}
	b, err := ev.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	parsed, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != TypeLoadTargetSet || parsed.CorrelationID != "corr-1" {
		t.Errorf("unexpected parsed envelope: %+v", parsed)
	}
	var got SetLoadTargetData
	if err := parsed.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if len(got.Targets) != 1 || got.Targets[0].LoadConstraint.Megawatts() != 20 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestSubjectHelpers(t *testing.T) {
	if got := LoadTargetSetSubscribeSubject(); got != "grid.v1.isv.*.loadtarget.set" {
		t.Errorf("subscribe subject = %q", got)
	}
	if got := PowerStateStatusSubject("maxlps"); got != "grid.v1.dsx-flex-agent.maxlps.powerstate.status" {
		t.Errorf("status subject = %q", got)
	}
	if got := PowerBreachSubject("maxlps"); got != "grid.v1.dsx-flex-agent.maxlps.powerbreach" {
		t.Errorf("breach subject = %q", got)
	}
}

func TestDecodeRejectsJWS(t *testing.T) {
	ev := &Event{Type: "grid.loadtarget.set.v1", Data: json.RawMessage(`"aaaa.bbbb.cccc"`)}
	var got SetLoadTargetData
	if err := ev.DecodeData(&got); err == nil {
		t.Fatal("expected error decoding JWS-serialized data")
	}
}

func TestPowerUnitConversion(t *testing.T) {
	cases := []struct {
		unit string
		val  float64
		want float64
	}{
		{UnitMegawatt, 5, 5},
		{UnitKilowatt, 5000, 5},
		{UnitWatt, 5_000_000, 5},
	}
	for _, c := range cases {
		if got := (PowerUnit{Value: c.val, Unit: c.unit}).Megawatts(); got != c.want {
			t.Errorf("%s %v => %v MW, want %v", c.unit, c.val, got, c.want)
		}
	}
}
