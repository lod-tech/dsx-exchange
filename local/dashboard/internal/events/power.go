// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

// telemetrySubject is the supplementary inference telemetry stream.
const telemetrySubject = "dsx.inference.v1.telemetry"

// dispatchPower routes DSX Flex power-control subjects and inference telemetry to
// the power UI. It returns true when the subject was handled (and therefore
// should not appear in the raw event feed).
func (s *Subscriber) dispatchPower(subject string, data []byte) bool {
	switch {
	case subject == telemetrySubject:
		s.handleTelemetry(data)
		return true
	case strings.HasPrefix(subject, "grid.v1.dsx-flex-agent.") && strings.HasSuffix(subject, ".powerbreach"):
		s.handleBreach(data)
		return true
	case strings.HasPrefix(subject, "grid.v1.dsx-flex-agent.") && strings.HasSuffix(subject, ".powerstate.status"):
		s.handlePowerState(data)
		return true
	case strings.HasPrefix(subject, "grid.v1.isv.") && strings.HasSuffix(subject, ".loadtarget.set"):
		s.handleLoadTarget(subject, data)
		return true
	default:
		return false
	}
}

type telemetryMsg struct {
	FeedTag        string  `json:"feed_tag"`
	RPS            float64 `json:"rps"`
	AcceptedPerSec float64 `json:"accepted_per_sec"`
	ShedPerSec     float64 `json:"shed_per_sec"`
	InFlight       int     `json:"in_flight"`
	PerRequestMW   float64 `json:"per_request_mw"`
	PowerMW        float64 `json:"power_mw"`
	TargetMW       float64 `json:"target_mw"`
	EffectiveCapMW float64 `json:"effective_cap_mw"`
	TargetActive   bool    `json:"target_active"`
	Compliant      bool    `json:"compliant"`
}

func (s *Subscriber) handleTelemetry(data []byte) {
	var t telemetryMsg
	if err := json.Unmarshal(data, &t); err != nil {
		return
	}
	s.mu.Lock()
	s.lastPower = model.Power{
		Time:           time.Now().UTC(),
		FeedTag:        t.FeedTag,
		RPS:            t.RPS,
		AcceptedPerSec: t.AcceptedPerSec,
		ShedPerSec:     t.ShedPerSec,
		InFlight:       t.InFlight,
		PerRequestMW:   t.PerRequestMW,
		PowerMW:        t.PowerMW,
		TargetMW:       t.EffectiveCapMW,
		TargetActive:   t.TargetActive,
		Compliant:      t.Compliant,
		BreachStatus:   s.breachStatus,
		BreachSeverity: s.breachSeverity,
		LastTarget:     s.lastTarget,
	}
	s.havePower = true
	p := s.lastPower
	s.mu.Unlock()
	s.sink.SetPower(p)
}

type cloudEvent struct {
	Type   string          `json:"type"`
	Source string          `json:"source"`
	Time   string          `json:"time"`
	Data   json.RawMessage `json:"data"`
}

type powerValue struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func (p powerValue) megawatts() float64 {
	switch p.Unit {
	case "watt":
		return p.Value / 1e6
	case "kilowatt":
		return p.Value / 1e3
	default:
		return p.Value
	}
}

type breachData struct {
	FeedTag      string     `json:"feed_tag"`
	Status       string     `json:"status"`
	Severity     string     `json:"severity"`
	MeasuredLoad powerValue `json:"measured_load"`
	Target       struct {
		LoadConstraint powerValue `json:"load_constraint"`
	} `json:"target"`
}

func (s *Subscriber) handleBreach(data []byte) {
	var ev cloudEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	var b breachData
	if err := json.Unmarshal(ev.Data, &b); err != nil {
		return
	}
	s.mu.Lock()
	if b.Status == "resolved" {
		s.breachStatus = ""
		s.breachSeverity = ""
	} else {
		s.breachStatus = b.Status
		s.breachSeverity = b.Severity
	}
	if s.havePower {
		s.lastPower.BreachStatus = s.breachStatus
		s.lastPower.BreachSeverity = s.breachSeverity
	}
	p := s.lastPower
	have := s.havePower
	s.mu.Unlock()

	if have {
		s.sink.SetPower(p)
	}

	level := "warning"
	if b.Severity == "critical" {
		level = "critical"
	}
	var text string
	if b.Status == "resolved" {
		level = "info"
		text = fmt.Sprintf("Power breach resolved on %s", b.FeedTag)
	} else {
		text = fmt.Sprintf("Power breach %s (%s): %g MW over %g MW target on %s",
			b.Status, b.Severity, b.MeasuredLoad.megawatts(), b.Target.LoadConstraint.megawatts(), b.FeedTag)
	}
	s.sink.PushNotice(model.Notice{Kind: "breach", Level: level, Text: text, Time: time.Now().UTC()})
}

type powerStateData struct {
	Feeds map[string]struct {
		Event          string     `json:"event"`
		CalculatedLoad powerValue `json:"calculated_load"`
		Compliant      bool       `json:"compliant"`
	} `json:"feeds"`
}

func (s *Subscriber) handlePowerState(data []byte) {
	var ev cloudEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	var ps powerStateData
	if err := json.Unmarshal(ev.Data, &ps); err != nil {
		return
	}
	for feed, st := range ps.Feeds {
		switch st.Event {
		case "start_ramp_down", "start_ramp_up":
			s.sink.PushNotice(model.Notice{Kind: "ramp", Level: "info",
				Text: fmt.Sprintf("%s on %s (load %g MW)", label(st.Event), feed, st.CalculatedLoad.megawatts()),
				Time: time.Now().UTC()})
		case "end_ramp_down", "end_ramp_up":
			s.sink.PushNotice(model.Notice{Kind: "ramp", Level: "info",
				Text: fmt.Sprintf("%s complete on %s (load %g MW)", label(st.Event), feed, st.CalculatedLoad.megawatts()),
				Time: time.Now().UTC()})
		}
	}
}

type setLoadData struct {
	Targets []struct {
		FeedTags       []string    `json:"feed_tags"`
		LoadConstraint *powerValue `json:"load_constraint"`
	} `json:"targets"`
}

func (s *Subscriber) handleLoadTarget(subject string, data []byte) {
	var ev cloudEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	var sl setLoadData
	if err := json.Unmarshal(ev.Data, &sl); err != nil {
		return
	}
	isv := isvFromSubject(subject)
	when := parseEventTime(ev.Time)
	for _, t := range sl.Targets {
		feed := "all feeds"
		if len(t.FeedTags) > 0 {
			feed = strings.Join(t.FeedTags, ",")
		}
		ts := &model.TargetSet{
			By:      isv,
			Source:  ev.Source,
			Cleared: t.LoadConstraint == nil,
			Feeds:   feed,
			Time:    when,
		}
		var text string
		if t.LoadConstraint == nil {
			text = fmt.Sprintf("%s cleared the power cap on %s", isv, feed)
		} else {
			ts.ValueMW = t.LoadConstraint.megawatts()
			text = fmt.Sprintf("%s set power cap to %g MW on %s", isv, ts.ValueMW, feed)
		}

		s.mu.Lock()
		s.lastTarget = ts
		if s.havePower {
			s.lastPower.LastTarget = ts
		}
		p := s.lastPower
		have := s.havePower
		s.mu.Unlock()

		if have {
			s.sink.SetPower(p)
		}
		s.sink.PushNotice(model.Notice{Kind: "target", Level: "info", Text: text, Time: when})
	}
}

// isvFromSubject extracts the ISV identifier from a
// grid.v1.isv.<isv>.loadtarget.set subject. It falls back to "ISV" when the
// subject does not match the expected shape.
func isvFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 4 && parts[0] == "grid" && parts[2] == "isv" {
		return parts[3]
	}
	return "ISV"
}

// parseEventTime parses the CloudEvents time, defaulting to now when absent or
// malformed.
func parseEventTime(v string) time.Time {
	if v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func label(event string) string {
	switch event {
	case "start_ramp_down":
		return "Ramp down started"
	case "end_ramp_down":
		return "Ramp down"
	case "start_ramp_up":
		return "Ramp up started"
	case "end_ramp_up":
		return "Ramp up"
	default:
		return event
	}
}
