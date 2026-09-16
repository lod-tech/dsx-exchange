// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package flex

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Subject helpers. MQTT topics in the DSX Flex spec map to NATS subjects by
// replacing "/" with ".".

// LoadTargetSetSubscribeSubject is the NATS subject the agent subscribes to for
// load target commands from any ISV.
func LoadTargetSetSubscribeSubject() string {
	return "grid.v1.isv.*.loadtarget.set"
}

// LoadTargetSetSubject builds the publish subject for a specific ISV.
func LoadTargetSetSubject(isvID string) string {
	return fmt.Sprintf("grid.v1.isv.%s.loadtarget.set", isvID)
}

// PowerStateStatusSubject builds the status publish subject for an agent.
func PowerStateStatusSubject(agentID string) string {
	return fmt.Sprintf("grid.v1.dsx-flex-agent.%s.powerstate.status", agentID)
}

// PowerBreachSubject builds the breach publish subject for an agent.
func PowerBreachSubject(agentID string) string {
	return fmt.Sprintf("grid.v1.dsx-flex-agent.%s.powerbreach", agentID)
}

// TelemetrySubject is the supplementary telemetry stream consumed by the
// dashboard. It is not part of the DSX Flex contract.
const TelemetrySubject = "dsx.inference.v1.telemetry"

// AgentSource builds the CloudEvents source URI for a DSX Flex agent.
func AgentSource(agentID string) string {
	return "//grid/v1/dsx-flex-agent/" + agentID
}

// ISVSource builds the CloudEvents source URI for an ISV.
func ISVSource(isvID string) string {
	return "//grid/v1/isv/" + isvID
}

// NewEvent builds a CloudEvents envelope with the given payload marshaled into
// the data field as plaintext JSON.
func NewEvent(eventType, source, subject, correlationID string, payload any) (*Event, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", eventType, err)
	}
	return &Event{
		SpecVersion:     SpecVersion,
		ID:              uuid.NewString(),
		Source:          source,
		Type:            eventType,
		Time:            time.Now().UTC().Format(time.RFC3339Nano),
		DataContentType: DataContentTypeJSON,
		Subject:         subject,
		CorrelationID:   correlationID,
		TraceParent:     newTraceParent(),
		Data:            data,
	}, nil
}

// Marshal serializes the event to JSON for the MQTT/NATS payload.
func (e *Event) Marshal() ([]byte, error) {
	return json.Marshal(e)
}

// Parse decodes a CloudEvents envelope from bytes.
func Parse(b []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("decode cloudevent: %w", err)
	}
	return &e, nil
}

// DecodeData unmarshals the plaintext data payload into v.
func (e *Event) DecodeData(v any) error {
	if len(e.Data) == 0 {
		return fmt.Errorf("event %s has empty data", e.Type)
	}
	// Tolerate a JWS compact serialization by rejecting it explicitly so callers
	// get a clear error rather than a confusing unmarshal failure.
	if looksLikeJWS(e.Data) {
		return fmt.Errorf("event %s data is JWS-signed; JWS verification is not enabled", e.Type)
	}
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return nil
}

func looksLikeJWS(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	if len(s) < 2 || s[0] != '"' {
		return false
	}
	inner := strings.Trim(s, "\"")
	return strings.Count(inner, ".") == 2 && !strings.ContainsAny(inner, "{}[] ")
}

func newTraceParent() string {
	traceID := make([]byte, 16)
	spanID := make([]byte, 8)
	_, _ = rand.Read(traceID)
	_, _ = rand.Read(spanID)
	return fmt.Sprintf("00-%s-%s-01", hex.EncodeToString(traceID), hex.EncodeToString(spanID))
}
