package server

// dashboardHTML is the single-page dashboard served at GET /.
// It uses EventSource for the realtime log and setInterval polling for tiles.
// No frameworks, no build step — plain HTML/CSS/JS.
var dashboardHTML = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>nexus dashboard</title>
<style>
  :root {
    --bg: #0d0d0d;
    --bg2: #141414;
    --bg3: #1c1c1c;
    --border: #2a2a2a;
    --text: #e0e0e0;
    --muted: #666;
    --accent: #4a9eff;
    --info: #4a9eff;
    --warn: #f5a623;
    --error: #e05252;
    --green: #4caf50;
    --font: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background: var(--bg);
    color: var(--text);
    font-family: var(--font);
    font-size: 13px;
    height: 100vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 8px 16px;
    background: var(--bg2);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  header h1 { font-size: 13px; font-weight: 600; letter-spacing: 0.05em; color: var(--accent); }
  .status-dot {
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--muted);
    transition: background 0.3s;
  }
  .status-dot.live { background: var(--green); box-shadow: 0 0 6px var(--green); }
  .status-dot.error { background: var(--error); }
  #status-label { color: var(--muted); font-size: 11px; }
  .spacer { flex: 1; }
  #clock { color: var(--muted); font-size: 11px; }

  .panels {
    display: grid;
    grid-template-columns: 1fr 320px;
    gap: 1px;
    flex: 1;
    overflow: hidden;
    background: var(--border);
  }
  .panel {
    background: var(--bg);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .panel-header {
    padding: 6px 12px;
    background: var(--bg2);
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .panel-header .count {
    margin-left: auto;
    color: var(--muted);
    font-weight: normal;
  }

  /* ── Event log ─────────────────────────────────────────────────────── */
  #log {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
    scroll-behavior: smooth;
  }
  .log-row {
    display: grid;
    grid-template-columns: 80px 52px 1fr;
    gap: 8px;
    padding: 2px 12px;
    line-height: 1.5;
    border-bottom: 1px solid transparent;
    transition: background 0.1s;
  }
  .log-row:hover { background: var(--bg3); }
  .log-ts { color: var(--muted); font-size: 11px; white-space: nowrap; }
  .log-level { font-size: 11px; font-weight: 600; text-align: center; border-radius: 3px; padding: 0 4px; }
  .level-INFO  { color: var(--info);  background: rgba(74,158,255,0.08); }
  .level-WARN  { color: var(--warn);  background: rgba(245,166,35,0.08); }
  .level-ERROR { color: var(--error); background: rgba(224,82,82,0.08); }
  .log-msg { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .log-fields { color: var(--muted); font-size: 11px; }

  /* ── Tiles ─────────────────────────────────────────────────────────── */
  #tiles-panel { gap: 0; }
  #tiles {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .tile {
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    transition: border-color 0.2s;
  }
  .tile.overridden { border-color: var(--warn); }
  .tile-name { font-size: 10px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); }
  .tile-value { font-size: 22px; font-weight: 600; color: var(--text); line-height: 1.2; }
  .tile-meta { font-size: 10px; color: var(--muted); display: flex; gap: 8px; align-items: center; }
  .tile-override-badge {
    font-size: 9px; text-transform: uppercase; letter-spacing: 0.05em;
    color: var(--warn); background: rgba(245,166,35,0.12);
    border-radius: 3px; padding: 1px 4px;
  }
  #tiles-updated { font-size: 10px; color: var(--muted); padding: 4px 12px 6px; flex-shrink: 0; text-align: right; }

  /* scrollbar */
  ::-webkit-scrollbar { width: 4px; }
  ::-webkit-scrollbar-track { background: transparent; }
  ::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }
</style>
</head>
<body>

<header>
  <h1>nexus</h1>
  <div class="status-dot" id="dot"></div>
  <span id="status-label">connecting…</span>
  <div class="spacer"></div>
  <span id="clock"></span>
</header>

<div class="panels">
  <!-- Left: realtime event log -->
  <div class="panel">
    <div class="panel-header">
      realtime
      <span class="count" id="log-count">0 events</span>
    </div>
    <div id="log"></div>
  </div>

  <!-- Right: digest tiles -->
  <div class="panel" id="tiles-panel">
    <div class="panel-header">digest tiles</div>
    <div id="tiles"><div style="color:var(--muted);padding:12px;font-size:11px">loading…</div></div>
    <div id="tiles-updated"></div>
  </div>
</div>

<script>
// ── Config ───────────────────────────────────────────────────────────────────
const MAX_LOG_ROWS = 500;   // max DOM rows before pruning oldest
const TILES_POLL_MS = 3000; // tile refresh interval

// ── Clock ────────────────────────────────────────────────────────────────────
const clock = document.getElementById('clock');
function updateClock() {
  const now = new Date();
  clock.textContent = now.toLocaleTimeString('en-US', {hour12: false});
}
updateClock();
setInterval(updateClock, 1000);

// ── SSE — realtime event log ─────────────────────────────────────────────────
const log   = document.getElementById('log');
const dot   = document.getElementById('dot');
const label = document.getElementById('status-label');
const countEl = document.getElementById('log-count');
let logCount = 0;
let autoScroll = true;

log.addEventListener('scroll', () => {
  autoScroll = log.scrollTop + log.clientHeight >= log.scrollHeight - 20;
});

function formatTime(ts) {
  const d = new Date(ts);
  return d.toLocaleTimeString('en-US', {hour12: false, hour:'2-digit', minute:'2-digit', second:'2-digit'});
}

function appendEvent(ev) {
  const row = document.createElement('div');
  row.className = 'log-row';

  const ts = document.createElement('span');
  ts.className = 'log-ts';
  ts.textContent = formatTime(ev.ts);

  const lvl = document.createElement('span');
  lvl.className = 'log-level level-' + (ev.level || 'INFO');
  lvl.textContent = (ev.level || 'INFO').substring(0, 4);

  const msg = document.createElement('span');
  msg.className = 'log-msg';
  let text = ev.msg || '';
  if (ev.fields && Object.keys(ev.fields).length > 0) {
    const pairs = Object.entries(ev.fields).map(([k,v]) => k+'='+v).join(' ');
    text += ' <span class="log-fields">' + pairs + '</span>';
  }
  msg.innerHTML = text;

  row.appendChild(ts);
  row.appendChild(lvl);
  row.appendChild(msg);
  log.appendChild(row);

  logCount++;
  countEl.textContent = logCount + (logCount === 1 ? ' event' : ' events');

  // Prune oldest rows to keep DOM lean.
  while (log.children.length > MAX_LOG_ROWS) {
    log.removeChild(log.firstChild);
    logCount = log.children.length;
    countEl.textContent = logCount + '+ events';
  }

  if (autoScroll) {
    log.scrollTop = log.scrollHeight;
  }
}

function connectSSE() {
  const es = new EventSource('/events');

  es.onopen = () => {
    dot.className = 'status-dot live';
    label.textContent = 'live';
  };

  es.onmessage = (e) => {
    try {
      appendEvent(JSON.parse(e.data));
    } catch(err) {
      console.error('parse event:', err);
    }
  };

  es.onerror = () => {
    dot.className = 'status-dot error';
    label.textContent = 'reconnecting…';
    // EventSource reconnects automatically; just update UI.
  };
}

connectSSE();

// ── Tiles — polling ──────────────────────────────────────────────────────────
const tilesEl   = document.getElementById('tiles');
const updatedEl = document.getElementById('tiles-updated');

function formatTileValue(v) {
  if (typeof v === 'number') {
    // Show up to 2 decimal places, strip trailing zeros.
    return v % 1 === 0 ? v.toFixed(0) : v.toFixed(2).replace(/\.?0+$/, '');
  }
  return String(v);
}

function renderTiles(snapshot) {
  if (!snapshot || !snapshot.tiles) return;

  tilesEl.innerHTML = '';
  for (const t of snapshot.tiles) {
    const tile = document.createElement('div');
    tile.className = 'tile' + (t.overridden ? ' overridden' : '');

    const name = document.createElement('div');
    name.className = 'tile-name';
    name.textContent = t.name.replace(/_/g, ' ');

    const val = document.createElement('div');
    val.className = 'tile-value';
    val.textContent = formatTileValue(t.value);

    const meta = document.createElement('div');
    meta.className = 'tile-meta';
    if (t.func) {
      meta.textContent = t.func;
    }
    if (t.overridden) {
      const badge = document.createElement('span');
      badge.className = 'tile-override-badge';
      badge.textContent = 'override';
      meta.appendChild(badge);
    }

    tile.appendChild(name);
    tile.appendChild(val);
    if (t.func || t.overridden) tile.appendChild(meta);
    tilesEl.appendChild(tile);
  }

  const d = new Date(snapshot.computed_at);
  updatedEl.textContent = 'updated ' + d.toLocaleTimeString('en-US', {hour12:false});
}

async function fetchTiles() {
  try {
    const res = await fetch('/tiles');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    renderTiles(await res.json());
  } catch(err) {
    updatedEl.textContent = 'tiles unavailable';
  }
}

fetchTiles();
setInterval(fetchTiles, TILES_POLL_MS);
</script>
</body>
</html>
`)
