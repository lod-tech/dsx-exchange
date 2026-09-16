// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package sysmon

import (
	"encoding/json"
	"testing"
)

func TestAccountFromSubject(t *testing.T) {
	cases := map[string]string{
		"$SYS.ACCOUNT.CSC.CONNECT":            "CSC",
		"$SYS.ACCOUNT.LaunchLayer.DISCONNECT": "LaunchLayer",
		"$SYS.ACCOUNT.SYS.CONNECT":            "SYS",
		"not.a.sys.subject":                   "",
		"$SYS.ACCOUNT":                        "",
	}
	for subject, want := range cases {
		if got := accountFromSubject(subject); got != want {
			t.Errorf("accountFromSubject(%q) = %q, want %q", subject, got, want)
		}
	}
}

func TestConnzResponseToModel(t *testing.T) {
	raw := `{
		"server": {"name": "nats-0", "id": "ABC123", "host": "0.0.0.0"},
		"data": {
			"num_connections": 1,
			"total": 1,
			"connections": [
				{
					"cid": 42,
					"kind": "Client",
					"type": "mqtt",
					"ip": "172.18.0.5",
					"port": 51000,
					"start": "2026-09-16T12:00:00Z",
					"last_activity": "2026-09-16T12:00:05Z",
					"uptime": "5s",
					"rtt": "1ms",
					"name": "demo-app-123",
					"lang": "go",
					"version": "1.5.1",
					"subscriptions": 3,
					"pending_bytes": 0,
					"in_msgs": 10,
					"out_msgs": 7,
					"in_bytes": 100,
					"out_bytes": 70,
					"acc": "CSC"
				}
			]
		}
	}`

	var resp connzResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Server.Name != "nats-0" {
		t.Errorf("server name = %q", resp.Server.Name)
	}
	if len(resp.Data.Connections) != 1 {
		t.Fatalf("connections = %d, want 1", len(resp.Data.Connections))
	}

	c := resp.Data.Connections[0].toModel(resp.Server.Name)
	if c.CID != 42 {
		t.Errorf("CID = %d, want 42", c.CID)
	}
	if c.Server != "nats-0" {
		t.Errorf("Server = %q, want nats-0", c.Server)
	}
	if c.Account != "CSC" {
		t.Errorf("Account = %q, want CSC", c.Account)
	}
	if c.Name != "demo-app-123" {
		t.Errorf("Name = %q", c.Name)
	}
	if c.Subscriptions != 3 || c.InMsgs != 10 || c.OutMsgs != 7 {
		t.Errorf("counters mismatch: subs=%d in=%d out=%d", c.Subscriptions, c.InMsgs, c.OutMsgs)
	}
	if c.Key() != "nats-0/42" {
		t.Errorf("Key = %q, want nats-0/42", c.Key())
	}
}

func TestConnInfoDefaultKind(t *testing.T) {
	var ci connInfo
	ci.CID = 1
	m := ci.toModel("srv")
	if m.Kind != "Client" {
		t.Errorf("default Kind = %q, want Client", m.Kind)
	}
}
