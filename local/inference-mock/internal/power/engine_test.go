// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package power

import (
	"context"
	"testing"
	"time"
)

func TestAdmissionCapsConcurrency(t *testing.T) {
	// perRequest=1 MW, default cap=3 MW => max 3 concurrent, no queue.
	e := New(1, 10, 0, 3, 0, 10*time.Millisecond)
	ctx := context.Background()

	var releases []func()
	for i := 0; i < 3; i++ {
		rel, shed := e.Acquire(ctx)
		if shed {
			t.Fatalf("acquire %d shed unexpectedly", i)
		}
		releases = append(releases, rel)
	}

	if _, shed := e.Acquire(ctx); !shed {
		t.Fatal("expected shed at capacity")
	}

	snap := e.Snapshot()
	if snap.InFlight != 3 {
		t.Errorf("in-flight = %d, want 3", snap.InFlight)
	}
	if snap.PowerMW != 3 {
		t.Errorf("power = %.1f MW, want 3", snap.PowerMW)
	}
	if !snap.Compliant {
		t.Error("expected compliant when at cap")
	}

	releases[0]()
	rel, shed := e.Acquire(ctx)
	if shed {
		t.Fatal("expected admission after a slot was released")
	}
	rel()
}

func TestSetAndClearTarget(t *testing.T) {
	e := New(1, 96, 0, 96, 0, 10*time.Millisecond)
	ctx := context.Background()

	e.SetTarget(2, "corr-1")
	r1, shed := e.Acquire(ctx)
	if shed {
		t.Fatal("first acquire shed")
	}
	r2, shed := e.Acquire(ctx)
	if shed {
		t.Fatal("second acquire shed")
	}
	if _, shed := e.Acquire(ctx); !shed {
		t.Fatal("expected shed above 2 MW target")
	}
	snap := e.Snapshot()
	if snap.TargetMW != 2 || snap.EffectiveCapMW != 2 || snap.CorrelationID != "corr-1" {
		t.Errorf("unexpected target snapshot: %+v", snap)
	}

	e.ClearTarget()
	r3, shed := e.Acquire(ctx)
	if shed {
		t.Fatal("expected admission after clearing target")
	}
	if snap := e.Snapshot(); snap.TargetActive {
		t.Error("target should be inactive after ClearTarget")
	}
	r1()
	r2()
	r3()
}

func TestRampDownIsNonCompliantUntilDrain(t *testing.T) {
	e := New(1, 96, 0, 96, 0, 10*time.Millisecond)
	ctx := context.Background()

	// Admit 5 requests under the default cap.
	var rels []func()
	for i := 0; i < 5; i++ {
		rel, shed := e.Acquire(ctx)
		if shed {
			t.Fatalf("acquire %d shed", i)
		}
		rels = append(rels, rel)
	}
	// Tighten the target below current load: measured power now exceeds target.
	e.SetTarget(2, "corr-ramp")
	snap := e.Snapshot()
	if snap.Compliant {
		t.Errorf("expected non-compliant during ramp-down (power=%.0f > target=%.0f)", snap.PowerMW, snap.EffectiveCapMW)
	}
	// Drain below the target.
	for _, r := range rels {
		r()
	}
	if snap := e.Snapshot(); !snap.Compliant {
		t.Errorf("expected compliant after drain, power=%.0f target=%.0f", snap.PowerMW, snap.EffectiveCapMW)
	}
}
