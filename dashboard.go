package main

// dashboardHTML returns the single-page dashboard. It is inlined so the plugin
// stays one shared library with no asset directory to deploy.
func dashboardHTML() string {
	return dashboardPage
}

const dashboardPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CPA context</title>
<style>
  :root {
    --bg: #0f1115; --panel: #171a21; --line: #262b36; --text: #e6e8ee;
    --dim: #8b93a7; --system: #6b7fd7; --tools: #4bb3a1; --user: #63a4ff;
    --assistant: #8a7cf0; --thinking: #b07cd6; --toolcall: #d69a4b;
    --toolresult: #4b9ad6; --injected: #7d8798; --image: #d6607c; --other: #4a5164;
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--text);
    font: 13px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; }
  header { padding: 16px 20px; border-bottom: 1px solid var(--line); display: flex;
    align-items: baseline; gap: 16px; flex-wrap: wrap; }
  h1 { font-size: 15px; margin: 0; font-weight: 600; letter-spacing: .01em; }
  .note { color: var(--dim); font-size: 12px; }
  main { display: grid; grid-template-columns: minmax(260px, 340px) 1fr; height: calc(100vh - 57px); }
  #list { border-right: 1px solid var(--line); overflow-y: auto; }
  .card { padding: 10px 14px; border-bottom: 1px solid var(--line); cursor: pointer; }
  .card:hover { background: #1b1f27; }
  .card[aria-selected="true"] { background: #1f2531; box-shadow: inset 2px 0 0 var(--user); }
  .card .top { display: flex; justify-content: space-between; gap: 8px; }
  .card .id { font-weight: 600; font-size: 12px; overflow: hidden; text-overflow: ellipsis;
    white-space: nowrap; }
  .card .meta { color: var(--dim); font-size: 11px; margin-top: 2px; }
  .card .bar { margin-top: 6px; }
  #detail { overflow-y: auto; padding: 18px 22px 60px; }
  .kpis { display: flex; gap: 22px; flex-wrap: wrap; margin-bottom: 18px; }
  .kpi .v { font-size: 20px; font-weight: 600; font-variant-numeric: tabular-nums; }
  .kpi .k { color: var(--dim); font-size: 11px; text-transform: uppercase;
    letter-spacing: .06em; }
  .bar { display: flex; height: 10px; border-radius: 5px; overflow: hidden;
    background: #10131a; }
  .bar span { display: block; height: 100%; }
  .legend { display: flex; flex-wrap: wrap; gap: 12px; margin: 10px 0 18px; }
  .legend div { display: flex; align-items: center; gap: 6px; font-size: 12px;
    color: var(--dim); cursor: pointer; }
  .legend i { width: 9px; height: 9px; border-radius: 2px; display: block; }
  .legend .off { opacity: .35; }
  section { margin-bottom: 26px; }
  h2 { font-size: 12px; text-transform: uppercase; letter-spacing: .08em;
    color: var(--dim); margin: 0 0 10px; font-weight: 600; }
  #chart { width: 100%; height: 190px; display: block; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { text-align: left; padding: 5px 8px; border-bottom: 1px solid var(--line); }
  th { color: var(--dim); font-weight: 500; text-transform: uppercase;
    font-size: 10px; letter-spacing: .06em; }
  td.n { text-align: right; font-variant-numeric: tabular-nums; }
  .item { border-bottom: 1px solid var(--line); }
  .item summary { padding: 6px 8px; cursor: pointer; display: flex; gap: 10px;
    align-items: baseline; }
  .item summary::-webkit-details-marker { display: none; }
  .item .tag { font-size: 10px; text-transform: uppercase; letter-spacing: .05em;
    color: var(--dim); min-width: 84px; }
  .item .lbl { flex: 1; overflow: hidden; text-overflow: ellipsis;
    white-space: nowrap; }
  .item .tok { color: var(--dim); font-variant-numeric: tabular-nums; }
  .item pre { margin: 0; padding: 10px 12px; background: #10131a; overflow-x: auto;
    max-height: 340px; font: 11.5px/1.5 ui-monospace, "Cascadia Mono", monospace;
    white-space: pre-wrap; word-break: break-word; }
  .empty { color: var(--dim); padding: 40px 0; text-align: center; }
  .ev { display: flex; gap: 10px; padding: 5px 0; border-bottom: 1px solid var(--line);
    font-size: 12px; }
  .ev .t { color: var(--dim); font-variant-numeric: tabular-nums; min-width: 62px; }
  .ev .d { color: var(--dim); }
  button { background: #232936; color: var(--text); border: 1px solid var(--line);
    border-radius: 5px; padding: 4px 10px; font-size: 12px; cursor: pointer; }
  button:hover { background: #2a3242; }
</style>
</head>
<body>
<header>
  <h1>CPA context</h1>
  <span class="note" id="scope"></span>
  <span style="flex:1"></span>
  <input id="key" type="password" placeholder="management key" autocomplete="off"
    style="background:#10131a;border:1px solid var(--line);border-radius:5px;
    padding:4px 8px;color:var(--text);font-size:12px;width:170px">
  <button id="refresh">Refresh</button>
</header>
<main>
  <div id="list"><div class="empty">Loading…</div></div>
  <div id="detail"><div class="empty">Select a conversation</div></div>
</main>
<script>
const COLORS = {
  system: "#6b7fd7", tools: "#4bb3a1", user: "#63a4ff", assistant: "#8a7cf0",
  thinking: "#b07cd6", toolCall: "#d69a4b", toolResult: "#4b9ad6",
  injected: "#7d8798", image: "#d6607c", other: "#4a5164"
};
const EVENT_LABELS = {
  compaction: "compacted", cache: "cache miss", model: "model switched",
  first: "conversation started", trim: "history trimmed"
};
const LABELS = {
  system: "System prompt", tools: "Tool schemas", user: "User messages",
  assistant: "Assistant replies", thinking: "Reasoning", toolCall: "Tool calls",
  toolResult: "Tool results", injected: "Injected context", image: "Images",
  other: "Other"
};

let data = null;
let selected = null;
let hidden = new Set();

const fmt = n => n >= 1e6 ? (n/1e6).toFixed(2)+"M"
  : n >= 1e3 ? (n/1e3).toFixed(1)+"k" : String(n||0);
const pct = (a,b) => b > 0 ? (100*a/b).toFixed(1)+"%" : "0%";
const esc = s => String(s ?? "").replace(/[&<>"]/g, c =>
  ({ "&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;" }[c]));

function bar(composition, total) {
  if (!composition || !total) return "";
  let html = "";
  for (const key of Object.keys(COLORS)) {
    const v = composition[key] || 0;
    if (!v || hidden.has(key)) continue;
    html += '<span style="width:' + (100*v/total).toFixed(3) +
      "%;background:" + COLORS[key] + '" title="' + LABELS[key] + ": " + fmt(v) + '"></span>';
  }
  return '<div class="bar">' + html + "</div>";
}

function title(s) {
  if (s.project) return s.project;
  return String(s.id || "").replace(/^header:/, "");
}

function flatten(node, depth) {
  const rows = [Object.assign({}, node, { depth: depth })];
  for (const child of node.children || []) rows.push(...flatten(child, depth + 1));
  return rows;
}

function familyRows() {
  const out = [];
  for (const root of data.families || []) out.push(...flatten(root, 0));
  return out;
}

function renderList() {
  const list = document.getElementById("list");
  if (!data || !data.sessions || data.sessions.length === 0) {
    list.innerHTML = '<div class="empty">No traffic observed yet.<br>' +
      '<span class="note">Send a request through the proxy.</span></div>';
    return;
  }
  const byId = new Map((data.sessions || []).map(s => [s.id, s]));
  const members = familyRows();
  const rows = members.length ? members.map(m => {
    const s = byId.get(m.id) || {};
    return { node: m, sess: s };
  }) : (data.sessions || []).map(s => ({ node: { id: s.id, depth: 0 }, sess: s }));

  list.innerHTML = rows.map(({ node, sess }) => {
    const when = sess.lastSeen ? new Date(sess.lastSeen).toLocaleTimeString() : "";
    const model = (sess.models && sess.models[0]) || "unknown model";
    const child = node.depth > 0;
    const badge = node.isSubagent || child ? "sub" : node.isFork ? "fork" : "";
    return '<div class="card" data-id="' + esc(sess.id) + '"' +
      ' style="padding-left:' + (14 + node.depth * 14) + 'px"' +
      (sess.id === selected ? ' aria-selected="true"' : "") + '>' +
      '<div class="top"><span class="id">' +
      (child ? "&#8627; " : "") + esc(title(sess)) + "</span>" +
      '<span class="meta">' + fmt(sess.lastContextTokens) + "</span></div>" +
      '<div class="meta">' + esc(model) + " · " + sess.requests + " req · " + esc(when) +
      (badge ? " · " + badge : "") + "</div>" +
      (sess.unsourced ? '<div class="meta">no session header</div>' : "") +
      bar(sess.composition, sess.lastContextTokens) + "</div>";
  }).join("");
  for (const card of list.querySelectorAll(".card")) {
    card.onclick = () => { selected = card.dataset.id; renderList(); renderDetail(); };
  }
}

function renderDetail() {
  const detail = document.getElementById("detail");
  const s = data && data.sessions ? data.sessions.find(x => x.id === selected) : null;
  if (!s) { detail.innerHTML = '<div class="empty">Select a conversation</div>'; return; }

  const used = s.lastContextTokens || 0;
  const events = (data.events || []).filter(e => e.sessionId === s.id).slice(0, 40);

  let html = '<div class="kpis">' +
    kpi(fmt(used), "current window") +
    kpi(fmt(s.maxOutputTokens), "output cap") +
    kpi(fmt(s.inputTokens), "prompt tokens") +
    kpi(fmt(s.outputTokens), "completion tokens") +
    kpi(fmt(s.cacheReadTokens), "cache read") +
    kpi(String(s.requests), "requests") +
    (s.failed ? kpi(String(s.failed), "failed") : "") +
    "</div>";

  if (!s.calibrated) {
    html += '<p class="note">Composition is estimated by the local tokenizer. ' +
      "The provider reported no usage counters for this request, so the split " +
      "is not scaled to a billed total.</p>";
  } else {
    html += '<p class="note">The composition is estimated per item, then scaled so ' +
      "it sums exactly to the prompt tokens the provider reported.</p>";
  }

  html += '<section><h2>Composition</h2>' +
    (bar(s.composition, used) || '<div class="note">No composition recorded.</div>');
  html += '<div class="legend">';
  for (const key of Object.keys(COLORS)) {
    const v = (s.composition || {})[key] || 0;
    if (!v) continue;
    html += '<div data-cat="' + key + '" class="' + (hidden.has(key) ? "off" : "") + '">' +
      '<i style="background:' + COLORS[key] + '"></i>' + LABELS[key] +
      " · " + fmt(v) + " · " + pct(v, used) + "</div>";
  }
  html += "</div></section>";

  html += '<section><h2>Window over time</h2><canvas id="chart"></canvas></section>';

  if (s.lastItems && s.lastItems.length) {
    html += '<section><h2>What the newest request carried</h2>';
    const sorted = [...s.lastItems].sort((a, b) => (b.chars || b.bytes) - (a.chars || a.bytes));
    for (const it of sorted) {
      const body = it.text ? esc(it.text) : "[" + fmt(it.bytes) + " bytes of encoded data]";
      html += '<details class="item"><summary>' +
        '<span class="tag" style="color:' + (COLORS[it.category] || "#888") + '">' +
        esc(LABELS[it.category] || it.category) + "</span>" +
        '<span class="lbl">' + esc(it.label) + "</span>" +
        '<span class="tok">' + fmt(it.chars) + " chars</span></summary>" +
        "<pre>" + body + "</pre></details>";
    }
    html += "</section>";
  }

  const family = (data.families || []).map(r => flatten(r, 0)).find(rows => rows.some(r => r.id === s.id));
  if (family && family.length > 1) {
    html += '<section><h2>Agent family</h2><table><thead><tr>' +
      "<th>Member</th><th>Kind</th><th>Context</th><th>Output</th><th>Requests</th>" +
      "</tr></thead><tbody>";
    for (const m of family) {
      const kind = m.isFork ? "fork" : m.isSubagent ? "subagent" : m.depth > 0 ? "child" : "root";
      html += "<tr><td>" + "&nbsp;".repeat(m.depth * 3) + esc(m.label || m.id) + "</td>" +
        "<td>" + kind + "</td><td class='n'>" + fmt(m.contextTokens) + "</td>" +
        "<td class='n'>" + fmt(m.outputTokens) + "</td><td class='n'>" + m.requests +
        "</td></tr>";
    }
    html += "</tbody></table></section>";
  }

  if (events.length) {
    html += '<section><h2>Window events</h2>';
    for (const ev of events) {
      html += '<div class="ev"><span class="t">' +
        new Date(ev.at).toLocaleTimeString() + '</span><span>' +
        esc(EVENT_LABELS[ev.kind] || ev.kind) +
        '</span><span class="d">' + esc(ev.detail || "") +
        (ev.delta ? " (" + (ev.delta > 0 ? "+" : "") + fmt(ev.delta) + ")" : "") +
        "</span></div>";
    }
    html += "</section>";
  }

  detail.innerHTML = html;
  detail.querySelectorAll(".legend div").forEach(el => {
    el.onclick = () => {
      const key = el.dataset.cat;
      if (hidden.has(key)) hidden.delete(key); else hidden.add(key);
      renderList(); renderDetail();
    };
  });
  drawChart(s);
}

function kpi(value, label) {
  return '<div class="kpi"><div class="v">' + value + '</div><div class="k">' + label + "</div></div>";
}

function drawChart(s) {
  const canvas = document.getElementById("chart");
  if (!canvas) return;
  const dpr = window.devicePixelRatio || 1;
  const w = canvas.clientWidth, h = 190;
  canvas.width = w * dpr; canvas.height = h * dpr;
  const ctx = canvas.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, w, h);

  const points = s.timeline || [];
  if (points.length === 0) return;
  const max = Math.max(...points.map(p => p.input || 0), 1);
  const bw = Math.max(2, Math.min(24, (w - 40) / points.length - 2));

  ctx.strokeStyle = "#262b36";
  ctx.beginPath(); ctx.moveTo(34, 10); ctx.lineTo(34, h - 20); ctx.lineTo(w - 6, h - 20); ctx.stroke();

  ctx.fillStyle = "#8b93a7";
  ctx.font = "10px ui-sans-serif";
  ctx.fillText(fmt(max), 2, 14);
  ctx.fillText("0", 22, h - 8);

  points.forEach((p, i) => {
    const x = 38 + i * ((w - 46) / points.length);
    const bh = (p.input / max) * (h - 40);
    const base = ctx.fillStyle;
    if (p.composition) {
      let y = h - 20;
      for (const key of Object.keys(COLORS)) {
        const v = p.composition[key] || 0;
        if (!v) continue;
        const seg = (v / max) * (h - 40);
        ctx.fillStyle = COLORS[key];
        ctx.fillRect(x, y - seg, bw, seg);
        y -= seg;
      }
      ctx.fillStyle = base;
    } else {
      ctx.fillStyle = p.failed ? "#d6607c" : "#4b9ad6";
      ctx.fillRect(x, h - 20 - bh, bw, bh);
    }
    if (p.compaction) {
      ctx.fillStyle = "#b07cd6";
      ctx.fillRect(x, 12, bw, 4);
    }
  });
}

const KEY_STORE = "cpa-context-management-key";
const keyInput = document.getElementById("key");
keyInput.value = localStorage.getItem(KEY_STORE) || "";
keyInput.onchange = () => {
  localStorage.setItem(KEY_STORE, keyInput.value.trim());
  load();
};

function showMessage(text) {
  document.getElementById("list").innerHTML =
    '<div class="empty">' + esc(text) + "</div>";
  document.getElementById("detail").innerHTML = '<div class="empty"></div>';
}

async function load() {
  const key = (keyInput.value || "").trim();
  if (!key) {
    showMessage("Enter the CPA management key to read context data. " +
      "The dashboard itself is a static page, so the data stays behind the " +
      "authenticated management API.");
    return;
  }
  const res = await fetch("/v0/management/cpa-context/overview", {
    headers: { "Accept": "application/json", "Authorization": "Bearer " + key }
  });
  if (!res.ok) {
    showMessage("Management API answered " + res.status + ". Check the key.");
    return;
  }
  data = await res.json();
  document.getElementById("scope").textContent = data.limits ? data.limits.scope : "";
  if (!selected && data.sessions && data.sessions.length) selected = data.sessions[0].id;
  renderList(); renderDetail();
}

document.getElementById("refresh").onclick = load;
window.addEventListener("resize", () => { if (selected) renderDetail(); });
load();
setInterval(() => { if ((keyInput.value || "").trim()) load(); }, 5000);
</script>
</body>
</html>
`
