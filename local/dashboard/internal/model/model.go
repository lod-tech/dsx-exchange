// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package model defines the data structures exchanged between the dashboard
// backend and the browser over the WebSocket connection.
package model

import "time"

// Connection is a single live client connection as reported by the NATS
// monitoring endpoint ($SYS.REQ.SERVER.PING.CONNZ).
type Connection struct {
	CID           uint64    `json:"cid"`
	Server        string    `json:"server"`
	Account       string    `json:"account"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Type          string    `json:"type"`
	IP            string    `json:"ip"`
	Port          int       `json:"port"`
	Start         time.Time `json:"start"`
	LastActivity  time.Time `json:"lastActivity"`
	Uptime        string    `json:"uptime"`
	RTT           string    `json:"rtt"`
	Subscriptions int       `json:"subscriptions"`
	Pending       int       `json:"pending"`
	InMsgs        int64     `json:"inMsgs"`
	OutMsgs       int64     `json:"outMsgs"`
	InBytes       int64     `json:"inBytes"`
	OutBytes      int64     `json:"outBytes"`
	Lang          string    `json:"lang"`
	Version       string    `json:"version"`
}

// Key uniquely identifies a connection across servers.
func (c Connection) Key() string {
	return c.Server + "/" + itoa(c.CID)
}

// Event is a single message observed on the CSC account.
type Event struct {
	Seq     uint64    `json:"seq"`
	Subject string    `json:"subject"`
	Payload string    `json:"payload"`
	Size    int       `json:"size"`
	Time    time.Time `json:"time"`
}

// LifecycleEvent is a connect or disconnect notification for the activity feed.
type LifecycleEvent struct {
	Kind    string    `json:"kind"` // "connect" or "disconnect"
	Account string    `json:"account"`
	Name    string    `json:"name"`
	CID     uint64    `json:"cid"`
	IP      string    `json:"ip"`
	Reason  string    `json:"reason,omitempty"`
	Time    time.Time `json:"time"`
}

// Stats summarizes current throughput and connection counts.
type Stats struct {
	Connections     int     `json:"connections"`
	ClientConns     int     `json:"clientConns"`
	EventsTotal     uint64  `json:"eventsTotal"`
	EventsPerSecond float64 `json:"eventsPerSecond"`
	SysConnected    bool    `json:"sysConnected"`
	EventConnected  bool    `json:"eventConnected"`
}

// Power is the latest AI-factory power snapshot derived from the mock inference
// service telemetry and DSX Flex breach alerts.
type Power struct {
	Time           time.Time `json:"time"`
	FeedTag        string    `json:"feedTag"`
	RPS            float64   `json:"rps"`
	AcceptedPerSec float64   `json:"acceptedPerSec"`
	ShedPerSec     float64   `json:"shedPerSec"`
	InFlight       int       `json:"inFlight"`
	PowerMW        float64   `json:"powerMw"`
	TargetMW       float64   `json:"targetMw"`
	TargetActive   bool      `json:"targetActive"`
	Compliant      bool      `json:"compliant"`
	BreachStatus   string    `json:"breachStatus,omitempty"`
	BreachSeverity string    `json:"breachSeverity,omitempty"`
}

// Notice is an activity-feed note for DSX Flex control-plane events (load target
// set, breach transitions, ramp events).
type Notice struct {
	Kind  string    `json:"kind"`  // target | breach | ramp
	Level string    `json:"level"` // info | warning | critical
	Text  string    `json:"text"`
	Time  time.Time `json:"time"`
}

// Envelope is the WebSocket message wrapper broadcast to browsers.
type Envelope struct {
	Type        string          `json:"type"` // connections | events | lifecycle | stats | power | notice
	Connections []Connection    `json:"connections,omitempty"`
	Events      []Event         `json:"events,omitempty"`
	Lifecycle   *LifecycleEvent `json:"lifecycle,omitempty"`
	Stats       *Stats          `json:"stats,omitempty"`
	Power       *Power          `json:"power,omitempty"`
	Notice      *Notice         `json:"notice,omitempty"`
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
