// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

(function () {
  "use strict";

  const MAX_FEED = 400;
  const MAX_ACTIVITY = 60;

  const state = {
    connections: [],
    filter: "",
    paused: false,
  };

  const el = {
    wsDot: document.getElementById("ws-dot"),
    sysDot: document.getElementById("sys-dot"),
    eventsDot: document.getElementById("events-dot"),
    statClients: document.getElementById("stat-clients"),
    statConns: document.getElementById("stat-conns"),
    statRate: document.getElementById("stat-rate"),
    statTotal: document.getElementById("stat-total"),
    connBody: document.getElementById("conn-body"),
    connCount: document.getElementById("conn-count"),
    activity: document.getElementById("activity-log"),
    feed: document.getElementById("event-feed"),
    filter: document.getElementById("filter"),
    pauseBtn: document.getElementById("pause-btn"),
    clearBtn: document.getElementById("clear-btn"),
  };

  // ---- Controls -----------------------------------------------------------
  el.filter.addEventListener("input", function () {
    state.filter = el.filter.value.trim().toLowerCase();
  });
  el.pauseBtn.addEventListener("click", function () {
    state.paused = !state.paused;
    el.pauseBtn.textContent = state.paused ? "Resume" : "Pause";
    el.pauseBtn.classList.toggle("active", state.paused);
  });
  el.clearBtn.addEventListener("click", function () {
    el.feed.innerHTML = "";
  });

  // ---- Rendering ----------------------------------------------------------
  function setDot(node, up) {
    node.classList.toggle("up", up);
    node.classList.toggle("down", !up);
  }

  function fmtBytes(n) {
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
    return (n / 1024 / 1024).toFixed(1) + " MB";
  }

  function renderConnections(conns) {
    state.connections = conns;
    el.connCount.textContent = conns.length;
    if (!conns.length) {
      el.connBody.innerHTML = '<tr class="empty"><td colspan="8">No active connections</td></tr>';
      return;
    }
    const rows = conns.map(function (c) {
      const addr = c.ip ? c.ip + ":" + c.port : "-";
      return (
        "<tr>" +
        "<td><span class=\"badge\">" + esc(c.account || "-") + "</span></td>" +
        "<td>" + esc(c.name || "-") + "</td>" +
        "<td>" + esc(c.kind || "-") + "</td>" +
        "<td>" + esc(addr) + "</td>" +
        "<td>" + c.subscriptions + "</td>" +
        "<td>" + c.inMsgs + "</td>" +
        "<td>" + c.outMsgs + "</td>" +
        "<td>" + esc(c.uptime || "-") + "</td>" +
        "</tr>"
      );
    });
    el.connBody.innerHTML = rows.join("");
  }

  function addActivity(le) {
    const li = document.createElement("li");
    const t = new Date(le.time).toLocaleTimeString();
    const who = le.name || ("cid " + le.cid);
    li.innerHTML =
      "<span class=\"" + esc(le.kind) + "\">" +
      (le.kind === "connect" ? "\u25B2 connected" : "\u25BC disconnected") +
      "</span> " + esc(who) + " <span class=\"badge\">" + esc(le.account || "-") + "</span> " +
      "<span class=\"ev-time\">" + t + "</span>";
    el.activity.insertBefore(li, el.activity.firstChild);
    while (el.activity.childElementCount > MAX_ACTIVITY) {
      el.activity.removeChild(el.activity.lastChild);
    }
  }

  function passesFilter(ev) {
    if (!state.filter) return true;
    return (
      ev.subject.toLowerCase().indexOf(state.filter) !== -1 ||
      (ev.payload && ev.payload.toLowerCase().indexOf(state.filter) !== -1)
    );
  }

  function addEvents(events) {
    if (state.paused) return;
    const atBottom =
      el.feed.parentElement.scrollTop + el.feed.parentElement.clientHeight >=
      el.feed.parentElement.scrollHeight - 30;

    const frag = document.createDocumentFragment();
    let added = 0;
    events.forEach(function (ev) {
      if (!passesFilter(ev)) return;
      const li = document.createElement("li");
      const t = new Date(ev.time).toLocaleTimeString();
      li.innerHTML =
        "<div class=\"ev-head\">" +
        "<span class=\"ev-time\">" + t + "</span>" +
        "<span class=\"ev-subject\">" + esc(ev.subject) + "</span>" +
        "<span class=\"ev-size\">" + fmtBytes(ev.size) + "</span>" +
        "</div>" +
        (ev.payload ? "<div class=\"ev-payload\">" + esc(ev.payload) + "</div>" : "");
      frag.appendChild(li);
      added++;
    });
    if (!added) return;
    el.feed.appendChild(frag);
    while (el.feed.childElementCount > MAX_FEED) {
      el.feed.removeChild(el.feed.firstChild);
    }
    if (atBottom) {
      el.feed.parentElement.scrollTop = el.feed.parentElement.scrollHeight;
    }
  }

  function renderStats(s) {
    el.statClients.textContent = s.clientConns;
    el.statConns.textContent = s.connections;
    el.statRate.textContent = s.eventsPerSecond;
    el.statTotal.textContent = s.eventsTotal;
    setDot(el.sysDot, s.sysConnected);
    setDot(el.eventsDot, s.eventConnected);
  }

  function esc(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // ---- WebSocket ----------------------------------------------------------
  function connect() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(proto + "//" + location.host + "/ws");

    ws.onopen = function () {
      setDot(el.wsDot, true);
    };
    ws.onclose = function () {
      setDot(el.wsDot, false);
      setDot(el.sysDot, false);
      setDot(el.eventsDot, false);
      setTimeout(connect, 2000);
    };
    ws.onerror = function () {
      ws.close();
    };
    ws.onmessage = function (msg) {
      let env;
      try {
        env = JSON.parse(msg.data);
      } catch (e) {
        return;
      }
      switch (env.type) {
        case "connections":
          renderConnections(env.connections || []);
          break;
        case "events":
          if (env.events && env.events.length) addEvents(env.events);
          break;
        case "lifecycle":
          if (env.lifecycle) addActivity(env.lifecycle);
          break;
        case "stats":
          if (env.stats) renderStats(env.stats);
          break;
      }
    };
  }

  connect();
})();
