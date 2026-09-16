// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"context"
	"math/rand"
	"time"
)

// vocabulary is a small word list used to synthesize completion text. The mock
// does not run a model; it generates plausible token streams for demonstration.
var vocabulary = []string{
	"the", "AI", "factory", "optimizes", "tokens", "per", "watt", "within",
	"a", "fixed", "power", "envelope", "dynamic", "allocation", "reclaims",
	"stranded", "capacity", "across", "racks", "and", "nodes", "while",
	"preserving", "throughput", "grid", "signals", "adjust", "workloads",
	"to", "meet", "demand", "response", "targets", "efficiently", "in",
	"real", "time", "megawatts", "become", "productive", "inference",
}

// generateTokens returns n synthetic tokens.
func generateTokens(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = vocabulary[rand.Intn(len(vocabulary))] //nolint:gosec // demo text, not security-sensitive
	}
	return out
}

// sleepCtx sleeps for d unless ctx is cancelled first. It returns false if the
// context was cancelled (for example, the client disconnected).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
