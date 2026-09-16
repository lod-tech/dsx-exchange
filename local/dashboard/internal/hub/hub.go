// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package hub fans out live connection snapshots, event batches, and stats to
// connected browsers over WebSocket. It batches high-frequency events and keeps
// a small ring buffer so newly connected browsers immediately see recent state.
package hub

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/NVIDIA/dsx-exchange/local/dashboard/internal/model"
)

const (
	clientSendBuffer = 64
	writeWait        = 10 * time.Second
	pongWait         = 60 * time.Second
	pingPeriod       = (pongWait * 9) / 10
)

// Hub coordinates broadcast state and connected browser clients.
type Hub struct {
	upgrader websocket.Upgrader

	flushInterval   time.Duration
	statsInterval   time.Duration
	eventBufferSize int

	mu            sync.RWMutex
	clients       map[*client]struct{}
	conns         []model.Connection
	recentEvents  []model.Event
	pendingEvents []model.Event
	eventsTotal   uint64
	eventsWindow  uint64 // events since last stats tick
	eventsPerSec  float64
	sysConnected  bool
	eventConnHere bool
	seq           uint64
	lastPower     *model.Power
	recentNotices []model.Notice
}

const maxRecentNotices = 40

type client struct {
	conn *websocket.Conn
	send chan []byte
}

// New creates a Hub.
func New(flushInterval, statsInterval time.Duration, eventBufferSize int) *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// Local dashboard: allow any origin (accessed via port-forward).
			CheckOrigin: func(*http.Request) bool { return true },
		},
		flushInterval:   flushInterval,
		statsInterval:   statsInterval,
		eventBufferSize: eventBufferSize,
		clients:         make(map[*client]struct{}),
	}
}

// Run drives periodic event flushing and stats broadcasts until ctx is done.
func (h *Hub) Run(ctx context.Context) {
	flush := time.NewTicker(h.flushInterval)
	defer flush.Stop()
	stats := time.NewTicker(h.statsInterval)
	defer stats.Stop()

	lastStats := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-flush.C:
			h.flushEvents()
		case now := <-stats.C:
			h.computeRate(now.Sub(lastStats))
			lastStats = now
			h.broadcast(h.statsEnvelope())
		}
	}
}

// ServeWS upgrades an HTTP request to a WebSocket and registers the client.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("hub: websocket upgrade failed: %v", err)
		return
	}
	c := &client{conn: conn, send: make(chan []byte, clientSendBuffer)}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	// Send the current snapshot so the browser renders immediately.
	for _, env := range h.snapshotEnvelopes() {
		if b, err := json.Marshal(env); err == nil {
			select {
			case c.send <- b:
			default:
			}
		}
	}

	go h.writePump(c)
	go h.readPump(c)
}

// SetConnections replaces the live connection roster and broadcasts it.
func (h *Hub) SetConnections(conns []model.Connection) {
	h.mu.Lock()
	h.conns = conns
	h.mu.Unlock()
	h.broadcast(model.Envelope{Type: "connections", Connections: conns})
}

// PushEvent buffers an observed event for the next flush.
func (h *Hub) PushEvent(e model.Event) {
	h.mu.Lock()
	h.seq++
	e.Seq = h.seq
	h.eventsTotal++
	h.eventsWindow++
	h.pendingEvents = append(h.pendingEvents, e)
	// Maintain the recent-events ring buffer.
	h.recentEvents = append(h.recentEvents, e)
	if len(h.recentEvents) > h.eventBufferSize {
		h.recentEvents = h.recentEvents[len(h.recentEvents)-h.eventBufferSize:]
	}
	h.mu.Unlock()
}

// PushLifecycle broadcasts a connect/disconnect notification immediately.
func (h *Hub) PushLifecycle(e model.LifecycleEvent) {
	le := e
	h.broadcast(model.Envelope{Type: "lifecycle", Lifecycle: &le})
}

// SetPower stores the latest power snapshot and broadcasts it to browsers.
func (h *Hub) SetPower(p model.Power) {
	pc := p
	h.mu.Lock()
	h.lastPower = &pc
	h.mu.Unlock()
	h.broadcast(model.Envelope{Type: "power", Power: &pc})
}

// PushNotice broadcasts a control-plane notice and retains it for new clients.
func (h *Hub) PushNotice(n model.Notice) {
	nc := n
	h.mu.Lock()
	h.recentNotices = append(h.recentNotices, nc)
	if len(h.recentNotices) > maxRecentNotices {
		h.recentNotices = h.recentNotices[len(h.recentNotices)-maxRecentNotices:]
	}
	h.mu.Unlock()
	h.broadcast(model.Envelope{Type: "notice", Notice: &nc})
}

// SetSysConnected records the SYS monitor connection state.
func (h *Hub) SetSysConnected(v bool) {
	h.mu.Lock()
	h.sysConnected = v
	h.mu.Unlock()
}

// SetEventConnected records the CSC event subscriber connection state.
func (h *Hub) SetEventConnected(v bool) {
	h.mu.Lock()
	h.eventConnHere = v
	h.mu.Unlock()
}

// ConnectionState reports whether the SYS monitor and event subscriber are
// currently connected. Used by the health endpoint.
func (h *Hub) ConnectionState() (sys, event bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sysConnected, h.eventConnHere
}

func (h *Hub) flushEvents() {
	h.mu.Lock()
	if len(h.pendingEvents) == 0 {
		h.mu.Unlock()
		return
	}
	batch := h.pendingEvents
	h.pendingEvents = nil
	h.mu.Unlock()
	h.broadcast(model.Envelope{Type: "events", Events: batch})
}

func (h *Hub) computeRate(elapsed time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	secs := elapsed.Seconds()
	if secs <= 0 {
		secs = 1
	}
	h.eventsPerSec = float64(h.eventsWindow) / secs
	h.eventsWindow = 0
}

func (h *Hub) statsEnvelope() model.Envelope {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.buildStatsLocked()
	return model.Envelope{Type: "stats", Stats: &s}
}

func (h *Hub) buildStatsLocked() model.Stats {
	clientConns := 0
	for _, c := range h.conns {
		if c.Kind == "" || c.Kind == "Client" {
			clientConns++
		}
	}
	return model.Stats{
		Connections:     len(h.conns),
		ClientConns:     clientConns,
		EventsTotal:     h.eventsTotal,
		EventsPerSecond: round2(h.eventsPerSec),
		SysConnected:    h.sysConnected,
		EventConnected:  h.eventConnHere,
	}
}

// snapshotEnvelopes returns the initial state sent to a newly connected client.
func (h *Hub) snapshotEnvelopes() []model.Envelope {
	h.mu.RLock()
	defer h.mu.RUnlock()
	conns := make([]model.Connection, len(h.conns))
	copy(conns, h.conns)
	events := make([]model.Event, len(h.recentEvents))
	copy(events, h.recentEvents)
	s := h.buildStatsLocked()
	envs := []model.Envelope{
		{Type: "connections", Connections: conns},
		{Type: "events", Events: events},
		{Type: "stats", Stats: &s},
	}
	if h.lastPower != nil {
		p := *h.lastPower
		envs = append(envs, model.Envelope{Type: "power", Power: &p})
	}
	for i := range h.recentNotices {
		n := h.recentNotices[i]
		envs = append(envs, model.Envelope{Type: "notice", Notice: &n})
	}
	return envs
}

func (h *Hub) broadcast(env model.Envelope) {
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- b:
		default:
			// Slow client: drop this message; it recovers on the next snapshot.
		}
	}
}

func (h *Hub) removeClient(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) readPump(c *client) {
	defer h.removeClient(c)
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
