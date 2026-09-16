// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package sysmon

import (
	"time"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

// serverInfo is the "server" envelope shared by all $SYS responses/events.
type serverInfo struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Host string `json:"host"`
}

// connzResponse is the reply envelope for $SYS.REQ.SERVER.PING.CONNZ.
type connzResponse struct {
	Server serverInfo `json:"server"`
	Data   connzData  `json:"data"`
}

type connzData struct {
	NumConnections int        `json:"num_connections"`
	Total          int        `json:"total"`
	Connections    []connInfo `json:"connections"`
}

// connInfo mirrors the subset of the NATS server ConnInfo we surface.
type connInfo struct {
	CID           uint64    `json:"cid"`
	Kind          string    `json:"kind"`
	Type          string    `json:"type"`
	IP            string    `json:"ip"`
	Port          int       `json:"port"`
	Start         time.Time `json:"start"`
	LastActivity  time.Time `json:"last_activity"`
	RTT           string    `json:"rtt"`
	Uptime        string    `json:"uptime"`
	Name          string    `json:"name"`
	Lang          string    `json:"lang"`
	Version       string    `json:"version"`
	Subscriptions int       `json:"subscriptions"`
	Pending       int       `json:"pending_bytes"`
	InMsgs        int64     `json:"in_msgs"`
	OutMsgs       int64     `json:"out_msgs"`
	InBytes       int64     `json:"in_bytes"`
	OutBytes      int64     `json:"out_bytes"`
	Account       string    `json:"acc"`
}

func (c connInfo) toModel(server string) model.Connection {
	kind := c.Kind
	if kind == "" {
		kind = "Client"
	}
	return model.Connection{
		CID:           c.CID,
		Server:        server,
		Account:       c.Account,
		Name:          c.Name,
		Kind:          kind,
		Type:          c.Type,
		IP:            c.IP,
		Port:          c.Port,
		Start:         c.Start,
		LastActivity:  c.LastActivity,
		Uptime:        c.Uptime,
		RTT:           c.RTT,
		Subscriptions: c.Subscriptions,
		Pending:       c.Pending,
		InMsgs:        c.InMsgs,
		OutMsgs:       c.OutMsgs,
		InBytes:       c.InBytes,
		OutBytes:      c.OutBytes,
		Lang:          c.Lang,
		Version:       c.Version,
	}
}

// clientInfo mirrors the NATS server ClientInfo used in connect/disconnect
// lifecycle events.
type clientInfo struct {
	Account string `json:"acc"`
	CID     uint64 `json:"cid"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Kind    string `json:"kind"`
	Type    string `json:"type"`
	User    string `json:"user"`
	Lang    string `json:"lang"`
	Version string `json:"ver"`
}
