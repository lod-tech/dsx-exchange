// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package agent implements the DSX Flex agent behavior for the mock workload. It
// subscribes to load target commands, applies them to the power engine, and
// publishes power state snapshots, breach alerts, and a supplementary telemetry
// stream consumed by the dashboard.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/config"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/flex"
	"github.com/NVIDIA/dsx-exchange/local/inference-mock/internal/power"
)

// Agent connects the power engine to the DSX Flex event plane.
type Agent struct {
	cfg    *config.Config
	engine *power.Engine
	nc     *nats.Conn

	mu             sync.Mutex
	ramping        bool
	rampDir        string
	eventStart     *time.Time
	breachID       string
	breachSeverity string

	// telemetry rate tracking
	lastArrival   uint64
	lastAccepted  uint64
	lastShed      uint64
	lastTelemetry time.Time
}

// Telemetry is the supplementary, non-DSX-Flex stream consumed by the dashboard.
type Telemetry struct {
	Time           string  `json:"time"`
	FeedTag        string  `json:"feed_tag"`
	RPS            float64 `json:"rps"`
	AcceptedPerSec float64 `json:"accepted_per_sec"`
	ShedPerSec     float64 `json:"shed_per_sec"`
	InFlight       int     `json:"in_flight"`
	Queued         int     `json:"queued"`
	Allowed        int     `json:"allowed"`
	PerRequestMW   float64 `json:"per_request_mw"`
	PowerMW        float64 `json:"power_mw"`
	TargetMW       float64 `json:"target_mw"`
	EffectiveCapMW float64 `json:"effective_cap_mw"`
	TargetActive   bool    `json:"target_active"`
	Compliant      bool    `json:"compliant"`
	AcceptedTotal  uint64  `json:"accepted_total"`
	ShedTotal      uint64  `json:"shed_total"`
}

// Connect dials NATS on the CSC account using OAuth2 client credentials.
func Connect(ctx context.Context, cfg *config.Config, engine *power.Engine) (*Agent, error) {
	if err := cfg.TrimIDPURL(); err != nil {
		return nil, err
	}
	ccfg := &clientcredentials.Config{
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		TokenURL:     cfg.OAuthIDPURL + "/token",
		Scopes:       []string{cfg.OAuthScope},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	tokenSource := ccfg.TokenSource(context.WithoutCancel(ctx))
	if _, err := tokenSource.Token(); err != nil {
		return nil, fmt.Errorf("obtain initial OAuth2 token: %w", err)
	}

	a := &Agent{cfg: cfg, engine: engine, lastTelemetry: time.Now()}
	opts := []nats.Option{
		nats.TokenHandler(func() string {
			tok, err := tokenSource.Token()
			if err != nil {
				log.Printf("agent: token refresh failed: %v", err)
				return ""
			}
			return tok.AccessToken
		}),
		nats.Name("dsx-inference-" + cfg.AgentID),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	nc, err := nats.Connect(cfg.NATSURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect CSC account: %w", err)
	}
	a.nc = nc
	return a, nil
}

// Run subscribes to load targets and drives the status/telemetry loops until ctx
// is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	sub, err := a.nc.Subscribe(flex.LoadTargetSetSubscribeSubject(), a.handleLoadTarget)
	if err != nil {
		return fmt.Errorf("subscribe load targets: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	log.Printf("agent: subscribed to %q; publishing status to %q",
		flex.LoadTargetSetSubscribeSubject(), flex.PowerStateStatusSubject(a.cfg.AgentID))

	statusTicker := time.NewTicker(500 * time.Millisecond)
	defer statusTicker.Stop()
	heartbeat := time.NewTicker(a.cfg.StatusInterval)
	defer heartbeat.Stop()
	telemetryTicker := time.NewTicker(a.cfg.TelemetryInterval)
	defer telemetryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-statusTicker.C:
			a.evaluateTransitions()
		case <-heartbeat.C:
			a.publishStatus(flex.EventPeriodic)
		case <-telemetryTicker.C:
			a.publishTelemetry()
		}
	}
}

// Close drains the NATS connection.
func (a *Agent) Close() {
	if a.nc != nil {
		_ = a.nc.Drain()
	}
}

func (a *Agent) handleLoadTarget(msg *nats.Msg) {
	event, err := flex.Parse(msg.Data)
	if err != nil {
		log.Printf("agent: parse load target: %v", err)
		return
	}
	if event.Type != flex.TypeLoadTargetSet {
		return
	}
	var data flex.SetLoadTargetData
	if err := event.DecodeData(&data); err != nil {
		log.Printf("agent: decode load target: %v", err)
		return
	}
	target, ok := a.applicableTarget(data)
	if !ok {
		return
	}
	a.applyTarget(target, event.CorrelationID)
}

// applicableTarget selects the target that applies to this agent's feed.
func (a *Agent) applicableTarget(data flex.SetLoadTargetData) (flex.LoadTargetRequest, bool) {
	for _, t := range data.Targets {
		if len(t.FeedTags) == 0 {
			return t, true
		}
		for _, tag := range t.FeedTags {
			if tag == a.cfg.FeedTag {
				return t, true
			}
		}
	}
	return flex.LoadTargetRequest{}, false
}

func (a *Agent) applyTarget(t flex.LoadTargetRequest, correlationID string) {
	// Honor the interval: apply at start_time (or now) and clear at end_time.
	apply := func() {
		snap := a.engine.Snapshot()
		if t.LoadConstraint == nil {
			a.engine.ClearTarget()
			log.Printf("agent: load target cleared (correlation=%s)", correlationID)
		} else {
			mw := t.LoadConstraint.Megawatts()
			a.engine.SetTarget(mw, correlationID)
			log.Printf("agent: load target set to %.2f MW (correlation=%s)", mw, correlationID)
		}
		a.onTargetSet(snap.PowerMW)
	}

	now := time.Now()
	start := parseTime(t.Interval.StartTime)
	if start != nil && start.After(now) {
		time.AfterFunc(start.Sub(now), apply)
	} else {
		apply()
	}
	if end := parseTime(t.Interval.EndTime); end != nil && end.After(now) {
		time.AfterFunc(end.Sub(now), func() {
			a.engine.ClearTarget()
			log.Printf("agent: load target expired at end_time (correlation=%s)", correlationID)
			a.onTargetSet(a.engine.Snapshot().PowerMW)
		})
	}
}

// onTargetSet records a power-event transition and publishes a target_set status.
func (a *Agent) onTargetSet(powerBeforeMW float64) {
	snap := a.engine.Snapshot()
	a.mu.Lock()
	nowStr := time.Now().UTC()
	a.eventStart = &nowStr
	if snap.EffectiveCapMW < powerBeforeMW {
		a.ramping = true
		a.rampDir = "down"
	} else {
		a.ramping = true
		a.rampDir = "up"
	}
	a.mu.Unlock()
	a.publishStatus(flex.EventTargetSet)
	if a.ramping && a.rampDir == "down" {
		a.publishStatus(flex.EventStartRampDown)
	} else {
		a.publishStatus(flex.EventStartRampUp)
	}
}

// evaluateTransitions detects ramp completion and breach lifecycle changes.
func (a *Agent) evaluateTransitions() {
	snap := a.engine.Snapshot()

	a.mu.Lock()
	ramping := a.ramping
	rampDir := a.rampDir
	a.mu.Unlock()

	if ramping {
		if rampDir == "down" && snap.PowerMW <= snap.EffectiveCapMW+1e-9 {
			a.mu.Lock()
			a.ramping = false
			a.mu.Unlock()
			a.publishStatus(flex.EventEndRampDown)
		} else if rampDir == "up" && snap.Queued == 0 {
			a.mu.Lock()
			a.ramping = false
			a.mu.Unlock()
			a.publishStatus(flex.EventEndRampUp)
		}
	}

	a.evaluateBreach(snap)
}

// evaluateBreach publishes breach alerts when measured load exceeds the target.
func (a *Agent) evaluateBreach(snap power.Snapshot) {
	over := snap.PowerMW > snap.EffectiveCapMW+1e-9
	severity := flex.SeverityWarning
	if snap.PowerMW > snap.EffectiveCapMW*1.2 {
		severity = flex.SeverityCritical
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case over && a.breachID == "":
		a.breachID = uuid.NewString()
		a.breachSeverity = severity
		a.publishBreachLocked(snap, flex.BreachActive, severity, false)
	case over && a.breachSeverity != severity && severity == flex.SeverityCritical:
		a.breachSeverity = severity
		a.publishBreachLocked(snap, flex.BreachEscalated, severity, false)
	case !over && a.breachID != "":
		a.publishBreachLocked(snap, flex.BreachResolved, a.breachSeverity, true)
		a.breachID = ""
		a.breachSeverity = ""
	}
}

func (a *Agent) publishBreachLocked(snap power.Snapshot, status, severity string, resolved bool) {
	detectedAt := time.Now().UTC().Format(time.RFC3339Nano)
	breach := flex.BreachDetails{BreachID: a.breachID, DetectedAt: detectedAt}
	if resolved {
		r := detectedAt
		breach.ResolvedAt = &r
	}
	data := flex.PowerBreachAlertData{
		FeedTag:      a.cfg.FeedTag,
		Status:       status,
		Severity:     severity,
		Breach:       breach,
		Target:       flex.BreachTarget{CorrelationID: snap.CorrelationID, LoadConstraint: flex.LoadConstraint{Value: snap.EffectiveCapMW, Unit: flex.UnitMegawatt}},
		MeasuredLoad: flex.MW(snap.PowerMW),
	}
	if !resolved {
		data.ShedHints = []flex.ShedHint{{
			ResourceID:   a.cfg.FeedTag,
			ResourceType: "node",
			Action:       "throttle",
			Priority:     1,
		}}
		data.InfrastructureActions = []string{"SHUTDOWN_NODES"}
	} else {
		data.InfrastructureActions = []string{}
	}
	a.publish(flex.TypePowerBreachAlert, flex.PowerBreachSubject(a.cfg.AgentID), snap.CorrelationID, data)
	log.Printf("agent: breach %s (severity=%s, measured=%.2f MW, target=%.2f MW)", status, severity, snap.PowerMW, snap.EffectiveCapMW)
}

func (a *Agent) publishStatus(event string) {
	snap := a.engine.Snapshot()

	a.mu.Lock()
	ramping := a.ramping
	var eventStart *string
	if a.eventStart != nil {
		s := a.eventStart.Format(time.RFC3339Nano)
		eventStart = &s
	}
	a.mu.Unlock()

	var targets []flex.FeedTarget
	if snap.TargetActive {
		targets = []flex.FeedTarget{{
			Active:         true,
			LoadConstraint: flex.LoadConstraint{Value: snap.TargetMW, Unit: flex.UnitMegawatt},
			Strategy:       flex.Strategy{BestEffort: true},
			CorrelationID:  snap.CorrelationID,
		}}
	}

	feed := flex.FeedState{
		Metadata: flex.FeedMetadata{
			PowerMinimum:      flex.MW(snap.PowerMinMW),
			PowerMaximum:      flex.MW(snap.PowerMaxMW),
			DefaultConstraint: flex.MW(snap.DefaultMW),
		},
		Targets:             targets,
		CalculatedLoad:      flex.MW(snap.PowerMW),
		InFlight:            ramping,
		Event:               event,
		PowerEventStartTime: eventStart,
		Compliant:           snap.Compliant,
	}
	data := flex.PowerStateStatusData{
		SnapshotTime: time.Now().UTC().Format(time.RFC3339Nano),
		Feeds:        map[string]flex.FeedState{a.cfg.FeedTag: feed},
	}
	a.publish(flex.TypePowerStateStatus, flex.PowerStateStatusSubject(a.cfg.AgentID), snap.CorrelationID, data)
}

func (a *Agent) publishTelemetry() {
	snap := a.engine.Snapshot()
	now := time.Now()

	a.mu.Lock()
	elapsed := now.Sub(a.lastTelemetry).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	rps := float64(snap.ArrivalTotal-a.lastArrival) / elapsed
	acceptedPerSec := float64(snap.AcceptedTotal-a.lastAccepted) / elapsed
	shedPerSec := float64(snap.ShedTotal-a.lastShed) / elapsed
	a.lastArrival = snap.ArrivalTotal
	a.lastAccepted = snap.AcceptedTotal
	a.lastShed = snap.ShedTotal
	a.lastTelemetry = now
	a.mu.Unlock()

	tel := Telemetry{
		Time:           now.UTC().Format(time.RFC3339Nano),
		FeedTag:        a.cfg.FeedTag,
		RPS:            round2(rps),
		AcceptedPerSec: round2(acceptedPerSec),
		ShedPerSec:     round2(shedPerSec),
		InFlight:       snap.InFlight,
		Queued:         snap.Queued,
		Allowed:        snap.Allowed,
		PerRequestMW:   snap.PerRequestMW,
		PowerMW:        round3(snap.PowerMW),
		TargetMW:       round3(snap.TargetMW),
		EffectiveCapMW: round3(snap.EffectiveCapMW),
		TargetActive:   snap.TargetActive,
		Compliant:      snap.Compliant,
		AcceptedTotal:  snap.AcceptedTotal,
		ShedTotal:      snap.ShedTotal,
	}
	b, err := json.Marshal(tel)
	if err != nil {
		return
	}
	if err := a.nc.Publish(flex.TelemetrySubject, b); err != nil {
		log.Printf("agent: publish telemetry: %v", err)
	}
}

func (a *Agent) publish(eventType, subject, correlationID string, payload any) {
	event, err := flex.NewEvent(eventType, flex.AgentSource(a.cfg.AgentID), subject, correlationID, payload)
	if err != nil {
		log.Printf("agent: build %s: %v", eventType, err)
		return
	}
	b, err := event.Marshal()
	if err != nil {
		log.Printf("agent: marshal %s: %v", eventType, err)
		return
	}
	if err := a.nc.Publish(subject, b); err != nil {
		log.Printf("agent: publish %s: %v", eventType, err)
	}
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}
