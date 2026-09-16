// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package power implements the workload power model and best_effort admission
// control. Each in-flight request draws a fixed amount of power, so the effective
// load equals in-flight requests times per-request power. An active DSX Flex load
// target caps the number of concurrent requests; excess demand is queued briefly
// and then shed (rejected), matching the DSX Flex best_effort strategy.
package power

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Engine performs admission control and tracks power/load metrics.
type Engine struct {
	perRequestMW float64
	powerMaxMW   float64
	powerMinMW   float64
	defaultMW    float64
	maxQueue     int
	maxQueueWait time.Duration

	mu           sync.Mutex
	cond         *sync.Cond
	inFlight     int
	queued       int
	targetActive bool
	targetMW     float64
	correlation  string

	arrivalTotal   atomic.Uint64
	acceptedTotal  atomic.Uint64
	shedTotal      atomic.Uint64
	completedTotal atomic.Uint64
}

// Snapshot is a point-in-time view of the power engine.
type Snapshot struct {
	InFlight       int
	Queued         int
	Allowed        int
	PowerMW        float64
	EffectiveCapMW float64
	TargetActive   bool
	TargetMW       float64
	CorrelationID  string
	Compliant      bool
	PerRequestMW   float64
	PowerMaxMW     float64
	PowerMinMW     float64
	DefaultMW      float64
	ArrivalTotal   uint64
	AcceptedTotal  uint64
	ShedTotal      uint64
	CompletedTotal uint64
}

// New creates a power Engine.
func New(perRequestMW, powerMaxMW, powerMinMW, defaultMW float64, maxQueue int, maxQueueWait time.Duration) *Engine {
	e := &Engine{
		perRequestMW: perRequestMW,
		powerMaxMW:   powerMaxMW,
		powerMinMW:   powerMinMW,
		defaultMW:    defaultMW,
		maxQueue:     maxQueue,
		maxQueueWait: maxQueueWait,
	}
	e.cond = sync.NewCond(&e.mu)
	return e
}

// Run periodically wakes waiters so queue-wait deadlines are enforced even when
// no slot is released. It returns when ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			e.mu.Lock()
			e.cond.Broadcast()
			e.mu.Unlock()
			return
		case <-ticker.C:
			e.mu.Lock()
			e.cond.Broadcast()
			e.mu.Unlock()
		}
	}
}

// effectiveCapLocked returns the current power cap in MW.
func (e *Engine) effectiveCapLocked() float64 {
	if e.targetActive {
		return e.targetMW
	}
	return e.defaultMW
}

// allowedLocked returns the max concurrent requests permitted by the cap.
func (e *Engine) allowedLocked() int {
	capMW := e.effectiveCapLocked()
	n := int(math.Floor(capMW / e.perRequestMW))
	if n < 0 {
		n = 0
	}
	return n
}

// SetTarget applies an active load target (megawatts). It relaxes or tightens
// the admission cap and wakes any queued requests.
func (e *Engine) SetTarget(targetMW float64, correlationID string) {
	e.mu.Lock()
	e.targetActive = true
	e.targetMW = targetMW
	e.correlation = correlationID
	e.cond.Broadcast()
	e.mu.Unlock()
}

// ClearTarget removes any active load target, reverting to the default cap.
func (e *Engine) ClearTarget() {
	e.mu.Lock()
	e.targetActive = false
	e.targetMW = 0
	e.correlation = ""
	e.cond.Broadcast()
	e.mu.Unlock()
}

// Acquire attempts to reserve one power slot for a request using the best_effort
// strategy. It returns a release func on success. If the request cannot be
// admitted within the queue-wait budget, it returns shed=true.
func (e *Engine) Acquire(ctx context.Context) (release func(), shed bool) {
	e.arrivalTotal.Add(1)
	deadline := time.Now().Add(e.maxQueueWait)

	e.mu.Lock()
	for e.inFlight >= e.allowedLocked() {
		if e.queued >= e.maxQueue || time.Now().After(deadline) || ctx.Err() != nil {
			e.mu.Unlock()
			e.shedTotal.Add(1)
			return nil, true
		}
		e.queued++
		e.cond.Wait()
		e.queued--
	}
	e.inFlight++
	e.mu.Unlock()
	e.acceptedTotal.Add(1)

	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			e.inFlight--
			e.cond.Broadcast()
			e.mu.Unlock()
			e.completedTotal.Add(1)
		})
	}, false
}

// Snapshot returns the current power/load state.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	inFlight := e.inFlight
	queued := e.queued
	allowed := e.allowedLocked()
	capMW := e.effectiveCapLocked()
	targetActive := e.targetActive
	targetMW := e.targetMW
	correlation := e.correlation
	e.mu.Unlock()

	powerMW := float64(inFlight) * e.perRequestMW
	return Snapshot{
		InFlight:       inFlight,
		Queued:         queued,
		Allowed:        allowed,
		PowerMW:        powerMW,
		EffectiveCapMW: capMW,
		TargetActive:   targetActive,
		TargetMW:       targetMW,
		CorrelationID:  correlation,
		Compliant:      powerMW <= capMW+1e-9,
		PerRequestMW:   e.perRequestMW,
		PowerMaxMW:     e.powerMaxMW,
		PowerMinMW:     e.powerMinMW,
		DefaultMW:      e.defaultMW,
		ArrivalTotal:   e.arrivalTotal.Load(),
		AcceptedTotal:  e.acceptedTotal.Load(),
		ShedTotal:      e.shedTotal.Load(),
		CompletedTotal: e.completedTotal.Load(),
	}
}
