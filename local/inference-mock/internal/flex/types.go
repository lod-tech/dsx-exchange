// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package flex implements the DSX Flex power-management event contract
// (schemas/asyncapi/dsx-flex/dsx-flex.yaml): CloudEvents 1.0.2 envelopes and the
// LoadTargetSet, PowerStateStatus, and PowerBreachAlert payloads. The data field
// is carried as plaintext JSON (the schema allows it); JWS signing can be layered
// on later without changing these types.
package flex

import "encoding/json"

// CloudEvents type identifiers.
const (
	TypeLoadTargetSet      = "grid.loadtarget.set.v1"
	TypePowerStateStatus   = "grid.powerstate.status.v1"
	TypePowerBreachAlert   = "grid.powerbreach.alert.v1"
	TypePowerBreachEnforce = "grid.powerbreach.enforcement.v1"
	SpecVersion            = "1.0.2"
	DataContentTypeJSON    = "application/json"
	MessageContentType     = "application/cloudevents+json"
)

// Power units.
const (
	UnitWatt     = "watt"
	UnitKilowatt = "kilowatt"
	UnitMegawatt = "megawatt"
)

// Event is the CloudEvents 1.0.2 envelope shared by all DSX Flex messages. The
// data field holds the plaintext JSON payload for the given type.
type Event struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            string          `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Subject         string          `json:"subject"`
	CorrelationID   string          `json:"correlationid,omitempty"`
	TraceParent     string          `json:"traceparent"`
	TraceState      string          `json:"tracestate,omitempty"`
	Data            json.RawMessage `json:"data"`
}

// PowerUnit is a power measurement with an explicit unit.
type PowerUnit struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Megawatts converts the measurement to megawatts.
func (p PowerUnit) Megawatts() float64 {
	switch p.Unit {
	case UnitWatt:
		return p.Value / 1e6
	case UnitKilowatt:
		return p.Value / 1e3
	default:
		return p.Value
	}
}

// MW builds a megawatt PowerUnit.
func MW(v float64) PowerUnit { return PowerUnit{Value: v, Unit: UnitMegawatt} }

// LoadConstraint is a power limit.
type LoadConstraint struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Megawatts converts the constraint to megawatts.
func (l LoadConstraint) Megawatts() float64 {
	switch l.Unit {
	case UnitWatt:
		return l.Value / 1e6
	case UnitKilowatt:
		return l.Value / 1e3
	default:
		return l.Value
	}
}

// Interval is the scheduling window for a load target.
type Interval struct {
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
}

// Strategy configures load-shedding behavior.
type Strategy struct {
	BestEffort bool `json:"best_effort"`
}

// SetLoadTargetData is the payload for grid.loadtarget.set.v1.
type SetLoadTargetData struct {
	Targets []LoadTargetRequest `json:"targets"`
}

// LoadTargetRequest is a single load target within a SetLoadTargetData.
type LoadTargetRequest struct {
	Interval       Interval        `json:"interval"`
	FeedTags       []string        `json:"feed_tags,omitempty"`
	LoadConstraint *LoadConstraint `json:"load_constraint"`
	Strategy       *Strategy       `json:"strategy,omitempty"`
}

// FeedMetadata is static feed configuration.
type FeedMetadata struct {
	PowerMinimum      PowerUnit `json:"power_minimum"`
	PowerMaximum      PowerUnit `json:"power_maximum"`
	DefaultConstraint PowerUnit `json:"default_constraint"`
}

// FeedTarget is an active or scheduled target for a feed.
type FeedTarget struct {
	Active         bool           `json:"active"`
	LoadConstraint LoadConstraint `json:"load_constraint"`
	Interval       Interval       `json:"interval"`
	Strategy       Strategy       `json:"strategy"`
	CorrelationID  string         `json:"correlation_id"`
}

// Feed lifecycle events for PowerStateStatus.
const (
	EventTargetSet     = "target_set"
	EventStartRampDown = "start_ramp_down"
	EventEndRampDown   = "end_ramp_down"
	EventStartRampUp   = "start_ramp_up"
	EventEndRampUp     = "end_ramp_up"
	EventPeriodic      = "periodic"
)

// FeedState is the current power state for a single feed.
type FeedState struct {
	Metadata            FeedMetadata `json:"metadata"`
	Targets             []FeedTarget `json:"targets"`
	CalculatedLoad      PowerUnit    `json:"calculated_load"`
	InFlight            bool         `json:"in_flight"`
	Event               string       `json:"event"`
	PowerEventStartTime *string      `json:"power_event_start_time"`
	Compliant           bool         `json:"compliant"`
}

// PowerStateStatusData is the payload for grid.powerstate.status.v1.
type PowerStateStatusData struct {
	SnapshotTime string               `json:"snapshot_time"`
	Feeds        map[string]FeedState `json:"feeds"`
}

// ShedHint is a resource-level load-shedding recommendation.
type ShedHint struct {
	ResourceID   string     `json:"resource_id"`
	ResourceType string     `json:"resource_type"`
	Action       string     `json:"action"`
	Priority     int        `json:"priority"`
	TargetPower  *PowerUnit `json:"target_power,omitempty"`
}

// BreachDetails identifies and times a breach.
type BreachDetails struct {
	BreachID   string  `json:"breach_id"`
	DetectedAt string  `json:"detected_at"`
	ResolvedAt *string `json:"resolved_at,omitempty"`
}

// BreachTarget is the target being breached.
type BreachTarget struct {
	CorrelationID  string         `json:"correlation_id"`
	LoadConstraint LoadConstraint `json:"load_constraint"`
}

// Breach lifecycle and severity values.
const (
	BreachActive     = "active"
	BreachEscalated  = "escalated"
	BreachResolved   = "resolved"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// PowerBreachAlertData is the payload for grid.powerbreach.alert.v1.
type PowerBreachAlertData struct {
	FeedTag               string        `json:"feed_tag"`
	Status                string        `json:"status"`
	Severity              string        `json:"severity"`
	Breach                BreachDetails `json:"breach"`
	Target                BreachTarget  `json:"target"`
	MeasuredLoad          PowerUnit     `json:"measured_load"`
	ShedHints             []ShedHint    `json:"shed_hints,omitempty"`
	InfrastructureActions []string      `json:"infrastructure_actions"`
}
