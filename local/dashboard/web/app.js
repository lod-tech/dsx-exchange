// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

(function () {
  "use strict";

  const MAX_FEED = 400;
  const MAX_ACTIVITY = 60;
  const MAX_POINTS = 120; // ~2 minutes at 1 Hz

  const state = {
    connections: [],
    filter: "",
    mqttOnly: false,
    paused: false,
    // Power history series.
    hist: {
      rps: [],
      accepted: [],
      power: [],
      target: [],
    },
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
    mqttOnly: document.getElementById("mqtt-only"),
    activity: document.getElementById("activity-log"),
    feed: document.getElementById("event-feed"),
    filter: document.getElementById("filter"),
    pauseBtn: document.getElementById("pause-btn"),
    clearBtn: document.getElementById("clear-btn"),
    connInfoBtn: document.getElementById("conn-info-btn"),
    connDialog: document.getElementById("conn-dialog"),
    connDialogClose: document.getElementById("conn-dialog-close"),
    connCompletion: document.getElementById("conn-completion"),
    connFlex: document.getElementById("conn-flex"),
    powerFeed: document.getElementById("power-feed"),
    breachBanner: document.getElementById("breach-banner"),
    pPower: document.getElementById("p-power"),
    pTarget: document.getElementById("p-target"),
    pRps: document.getElementById("p-rps"),
    pInflight: document.getElementById("p-inflight"),
    pInflightLabel: document.getElementById("p-inflight-label"),
    pShed: document.getElementById("p-shed"),
    pCompliant: document.getElementById("p-compliant"),
    targetBody: document.getElementById("target-body"),
    chartRps: document.getElementById("chart-rps"),
    chartPower: document.getElementById("chart-power"),
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
  el.mqttOnly.addEventListener("change", function () {
    state.mqttOnly = el.mqttOnly.checked;
    renderConnections(state.connections);
  });

  // ---- Connection info dialog --------------------------------------------
  if (el.connInfoBtn && el.connDialog) {
    el.connInfoBtn.addEventListener("click", function () {
      if (typeof el.connDialog.showModal === "function") {
        el.connDialog.showModal();
      } else {
        el.connDialog.setAttribute("open", "");
      }
    });
    el.connDialogClose.addEventListener("click", function () {
      el.connDialog.close();
    });
    // Close when clicking the backdrop (outside the dialog content box).
    el.connDialog.addEventListener("click", function (e) {
      if (e.target === el.connDialog) el.connDialog.close();
    });
    // Click-to-copy for any code value marked copyable.
    el.connDialog.addEventListener("click", function (e) {
      const code = e.target.closest(".copyable");
      if (!code) return;
      const text = code.textContent;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(function () {
          flashCopied(code);
        }, function () {});
      }
    });
    loadConnectionInfo();
  }

  function flashCopied(node) {
    node.classList.add("copied");
    setTimeout(function () { node.classList.remove("copied"); }, 900);
  }

  function loadConnectionInfo() {
    fetch("/api/connection-info", { headers: { accept: "application/json" } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (info) { if (info) renderConnectionInfo(info); })
      .catch(function () {});
  }

  function ce(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  // codeVal builds a click-to-copy code chip.
  function codeVal(text) {
    return ce("code", "copyable", text);
  }

  // addRow appends a <dt>/<dd> pair. Each value may be a string (rendered as a
  // copyable code chip) or a DOM node (appended verbatim). An optional note is
  // appended as muted text.
  function addRow(dl, label, values, note) {
    dl.appendChild(ce("dt", null, label));
    const dd = ce("dd");
    values.forEach(function (v, i) {
      if (i > 0) dd.appendChild(document.createTextNode(" "));
      dd.appendChild(typeof v === "string" ? codeVal(v) : v);
    });
    if (note) {
      const s = ce("span", "conn-note", " " + note);
      dd.appendChild(s);
    }
    dl.appendChild(dd);
  }

  function renderConnectionInfo(info) {
    const c = info.completion || {};
    const f = info.flex || {};

    if (el.connCompletion) {
      const dl = el.connCompletion;
      dl.innerHTML = "";
      const base = c.baseURL || "";
      addRow(dl, "Base URL", [base || "—"]);
      const eps = (c.endpoints || []).map(function (e) { return codeVal(base + e); });
      addRow(dl, "Endpoints", eps.length ? eps : [document.createTextNode("—")]);
      addRow(dl, "Model", [c.model || "—"]);
      addRow(dl, "Auth", [document.createTextNode(c.auth === "none" || !c.auth ? "None (open endpoint)" : c.auth)]);
      if (base) {
        const example = "curl " + base + "/chat/completions -H 'content-type: application/json' -d '{\"model\":\"" + (c.model || "") + "\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'";
        addRow(dl, "Example", [example]);
      }
    }

    if (el.connFlex) {
      const dl = el.connFlex;
      dl.innerHTML = "";
      addRow(dl, "MQTT broker", [f.mqttURL || "—"]);
      addRow(dl, "OAuth2 token URL", [f.oauthTokenURL || "—"]);
      addRow(dl, "Client ID", [f.clientID || "—"]);
      if (f.clientSecret) addRow(dl, "Client secret", [f.clientSecret]);
      addRow(dl, "Scope", [f.scope || "—"], "grant: " + (f.grant || "client_credentials"));
      addRow(dl, "MQTT username", [f.username || "oauthtoken"], "password = OAuth2 access token");
      addRow(dl, "Publish topic", [f.publishTopic || "—"], "replace <isv-id> with your ISV identifier");
      const subs = (f.subscribeTopics || []).map(function (t) { return codeVal(t); });
      addRow(dl, "Subscribe topics", subs.length ? subs : [document.createTextNode("—")]);
      addRow(dl, "CloudEvents type", [f.cloudEventType || "—"]);
      addRow(dl, "CloudEvents source", [f.cloudEventSource || "—"]);
      addRow(dl, "Feed", [f.feed || "—"]);
    }
  }

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

  function isMqtt(c) {
    return String(c.type || "").toLowerCase() === "mqtt";
  }

  function renderConnections(conns) {
    state.connections = conns;
    const shown = state.mqttOnly ? conns.filter(isMqtt) : conns;
    el.connCount.textContent = state.mqttOnly
      ? shown.length + " / " + conns.length
      : conns.length;
    if (!shown.length) {
      const msg = state.mqttOnly ? "No MQTT clients connected" : "No active connections";
      el.connBody.innerHTML = '<tr class="empty"><td colspan="8">' + msg + "</td></tr>";
      return;
    }
    const rows = shown.map(function (c) {
      const addr = c.ip ? c.ip + ":" + c.port : "-";
      const transport = (c.type || "nats").toLowerCase();
      const kind = esc(c.kind || "-") +
        " <span class=\"transport transport-" + esc(transport) + "\">" + esc(transport) + "</span>";
      return (
        "<tr>" +
        "<td><span class=\"badge\">" + esc(c.account || "-") + "</span></td>" +
        "<td>" + esc(c.name || "-") + "</td>" +
        "<td>" + kind + "</td>" +
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

  // ---- Power (AI factory) -------------------------------------------------
  function pushPoint(arr, v) {
    arr.push(v);
    if (arr.length > MAX_POINTS) arr.shift();
  }

  function renderPower(p) {
    if (p.feedTag) el.powerFeed.textContent = p.feedTag;
    pushPoint(state.hist.rps, p.rps || 0);
    pushPoint(state.hist.accepted, p.acceptedPerSec || 0);
    pushPoint(state.hist.power, p.powerMw || 0);
    pushPoint(state.hist.target, typeof p.targetMw === "number" ? p.targetMw : null);

    el.pPower.textContent = fmtMW(p.powerMw || 0);
    el.pTarget.textContent = fmtMW(p.targetMw || 0) + (p.targetActive ? "" : " (default)");
    el.pRps.textContent = (p.rps || 0).toFixed(1);
    el.pInflight.textContent = p.inFlight || 0;
    el.pInflightLabel.textContent = p.perRequestMw
      ? "In-flight (" + fmtRate(p.perRequestMw) + " MW each)"
      : "In-flight requests";
    el.pShed.textContent = (p.shedPerSec || 0).toFixed(1);
    el.pCompliant.textContent = p.compliant ? "OK" : "OVER";
    el.pCompliant.className = "pstat-value " + (p.compliant ? "ok" : "over");

    renderTargetCard(p.lastTarget);
    renderBreach(p.breachStatus, p.breachSeverity);
    drawCharts();
  }

  function renderTargetCard(t) {
    if (!t) {
      el.targetBody.innerHTML =
        "No ISV target received yet \u2014 running at default cap.";
      return;
    }
    const when = new Date(t.time).toLocaleTimeString();
    const value = t.cleared
      ? "<span class=\"target-value cleared\">cap cleared</span>"
      : "<span class=\"target-value\">" + fmtMW(t.valueMw || 0) + " MW</span>";
    el.targetBody.innerHTML =
      "<span class=\"badge\">" + esc(t.by || "ISV") + "</span> " +
      value +
      " <span class=\"target-feed\">on " + esc(t.feeds || "all feeds") + "</span>" +
      " <span class=\"ev-time\">" + esc(when) + "</span>";
  }

  function renderBreach(status, severity) {
    if (!status) {
      el.breachBanner.classList.add("hidden");
      el.breachBanner.className = "breach-banner hidden";
      return;
    }
    el.breachBanner.className = "breach-banner " + (severity === "critical" ? "critical" : "warning");
    el.breachBanner.textContent = "\u26A0 Power breach " + status + " (" + (severity || "warning") + ")";
  }

  function sizeCanvas(canvas) {
    const dpr = window.devicePixelRatio || 1;
    // Use the CSS-rendered size (fixed height in style.css) as the source of
    // truth so the backing store never compounds across redraws.
    const w = canvas.clientWidth || canvas.parentElement.clientWidth;
    const h = canvas.clientHeight || 150;
    if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
      canvas.width = Math.floor(w * dpr);
      canvas.height = Math.floor(h * dpr);
    }
    const ctx = canvas.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    return { ctx: ctx, w: w, h: h };
  }

  // drawChart renders one or more line series on a canvas with a shared y-axis.
  function drawChart(canvas, lines, yMaxHint, unit) {
    const dim = sizeCanvas(canvas);
    const ctx = dim.ctx;
    const w = dim.w;
    const h = dim.h;
    const padL = 42, padR = 8, padT = 10, padB = 16;
    ctx.clearRect(0, 0, w, h);

    let yMax = yMaxHint || 1;
    lines.forEach(function (ln) {
      ln.values.forEach(function (v) {
        if (v != null && v > yMax) yMax = v;
      });
    });
    yMax = niceMax(yMax);

    const plotW = w - padL - padR;
    const plotH = h - padT - padB;
    const n = MAX_POINTS;
    const xAt = function (i) { return padL + (plotW * i) / (n - 1); };
    const yAt = function (v) { return padT + plotH - (plotH * v) / yMax; };

    // Grid + y labels.
    ctx.strokeStyle = "#2b3340";
    ctx.fillStyle = "#8b949e";
    ctx.font = "10px -apple-system, sans-serif";
    ctx.lineWidth = 1;
    for (let g = 0; g <= 2; g++) {
      const val = (yMax * g) / 2;
      const y = yAt(val);
      ctx.beginPath();
      ctx.moveTo(padL, y);
      ctx.lineTo(w - padR, y);
      ctx.stroke();
      ctx.fillText(fmtNum(val) + (unit ? " " + unit : ""), 4, y + 3);
    }

    lines.forEach(function (ln) {
      const vals = ln.values;
      const offset = n - vals.length;
      ctx.strokeStyle = ln.color;
      ctx.lineWidth = ln.width || 2;
      if (ln.dashed) ctx.setLineDash([5, 4]); else ctx.setLineDash([]);
      ctx.beginPath();
      let started = false;
      for (let i = 0; i < vals.length; i++) {
        if (vals[i] == null) { started = false; continue; }
        const x = xAt(offset + i);
        const y = yAt(vals[i]);
        if (!started) { ctx.moveTo(x, y); started = true; } else { ctx.lineTo(x, y); }
      }
      ctx.stroke();
      ctx.setLineDash([]);
    });
  }

  function drawCharts() {
    drawChart(el.chartRps, [
      { values: state.hist.rps, color: "#58a6ff", width: 2 },
      { values: state.hist.accepted, color: "#76b900", width: 2 },
    ], 5, "");
    // Small y-floor so sub-MW loads (from small per-request rates) auto-scale
    // instead of being pinned near zero under a fixed 5 MW hint.
    drawChart(el.chartPower, [
      { values: state.hist.power, color: "#76b900", width: 2 },
      { values: state.hist.target, color: "#f85149", width: 2, dashed: true },
    ], 0.01, "MW");
  }

  // niceMax rounds an axis maximum up to a clean 1/2/5 * 10^n value. It works
  // for sub-unit values too (e.g. 0.05), which matters for small power scales.
  function niceMax(v) {
    if (v <= 0) return 1;
    const pow = Math.pow(10, Math.floor(Math.log10(v)));
    const n = v / pow;
    let step = 1;
    if (n > 5) step = 10; else if (n > 2) step = 5; else if (n > 1) step = 2;
    return step * pow;
  }

  function fmtNum(v) {
    if (v >= 10) return v.toFixed(0);
    if (v >= 1) return v.toFixed(1);
    if (v > 0) return String(Number(v.toFixed(3)));
    return "0";
  }

  // fmtRate renders the per-request power rate without trailing zeros
  // (e.g. 0.001, 0.5, 1).
  function fmtRate(v) {
    return String(Number(v.toFixed(6)));
  }

  // fmtMW renders a power value in MW with scale-appropriate precision so
  // sub-MW loads (from small per-request rates) stay legible.
  function fmtMW(v) {
    return fmtNum(v);
  }

  function addNotice(n) {
    const li = document.createElement("li");
    const t = new Date(n.time).toLocaleTimeString();
    const icon = n.kind === "target" ? "\u2699" : n.kind === "breach" ? "\u26A0" : "\u21C5";
    li.innerHTML =
      "<span class=\"notice-" + esc(n.level || "info") + "\">" + icon + " " + esc(n.text) + "</span> " +
      "<span class=\"ev-time\">" + t + "</span>";
    el.activity.insertBefore(li, el.activity.firstChild);
    while (el.activity.childElementCount > MAX_ACTIVITY) {
      el.activity.removeChild(el.activity.lastChild);
    }
  }

  window.addEventListener("resize", drawCharts);

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
        case "power":
          if (env.power) renderPower(env.power);
          break;
        case "notice":
          if (env.notice) addNotice(env.notice);
          break;
      }
    };
  }

  connect();
})();
